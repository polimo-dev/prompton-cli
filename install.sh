#!/usr/bin/env sh
#
# Install the PromptOn CLI.
#
#   curl -fsSL https://prompton.ai/install.sh | sh
#
# prompton.ai/install.sh redirects to this file on the repository's main
# branch, so the two are always the same script:
#
#   https://raw.githubusercontent.com/polimo-dev/prompton-cli/main/install.sh
#
# Environment:
#   PTN_VERSION       release to install: a tag such as v0.1.0 (0.1.0 works
#                     too), or "main" for the rolling build of the main
#                     branch. Default: the latest stable release; while there
#                     is none yet, the rolling main build.
#   PTN_INSTALL_DIR   where to put the binary (default: /usr/local/bin,
#                     falling back to ~/.local/bin when that is not writable)
#   GITHUB_TOKEN      optional; sent to the GitHub API so shared CI runners do
#                     not hit the unauthenticated rate limit.
#
# The download is verified against the release's checksums.txt before anything
# is installed, and everything happens inside a temporary directory that is
# removed on exit.
#
# install_test.sh loads this file with PTN_INSTALL_LIB=1 set, which defines
# the functions without running the install.

set -eu

REPO="polimo-dev/prompton-cli"
BINARY="prompton"
ROLLING_TAG="main-latest"
API="https://api.github.com/repos/$REPO"
DOWNLOADS="https://github.com/$REPO/releases/download"

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"
}

# ---- environment ----------------------------------------------------------

detect_os() {
	os=$(uname -s)
	case "$os" in
		Linux) printf 'linux' ;;
		Darwin) printf 'darwin' ;;
		MINGW* | MSYS* | CYGWIN*)
			die "Windows is not supported by this script — download the zip from https://github.com/$REPO/releases"
			;;
		*) die "unsupported operating system: $os" ;;
	esac
}

detect_arch() {
	arch=$(uname -m)
	case "$arch" in
		x86_64 | amd64) printf 'amd64' ;;
		arm64 | aarch64) printf 'arm64' ;;
		*) die "unsupported architecture: $arch" ;;
	esac
}

# fetch writes a URL's body to a file, using whichever downloader is present.
fetch() {
	url=$1
	dest=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$dest"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$dest"
	else
		die "curl or wget is required"
	fi
}

# api_get writes a GitHub API response body to a file and prints the HTTP
# status code — "000" when no response arrived at all — so a caller can tell
# 404 (no such release) apart from every other failure.
api_get() {
	url=$1
	dest=$2
	token=${GITHUB_TOKEN:-${GH_TOKEN:-}}
	if command -v curl >/dev/null 2>&1; then
		set -- -sSL -o "$dest" -w '%{http_code}' "$url"
		[ -z "$token" ] || set -- -H "Authorization: Bearer $token" "$@"
		code=$(curl "$@") || true
	elif command -v wget >/dev/null 2>&1; then
		# -S prints each response's status line to stderr even under -q; the
		# last one is the final answer once redirects are followed.
		set -- -q -S -O "$dest" "$url"
		[ -z "$token" ] || set -- --header "Authorization: Bearer $token" "$@"
		code=$(wget "$@" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9][0-9][0-9]\).*/\1/p' | tail -n 1)
	else
		die "curl or wget is required"
	fi
	printf '%s' "${code:-000}"
}

# ---- release resolution ---------------------------------------------------
#
# Everything below reads the JSON that GitHub's releases API returns and is
# what install_test.sh exercises. grep/sed keep the script free of a JSON
# parser dependency; the fields read here never contain escaped quotes.

# release_tag maps PTN_VERSION to the Git tag that names its release.
release_tag() {
	case "$1" in
		main | "$ROLLING_TAG") printf '%s' "$ROLLING_TAG" ;;
		[0-9]*) printf 'v%s' "$1" ;;
		*) printf '%s' "$1" ;;
	esac
}

# json_string prints the value of the first "<key>": "…" pair in the JSON on
# stdin. The release's own fields come before its assets, so for tag_name this
# is the release, not an asset.
json_string() {
	grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" |
		head -n 1 |
		sed -e "s/^\"$1\"[[:space:]]*:[[:space:]]*\"//" -e 's/"$//'
}

# asset_name reads a release's JSON on stdin and prints the archive built for
# os/arch, e.g. prompton_0.1.0_darwin_arm64.tar.gz. The version in the name is
# whatever GoReleaser stamped — the tag for a release, a snapshot such as
# 0.0.0-main.1a2b3c4 for the rolling build — so it is matched, never guessed.
asset_name() {
	os=$1
	arch=$2
	grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' |
		sed -e 's/^"name"[[:space:]]*:[[:space:]]*"//' -e 's/"$//' |
		grep "^${BINARY}_.*_${os}_${arch}\\.tar\\.gz\$" |
		head -n 1
}

# archive_version takes the version back out of an archive name:
# prompton_0.1.0_darwin_arm64.tar.gz darwin arm64 → 0.1.0
archive_version() {
	v=${1#"${BINARY}_"}
	printf '%s' "${v%"_$2_$3.tar.gz"}"
}

# resolve_release picks the release to install, writes its API JSON to the
# file named by $1, and prints its tag:
#
#   1. PTN_VERSION when set ("main" is the rolling build of the main branch);
#   2. otherwise the latest stable release;
#   3. and while there is no stable release yet (the API answers 404), the
#      rolling main-latest pre-release.
resolve_release() {
	dest=$1

	if [ -n "${PTN_VERSION:-}" ]; then
		tag=$(release_tag "$PTN_VERSION")
		code=$(api_get "$API/releases/tags/$tag" "$dest")
		[ "$code" = 200 ] || die "no release for $PTN_VERSION (tag $tag): GitHub answered HTTP $code"
		printf '%s' "$tag"
		return
	fi

	code=$(api_get "$API/releases/latest" "$dest")
	case "$code" in
		200)
			tag=$(json_string tag_name < "$dest")
			[ -n "$tag" ] || die "could not read tag_name from the latest release"
			printf '%s' "$tag"
			return
			;;
		404) ;;
		*) die "could not look up the latest release: GitHub answered HTTP $code" ;;
	esac

	code=$(api_get "$API/releases/tags/$ROLLING_TAG" "$dest")
	[ "$code" = 200 ] || die "no stable release and no $ROLLING_TAG build: GitHub answered HTTP $code"
	printf '%s' "$ROLLING_TAG"
}

# choose_archive prints the archive for os/arch from the release JSON in $2,
# and says so on stderr when the rolling build was chosen because there is no
# stable release yet.
choose_archive() {
	tag=$1
	json=$2
	os=$3
	arch=$4

	archive=$(asset_name "$os" "$arch" < "$json")
	[ -n "$archive" ] || die "release $tag has no archive for $os/$arch"

	if [ "$tag" = "$ROLLING_TAG" ] && [ -z "${PTN_VERSION:-}" ]; then
		log "no stable release yet — installing the latest main build $(archive_version "$archive" "$os" "$arch")"
	fi
	printf '%s' "$archive"
}

# verify_checksum compares one file against its line in checksums.txt.
verify_checksum() {
	file=$1
	sums=$2
	name=$(basename "$file")

	expected=$(grep " $name\$" "$sums" | awk '{print $1}' | head -n 1)
	[ -n "$expected" ] || die "no checksum for $name in checksums.txt"

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$file" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$file" | awk '{print $1}')
	else
		die "sha256sum or shasum is required to verify the download"
	fi

	[ "$expected" = "$actual" ] ||
		die "checksum mismatch for $name (expected $expected, got $actual)"
}

# install_dir picks a writable destination, preferring a system-wide one.
install_dir() {
	if [ -n "${PTN_INSTALL_DIR:-}" ]; then
		printf '%s' "$PTN_INSTALL_DIR"
		return
	fi
	if [ -w /usr/local/bin ] 2>/dev/null; then
		printf '/usr/local/bin'
		return
	fi
	printf '%s/.local/bin' "$HOME"
}

# ---- install --------------------------------------------------------------

main() {
	need uname
	need tar
	need mktemp

	os=$(detect_os)
	arch=$(detect_arch)

	tmp=$(mktemp -d)
	# shellcheck disable=SC2064  # $tmp must expand now, not at trap time.
	trap "rm -rf '$tmp'" EXIT INT TERM

	log "Looking up the release…"
	tag=$(resolve_release "$tmp/release.json")
	archive=$(choose_archive "$tag" "$tmp/release.json" "$os" "$arch")
	version=$(archive_version "$archive" "$os" "$arch")
	base="$DOWNLOADS/$tag"

	log "Downloading ${archive}…"
	fetch "$base/$archive" "$tmp/$archive" ||
		die "could not download $base/$archive"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" ||
		die "could not download the checksum file"

	log "Verifying checksum…"
	verify_checksum "$tmp/$archive" "$tmp/checksums.txt"

	tar -xzf "$tmp/$archive" -C "$tmp"
	[ -f "$tmp/$BINARY" ] || die "the archive did not contain $BINARY"
	chmod +x "$tmp/$BINARY"

	dir=$(install_dir)
	mkdir -p "$dir" || die "could not create $dir"

	if [ -w "$dir" ]; then
		mv "$tmp/$BINARY" "$dir/$BINARY"
	elif command -v sudo >/dev/null 2>&1; then
		log "$dir needs elevated permissions; using sudo."
		sudo mv "$tmp/$BINARY" "$dir/$BINARY"
	else
		die "$dir is not writable — set PTN_INSTALL_DIR to somewhere you own"
	fi

	log "Installed $BINARY $version to $dir/$BINARY"

	case ":$PATH:" in
		*":$dir:"*) ;;
		*)
			log ""
			log "$dir is not on your PATH. Add it:"
			log "    export PATH=\"$dir:\$PATH\""
			;;
	esac

	log ""
	log "Check it: $BINARY --version"
	log "Next:     $BINARY login"
}

if [ -z "${PTN_INSTALL_LIB:-}" ]; then
	main "$@"
fi
