#!/bin/sh
# shellcheck disable=SC2016  # assertions are eval-ed strings on purpose
# Offline test for uninstall.sh: a fake binary + config in a scratch HOME.
set -eu
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export HOME="$tmp/home" XDG_CONFIG_HOME="$tmp/xdg" PTN_INSTALL_DIR="$tmp/bin"
mkdir -p "$HOME" "$XDG_CONFIG_HOME/prompton" "$PTN_INSTALL_DIR"
printf '{"host":"https://example.test","token":"t"}\n' > "$XDG_CONFIG_HOME/prompton/config.json"
# fake binary records that logout was called
cat > "$PTN_INSTALL_DIR/prompton" <<'BIN'
#!/bin/sh
[ "$1" = "logout" ] && printf 'logout\n' >> "$PTN_LOGOUT_LOG"
BIN
chmod +x "$PTN_INSTALL_DIR/prompton"
export PTN_LOGOUT_LOG="$tmp/logout.log"

sh "$(dirname "$0")/uninstall.sh" 2> "$tmp/out"

fail=0
check() { if eval "$2"; then :; else printf 'FAIL: %s\n' "$1"; fail=1; fi; }
check "logout was invoked before removal" '[ -f "$PTN_LOGOUT_LOG" ] && grep -q logout "$PTN_LOGOUT_LOG"'
check "binary removed" '[ ! -e "$PTN_INSTALL_DIR/prompton" ]'
check "config dir removed" '[ ! -d "$XDG_CONFIG_HOME/prompton" ]'
check "summary printed" 'grep -q "PromptOn CLI uninstalled" "$tmp/out"'

# second run: nothing to do, still exits 0; PTN_KEEP_CONFIG keeps the directory
mkdir -p "$XDG_CONFIG_HOME/prompton"
PTN_KEEP_CONFIG=1 sh "$(dirname "$0")/uninstall.sh" 2> "$tmp/out2"
check "keep config honoured" '[ -d "$XDG_CONFIG_HOME/prompton" ] && grep -q "kept" "$tmp/out2"'
check "no binary message" 'grep -q "no prompton binary found" "$tmp/out2"'

[ "$fail" -eq 0 ] && printf 'uninstall_test: ok\n'
exit "$fail"
