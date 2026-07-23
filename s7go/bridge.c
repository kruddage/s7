/* SPDX-License-Identifier: 0BSD */
/*
 * bridge.c — the single C translation unit that pulls the vendored s7
 * interpreter into the Go build.
 *
 * cgo compiles the .c files that live in this package directory, but not files
 * in sibling directories. Rather than duplicate the 3.8 MB checksummed
 * amalgamation, this one-line unit #includes ../third_party/s7.c (resolved via
 * the -I in s7.go's #cgo CFLAGS). It is the *only* place s7.c is compiled, so
 * there is exactly one copy of s7's symbols in the resulting archive.
 *
 * The feature defines are set alongside the include path in s7.go's #cgo
 * CFLAGS, so both this unit and every s7.h include agree on them. They match
 * the krudds7 CLI and the libs7 release archives exactly (see
 * ../tools/build-lib.sh and ../third_party/VENDOR.md):
 *
 *   -DWITH_C_LOADER=0   drop the dlopen-based C loader
 *   -DWITH_MAIN=0       keep s7 a library (no s7-owned main)
 *
 * so the interpreter linked into a Go program is byte-for-byte the one krudds7
 * ships.
 */
#include "s7.c"
