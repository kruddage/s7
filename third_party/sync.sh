# SPDX-License-Identifier: 0BSD
#
# third_party/sync.sh — verify (and, if needed, fetch) the pinned s7 amalgamation.
#
# s7.c / s7.h are committed at the pin in s7.artifact; this checks their
# sha256 so every build compiles the same bytes CI does, and re-downloads
# from the pinned commit if a file is missing or doesn't match (e.g. after
# bumping the pin). Idempotent: once a file matches its pinned checksum, no
# network I/O happens on the next run.
#
# Sourced (not executed) by krudds7.sh before it compiles s7.c into the
# krudds7 binary — so this has to be plain POSIX shell, with no dependency on
# krudds7 itself. Expects $root (the repo root) to already be set by the
# sourcing script.

s7_dir="$root/third_party"
# shellcheck source=s7.artifact
. "$s7_dir/s7.artifact"

s7_sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		echo "third_party: no sha256sum or shasum found" >&2
		exit 1
	fi
}

s7_download() {
	# $1 url, $2 dest
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		echo "third_party: no curl or wget found" >&2
		exit 1
	fi
}

# Fetch $2 (url) to $3 (dest) unless it's already there with the pinned
# checksum $4; verify after downloading either way.
s7_sync_one() {
	name=$1 url=$2 dest=$3 want=$4
	if [ -f "$dest" ] && [ "$(s7_sha256 "$dest")" = "$want" ]; then
		return 0
	fi
	echo "third_party: fetching $name ($S7_VERSION, $S7_DATE)" >&2
	tmp="$dest.tmp.$$"
	if ! s7_download "$url" "$tmp"; then
		rm -f "$tmp"
		echo "third_party: failed to fetch $name from $url" >&2
		exit 1
	fi
	got=$(s7_sha256 "$tmp")
	if [ "$got" != "$want" ]; then
		rm -f "$tmp"
		echo "third_party: $name checksum mismatch (got $got, want $want)" >&2
		exit 1
	fi
	mv "$tmp" "$dest"
}

s7_sync_one "s7.c" "$S7_C_URL" "$s7_dir/s7.c" "$S7_C_SHA256"
s7_sync_one "s7.h" "$S7_H_URL" "$s7_dir/s7.h" "$S7_H_SHA256"
