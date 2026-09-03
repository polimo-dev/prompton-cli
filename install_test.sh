#!/usr/bin/env sh
#
# Tests for install.sh's release resolution:
#
#   sh install_test.sh
#
# install.sh is loaded with PTN_INSTALL_LIB=1, which defines its functions
# without running the install. They are fed the JSON shapes GitHub's releases
# API returns, and api_get is replaced so the lookup order — PTN_VERSION, then
# the latest stable release, then the rolling main build — runs without the
# network.

# PTN_VERSION is set inside $(...) on purpose: each case gets its own value and
# none leaks into the next. shellcheck reads that as a lost assignment.
# shellcheck disable=SC2030,SC2031

set -u
cd "$(dirname "$0")"

PTN_INSTALL_LIB=1
export PTN_INSTALL_LIB
# shellcheck source=install.sh
. ./install.sh
set +e # install.sh turns on -e; a failing assertion must not stop the run.

unset PTN_VERSION GITHUB_TOKEN GH_TOKEN

tmp=$(mktemp -d)
# shellcheck disable=SC2064  # $tmp must expand now, not at trap time.
trap "rm -rf '$tmp'" EXIT INT TERM

fails=0
assert_eq() { # name expected actual
	if [ "$2" = "$3" ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s\n     want: %s\n     got:  %s\n' "$1" "$2" "$3"
		fails=$((fails + 1))
	fi
}

# ---- fixtures -------------------------------------------------------------
# The shapes are what api.github.com/repos/<repo>/releases/… returns, trimmed
# to the fields around the ones install.sh reads. Note the release's own
# "name" and the "name" inside each asset: only the latter are archives.

cat > "$tmp/rolling.json" <<'JSON'
{
  "url": "https://api.github.com/repos/polimo-dev/prompton-cli/releases/1",
  "html_url": "https://github.com/polimo-dev/prompton-cli/releases/tag/main-latest",
  "id": 1,
  "author": {
    "login": "github-actions[bot]",
    "id": 41898282
  },
  "tag_name": "main-latest",
  "target_commitish": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b",
  "name": "main (rolling)",
  "draft": false,
  "prerelease": true,
  "assets": [
    {
      "url": "https://api.github.com/repos/polimo-dev/prompton-cli/releases/assets/11",
      "id": 11,
      "name": "checksums.txt",
      "content_type": "text/plain",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/checksums.txt"
    },
    {
      "id": 12,
      "name": "prompton_0.0.0-main.1a2b3c4_darwin_amd64.tar.gz",
      "content_type": "application/gzip",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/prompton_0.0.0-main.1a2b3c4_darwin_amd64.tar.gz"
    },
    {
      "id": 13,
      "name": "prompton_0.0.0-main.1a2b3c4_darwin_arm64.tar.gz",
      "content_type": "application/gzip",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/prompton_0.0.0-main.1a2b3c4_darwin_arm64.tar.gz"
    },
    {
      "id": 14,
      "name": "prompton_0.0.0-main.1a2b3c4_linux_amd64.tar.gz",
      "content_type": "application/gzip",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/prompton_0.0.0-main.1a2b3c4_linux_amd64.tar.gz"
    },
    {
      "id": 15,
      "name": "prompton_0.0.0-main.1a2b3c4_linux_arm64.tar.gz",
      "content_type": "application/gzip",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/prompton_0.0.0-main.1a2b3c4_linux_arm64.tar.gz"
    },
    {
      "id": 16,
      "name": "prompton_0.0.0-main.1a2b3c4_windows_amd64.zip",
      "content_type": "application/zip",
      "browser_download_url": "https://github.com/polimo-dev/prompton-cli/releases/download/main-latest/prompton_0.0.0-main.1a2b3c4_windows_amd64.zip"
    }
  ],
  "body": "Rolling build of `main`.\n\n- Version: `0.0.0-main.1a2b3c4`\n"
}
JSON

cat > "$tmp/stable.json" <<'JSON'
{
  "url": "https://api.github.com/repos/polimo-dev/prompton-cli/releases/2",
  "id": 2,
  "tag_name": "v0.1.0",
  "target_commitish": "main",
  "name": "v0.1.0",
  "draft": false,
  "prerelease": false,
  "assets": [
    {"id": 21, "name": "checksums.txt"},
    {"id": 22, "name": "prompton_0.1.0_darwin_amd64.tar.gz"},
    {"id": 23, "name": "prompton_0.1.0_darwin_arm64.tar.gz"},
    {"id": 24, "name": "prompton_0.1.0_linux_amd64.tar.gz"},
    {"id": 25, "name": "prompton_0.1.0_linux_arm64.tar.gz"},
    {"id": 26, "name": "prompton_0.1.0_windows_amd64.zip"}
  ],
  "body": "## Install\n\n```sh\ncurl -fsSL https://prompton.ai/install.sh | sh\n```\n"
}
JSON

# The same release with no whitespace, as a proxy client might re-serialise it.
printf '%s' '{"id":2,"tag_name":"v0.1.0","name":"v0.1.0","prerelease":false,"assets":[{"id":21,"name":"checksums.txt"},{"id":23,"name":"prompton_0.1.0_darwin_arm64.tar.gz"},{"id":24,"name":"prompton_0.1.0_linux_amd64.tar.gz"}],"body":""}' > "$tmp/compact.json"

# ---- asset_name: pick the archive by os/arch, never by guessing the version

assert_eq "rolling darwin/arm64" \
	"prompton_0.0.0-main.1a2b3c4_darwin_arm64.tar.gz" \
	"$(asset_name darwin arm64 < "$tmp/rolling.json")"
assert_eq "rolling darwin/amd64" \
	"prompton_0.0.0-main.1a2b3c4_darwin_amd64.tar.gz" \
	"$(asset_name darwin amd64 < "$tmp/rolling.json")"
assert_eq "rolling linux/amd64 (not the windows zip)" \
	"prompton_0.0.0-main.1a2b3c4_linux_amd64.tar.gz" \
	"$(asset_name linux amd64 < "$tmp/rolling.json")"
assert_eq "rolling linux/arm64" \
	"prompton_0.0.0-main.1a2b3c4_linux_arm64.tar.gz" \
	"$(asset_name linux arm64 < "$tmp/rolling.json")"
assert_eq "stable darwin/arm64" \
	"prompton_0.1.0_darwin_arm64.tar.gz" \
	"$(asset_name darwin arm64 < "$tmp/stable.json")"
assert_eq "compact JSON darwin/arm64" \
	"prompton_0.1.0_darwin_arm64.tar.gz" \
	"$(asset_name darwin arm64 < "$tmp/compact.json")"
assert_eq "windows zip is not a tar.gz archive" \
	"" \
	"$(asset_name windows amd64 < "$tmp/rolling.json")"
assert_eq "unknown arch yields nothing" \
	"" \
	"$(asset_name linux riscv64 < "$tmp/rolling.json")"

# ---- json_string: the release's tag, not an asset's name ------------------

assert_eq "tag_name of the rolling release" "main-latest" "$(json_string tag_name < "$tmp/rolling.json")"
assert_eq "tag_name of the stable release" "v0.1.0" "$(json_string tag_name < "$tmp/stable.json")"
assert_eq "tag_name from compact JSON" "v0.1.0" "$(json_string tag_name < "$tmp/compact.json")"
assert_eq "name is the release's own, not an asset's" "main (rolling)" "$(json_string name < "$tmp/rolling.json")"

# ---- archive_version / release_tag ----------------------------------------

assert_eq "version out of a rolling archive name" \
	"0.0.0-main.1a2b3c4" \
	"$(archive_version prompton_0.0.0-main.1a2b3c4_darwin_arm64.tar.gz darwin arm64)"
assert_eq "version out of a stable archive name" \
	"0.1.0" \
	"$(archive_version prompton_0.1.0_linux_amd64.tar.gz linux amd64)"

assert_eq "PTN_VERSION=main is the rolling tag" "main-latest" "$(release_tag main)"
assert_eq "PTN_VERSION=main-latest is the rolling tag" "main-latest" "$(release_tag main-latest)"
assert_eq "PTN_VERSION=v0.1.0 is used as is" "v0.1.0" "$(release_tag v0.1.0)"
assert_eq "PTN_VERSION=0.1.0 gets its v" "v0.1.0" "$(release_tag 0.1.0)"

# ---- resolve_release: lookup order against a scripted GitHub API ----------
#
# The stub answers /releases/latest with $LATEST_CODE, /releases/tags/main-latest
# with $ROLLING_CODE, /releases/tags/v0.1.0 with 200, and records every URL.

LATEST_CODE=200
ROLLING_CODE=200
api_get() {
	printf '%s\n' "$1" >> "$tmp/calls"
	case "$1" in
		*/releases/latest) code=$LATEST_CODE; body=$tmp/stable.json ;;
		*/releases/tags/main-latest) code=$ROLLING_CODE; body=$tmp/rolling.json ;;
		*/releases/tags/v0.1.0) code=200; body=$tmp/stable.json ;;
		*) code=404; body= ;;
	esac
	[ "$code" != 200 ] || cp "$body" "$2"
	printf '%s' "$code"
}
calls() { tr '\n' ' ' < "$tmp/calls" | sed 's/ $//'; }
reset() { : > "$tmp/calls"; rm -f "$tmp/out.json"; }

reset
LATEST_CODE=200
assert_eq "latest stable release wins by default" "v0.1.0" "$(resolve_release "$tmp/out.json")"
assert_eq "  …with one API call" "$API/releases/latest" "$(calls)"
assert_eq "  …and its JSON written for the caller" "v0.1.0" "$(json_string tag_name < "$tmp/out.json")"

reset
LATEST_CODE=404
ROLLING_CODE=200
assert_eq "no stable release falls back to main-latest" "main-latest" "$(resolve_release "$tmp/out.json")"
assert_eq "  …after asking for latest first" "$API/releases/latest $API/releases/tags/main-latest" "$(calls)"
assert_eq "  …with the rolling JSON written" "main-latest" "$(json_string tag_name < "$tmp/out.json")"

reset
LATEST_CODE=404
ROLLING_CODE=404
out=$(resolve_release "$tmp/out.json" 2> "$tmp/err")
assert_eq "no release at all fails" "1" "$?"
assert_eq "  …with an error naming main-latest" "error: no stable release and no main-latest build: GitHub answered HTTP 404" "$(cat "$tmp/err")"
assert_eq "  …and prints no tag" "" "$out"

reset
LATEST_CODE=403
ROLLING_CODE=200
out=$(resolve_release "$tmp/out.json" 2> "$tmp/err")
assert_eq "a non-404 error does not fall back" "1" "$?"
assert_eq "  …so only latest was asked" "$API/releases/latest" "$(calls)"
assert_eq "  …and the status is reported" "error: could not look up the latest release: GitHub answered HTTP 403" "$(cat "$tmp/err")"

reset
LATEST_CODE=200
assert_eq "PTN_VERSION=main asks for the rolling tag" "main-latest" \
	"$(export PTN_VERSION=main; resolve_release "$tmp/out.json")"
assert_eq "  …without touching latest" "$API/releases/tags/main-latest" "$(calls)"

reset
assert_eq "PTN_VERSION=0.1.0 asks for tag v0.1.0" "v0.1.0" \
	"$(export PTN_VERSION=0.1.0; resolve_release "$tmp/out.json")"
assert_eq "  …by its tag URL" "$API/releases/tags/v0.1.0" "$(calls)"

reset
out=$(export PTN_VERSION=v9.9.9; resolve_release "$tmp/out.json" 2> "$tmp/err")
assert_eq "an unknown PTN_VERSION fails" "1" "$?"
assert_eq "  …naming what was asked for" "error: no release for v9.9.9 (tag v9.9.9): GitHub answered HTTP 404" "$(cat "$tmp/err")"

# ---- choose_archive: the fallback is announced, a pinned main build is not -

out=$(choose_archive main-latest "$tmp/rolling.json" darwin arm64 2> "$tmp/err")
assert_eq "fallback picks the rolling archive" "prompton_0.0.0-main.1a2b3c4_darwin_arm64.tar.gz" "$out"
assert_eq "  …and says so, with the version" \
	"no stable release yet — installing the latest main build 0.0.0-main.1a2b3c4" \
	"$(cat "$tmp/err")"

out=$(export PTN_VERSION=main; choose_archive main-latest "$tmp/rolling.json" linux amd64 2> "$tmp/err")
assert_eq "PTN_VERSION=main picks the rolling archive" "prompton_0.0.0-main.1a2b3c4_linux_amd64.tar.gz" "$out"
assert_eq "  …quietly" "" "$(cat "$tmp/err")"

out=$(choose_archive v0.1.0 "$tmp/stable.json" linux arm64 2> "$tmp/err")
assert_eq "a stable release picks its archive" "prompton_0.1.0_linux_arm64.tar.gz" "$out"
assert_eq "  …quietly" "" "$(cat "$tmp/err")"

out=$(choose_archive v0.1.0 "$tmp/stable.json" linux riscv64 2> "$tmp/err")
assert_eq "a missing platform fails" "1" "$?"
assert_eq "  …naming the release and platform" "error: release v0.1.0 has no archive for linux/riscv64" "$(cat "$tmp/err")"

# ---- done -----------------------------------------------------------------

if [ "$fails" -ne 0 ]; then
	printf '\n%s assertion(s) failed\n' "$fails"
	exit 1
fi
printf '\nall install.sh tests passed\n'
