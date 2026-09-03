#!/bin/sh
# PromptOn CLI uninstaller.
#
#   curl -fsSL https://prompton.ai/uninstall.sh | sh
#
# What it does, in order:
#   1. signs the CLI out (`prompton logout`) so the session token is revoked on the server,
#   2. removes the `prompton` binary install.sh put in place (PTN_INSTALL_DIR, /usr/local/bin,
#      ~/.local/bin, or wherever it is on PATH),
#   3. removes the configuration directory (${XDG_CONFIG_HOME:-~/.config}/prompton).
#
# Environment:
#   PTN_INSTALL_DIR    where install.sh put the binary (checked first)
#   PTN_KEEP_CONFIG=1  keep the configuration directory (session token stays revoked)
#
# Homebrew installs are not touched: run `brew uninstall prompton` for those.
set -eu

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

config_dir() {
	printf '%s/prompton' "${XDG_CONFIG_HOME:-$HOME/.config}"
}

# Every place a prompton binary may live, one per line, deduplicated.
candidates() {
	{
		[ -n "${PTN_INSTALL_DIR:-}" ] && printf '%s/prompton\n' "$PTN_INSTALL_DIR"
		printf '/usr/local/bin/prompton\n'
		printf '%s/.local/bin/prompton\n' "$HOME"
		command -v prompton 2>/dev/null || true
	} | awk 'NF && !seen[$0]++'
}

brew_managed() {
	case "$1" in
	*/Cellar/prompton/* | */opt/homebrew/opt/prompton/* | */usr/local/opt/prompton/*) return 0 ;;
	esac
	return 1
}

# Revoke the server session through the CLI itself, if a binary and a config file exist.
sign_out() {
	bin=""
	for c in $(candidates); do
		if [ -x "$c" ] && ! brew_managed "$c"; then
			bin="$c"
			break
		fi
	done
	[ -n "$bin" ] || return 0
	[ -f "$(config_dir)/config.json" ] || return 0
	if "$bin" logout >/dev/null 2>&1; then
		log "signed out (session token revoked)"
	else
		log "note: could not sign out (already logged out, or the server is unreachable)"
	fi
}

remove_binaries() {
	removed=0
	for c in $(candidates); do
		[ -e "$c" ] || continue
		if brew_managed "$c"; then
			log "skipping Homebrew-managed $c — run: brew uninstall prompton"
			continue
		fi
		if rm -f "$c" 2>/dev/null; then
			log "removed $c"
			removed=1
		else
			log "cannot remove $c (permission denied) — run: sudo rm -f $c"
		fi
	done
	[ "$removed" -eq 1 ] || log "no prompton binary found in the usual places"
}

remove_config() {
	dir="$(config_dir)"
	if [ "${PTN_KEEP_CONFIG:-0}" = "1" ]; then
		log "kept $dir (PTN_KEEP_CONFIG=1)"
		return 0
	fi
	if [ -d "$dir" ]; then
		rm -rf "$dir"
		log "removed $dir"
	fi
}

main() {
	sign_out
	remove_binaries
	remove_config
	log "PromptOn CLI uninstalled."
}

if [ "${PTN_INSTALL_LIB:-0}" != "1" ]; then
	main "$@"
fi
