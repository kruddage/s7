#!/bin/sh
# SPDX-License-Identifier: 0BSD
#
# tools/build-lib.sh — compile the vendored s7 into a linkable static library.
#
# krudds7.sh builds the *CLI* — s7 linked into the krudds7 front door. This
# builds the *library*: the same third_party/s7.c, compiled with the same
# feature defines the CLI uses (WITH_C_LOADER=0, WITH_MAIN=0), archived into a
# static lib another project (e.g. kruddage/engine) can link in-process. It also
# stages the matching s7.h, since a linkable lib is useless without its header,
# and writes a .sha256 next to each artifact (same convention as the CLI asset).
#
#   ./tools/build-lib.sh native <outdir>    # cc / ar    -> libs7-linux-x86_64.a
#   ./tools/build-lib.sh wasm   <outdir>    # emcc / emar -> libs7-wasm32.a
#
# The compiler/archiver default per target but honour CC/AR overrides. The
# archive name defaults per target so existing release assets stay stable;
# S7_LIB_TRIPLE overrides just the platform tag, for building an archive for a
# target other than the host:
#
#   S7_LIB_TRIPLE=windows-x86_64 CC=x86_64-w64-mingw32-gcc \
#       AR=x86_64-w64-mingw32-ar ./tools/build-lib.sh native dist
#
# writes libs7-windows-x86_64.a instead. Nothing derives the tag from the host:
# the compiler decides what is actually produced, so guessing from uname would
# just mislabel a cross build. Unset, every existing caller (ci.yml,
# release-please.yml) produces byte-identical asset names.
set -e

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

target=${1:?usage: build-lib.sh <native|wasm> <outdir>}
outdir=${2:?usage: build-lib.sh <native|wasm> <outdir>}

case "$target" in
native)
	cc=${CC:-cc}
	ar=${AR:-ar}
	triple=${S7_LIB_TRIPLE:-linux-x86_64}
	;;
wasm)
	cc=${CC:-emcc}
	ar=${AR:-emar}
	triple=${S7_LIB_TRIPLE:-wasm32}
	;;
*)
	echo "build-lib.sh: unknown target '$target' (want: native | wasm)" >&2
	exit 1
	;;
esac

lib=libs7-$triple.a

# Verify (or fetch) the pinned s7 amalgamation, exactly as krudds7.sh does, so
# the lib is built from the same checksummed bytes as the CLI.
. "$root/third_party/sync.sh"

mkdir -p "$outdir"
obj="$outdir/s7.o"

echo "build-lib.sh: building $lib with $cc / $ar" >&2
# Same feature defines as the CLI build (see krudds7.sh / VENDOR.md): drop the
# dlopen C loader and keep s7 a library (no s7-owned main), so the archive is
# ABI-compatible with how engine already compiles s7.c in. -w silences upstream
# s7's warnings. The KRUDD-LOCAL PATCH already baked into third_party/s7.c is
# what makes this archive safe to link on wasm.
"$cc" -O2 -w -DWITH_C_LOADER=0 -DWITH_MAIN=0 \
	-I"$root/third_party" \
	-c "$root/third_party/s7.c" -o "$obj"

rm -f "$outdir/$lib"
"$ar" rcs "$outdir/$lib" "$obj"
rm -f "$obj"

# The header is target-independent (it's the vendored s7.h), so both targets
# stage identical bytes — a single shared s7.h asset per release.
cp "$root/third_party/s7.h" "$outdir/s7.h"

# Checksums mirror the CLI asset: `<hash>  <basename>`, verifiable with
# `sha256sum -c`. Run from outdir so the recorded name is the bare basename.
( cd "$outdir" \
	&& sha256sum "$lib" > "$lib.sha256" \
	&& sha256sum s7.h > s7.h.sha256 )

echo "build-lib.sh: wrote $outdir/$lib, $outdir/s7.h (+ .sha256)" >&2
