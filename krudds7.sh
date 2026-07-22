#!/bin/sh
# SPDX-License-Identifier: 0BSD
#
# krudds7 bootstrap
#   ./krudds7.sh              build if stale, then run the krudds7 REPL
#   ./krudds7.sh build        build only, no run (what CI runs)
#   ./krudds7.sh -e '(+ 1 2)' build if stale, then pass args straight to krudds7
#   ./krudds7.sh script.scm   build if stale, then run a Scheme file
#
# The binary is a single self-contained file: krudds7.c linked against the
# vendored s7 amalgamation. No configure step, no build system — just cc.
set -e

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# System default compiler — CC if set, else the first of clang/gcc/cc found.
cc=${CC:-}
if [ -z "$cc" ]; then
	for c in clang gcc cc; do
		if command -v "$c" >/dev/null 2>&1; then
			cc=$c
			break
		fi
	done
fi
if [ -z "$cc" ]; then
	echo "krudds7.sh: no C compiler found (set CC, or install cc/gcc/clang)" >&2
	exit 1
fi

# s7.c/s7.h are vendored (see third_party/s7.artifact); sync.sh verifies their
# checksum — and fetches them if missing — before we compile.
. "$root/third_party/sync.sh"

# The version stamped into the binary comes from version.txt (maintained by
# release-please), overridable via KRUDDS7_VERSION so CI can append a PR/preview
# suffix. Strip whitespace so a trailing newline in the file doesn't leak in.
version=${KRUDDS7_VERSION:-}
if [ -z "$version" ] && [ -f "$root/version.txt" ]; then
	version=$(tr -d ' \t\n\r' < "$root/version.txt")
fi
: "${version:=0.0.0-dev}"

# Windows toolchains write an .exe whether or not -o asked for one, so the
# staleness check below has to look for the name the compiler will actually
# produce — otherwise `[ ! -x "$bin" ]` is always true and every run rebuilds.
# Detected from the shell rather than the compiler because the check happens
# before any compile; KRUDDS7_EXE_SUFFIX overrides for cross builds.
exe=${KRUDDS7_EXE_SUFFIX-}
if [ -z "${KRUDDS7_EXE_SUFFIX+set}" ]; then
	case $(uname -s 2>/dev/null || echo unknown) in
	MINGW*|MSYS*|CYGWIN*) exe=.exe ;;
	esac
fi

bin="$root/krudds7$exe"
src="$root/src/krudds7.c"
s7="$root/third_party/s7.c"
if [ ! -x "$bin" ] || [ "$src" -nt "$bin" ] || [ "$s7" -nt "$bin" ]; then
	echo "krudds7.sh: building krudds7 $version with $cc" >&2
	# -w silences upstream s7's warnings; WITH_C_LOADER=0 drops the dlopen
	# loader and WITH_MAIN=0 keeps s7 a library so krudds7.c owns main().
	"$cc" -O2 -w -DWITH_C_LOADER=0 -DWITH_MAIN=0 \
		-DKRUDDS7_VERSION="\"$version\"" \
		-I"$root/third_party" \
		-o "$bin" "$src" "$s7" -lm
fi

# `build` just builds; anything else is handed to the binary. `--` lets a caller
# pass a leading arg that happens to be "build".
if [ "$1" = build ]; then
	exit 0
fi
if [ "$1" = -- ]; then
	shift
fi
exec "$bin" "$@"
