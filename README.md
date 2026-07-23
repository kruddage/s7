# krudds7

[![License: 0BSD](https://img.shields.io/badge/License-0BSD-blue.svg)](https://spdx.org/licenses/0BSD.html)
[![CI](https://github.com/kruddage/s7/actions/workflows/ci.yml/badge.svg)](https://github.com/kruddage/s7/actions/workflows/ci.yml)

A standalone build of [S7 Scheme](https://ccrma.stanford.edu/software/snd/snd/s7.html) —
the small, embeddable Scheme interpreter — packaged as a single self-contained program
called **`krudds7`**.

## Overview

S7 is a one-file R7RS-ish Scheme with no external dependencies, written by Bill
Schottstaedt at CCRMA/Stanford. It powers the build language and scripting layer of
[`kruddage/engine`](https://github.com/kruddage/engine), where it is embedded as a library.

This repo is the *other* deployment of the same interpreter: not a library linked into
something else, but s7 on its own, with a small C front door (`src/krudds7.c`) that gives it
a command line — a REPL, a script runner, and a one-liner evaluator. The interpreter source
is vendored verbatim and pinned by checksum; `krudds7.c` and the build/CI around it are all
this project adds.

## Building

### Prerequisites

- A C compiler (`cc`, `clang`, or `gcc`) and `libm` — nothing else.

### Build

```sh
./krudds7.sh build
```

That's the whole build: `krudds7.sh` verifies the pinned s7 sources against their checksum
(fetching them only if a file is missing — see [Vendored s7](#vendored-s7)), then compiles
`src/krudds7.c` and `third_party/s7.c` into a single `krudds7` binary. There is no configure
step and no build system to install.

## Using krudds7

```sh
# Interactive REPL (multi-line forms are read until balanced):
./krudds7

# Evaluate an expression and print its value:
./krudds7 -p '(+ 40 2)'            # => 42

# Evaluate for effect, printing nothing on its own (like ruby/perl/node -e):
./krudds7 -e '(display (* 6 7)) (newline)'

# Run a Scheme script:
./krudds7 path/to/program.scm

# Read a program from stdin:
echo '(display (map (lambda (x) (* x x)) (list 1 2 3)))' | ./krudds7 -

# Version (krudds7 and the embedded s7) / help:
./krudds7 --version
./krudds7 --help
```

`krudds7.sh` also rebuilds-if-stale and forwards arguments, so `./krudds7.sh -p '(* 6 7)'`
works from a clean checkout. An error is reported with s7's own message on stderr and yields a
non-zero exit status; the REPL reports it and keeps going.

Anything the s7 language supports works here — this is stock s7, just with a name and a front
door. For the language itself, the upstream
[s7 documentation](https://ccrma.stanford.edu/software/snd/snd/s7.html) applies unchanged.

## Embedding in Go

[`s7go/`](s7go/) is a Go binding that links the same vendored interpreter through cgo — no
prebuilt archive to download, no linker flags to wire up. `go get github.com/kruddage/s7/s7go`
and call into Scheme from Go:

```go
sc := s7.New()
defer sc.Close()
n, _ := sc.EvalInt("(+ 40 2)")   // 42

// Scheme can call back into Go, too:
sc.DefineFunc("go-add", func(a []any) (any, error) { return a[0].(int64) + a[1].(int64), nil })
sc.EvalInt("(go-add 2 3)")       // 5
```

The module root sits at the repo root so the binding can compile the single checksummed
`third_party/s7.c` (a Go module only ships files at or below its `go.mod`); the importable
package lives under `s7go/`. See [`s7go/README.md`](s7go/README.md) for the full API.

## Vendored s7

`third_party/s7.c` and `third_party/s7.h` are third-party source, committed at the pinned
commit in [`third_party/s7.artifact`](third_party/s7.artifact) and verified by
`third_party/sync.sh` before every build, so every build compiles the exact same bytes. The
pin is s7 **10.8** (17-Apr-2024). One local patch is carried, inherited from engine's copy so
the two repos vendor identical s7; it only matters on a wasm target and is inert here. The
full story — the fetch mechanism, the patch, and re-vendoring — is in
[`third_party/VENDOR.md`](third_party/VENDOR.md).

## CI

`ci.yml` runs on every pull request and on push to `main`, alongside two release workflows:

| Workflow · job | What it does |
|---|---|
| **ci · build** | Builds `krudds7` and runs the behavioural smoke test (`tools/smoke.sh`) |
| **ci · libs** | Builds the linkable `libs7` static libs (native + wasm) and link-tests the native archive, so the release products can't rot |
| **ci · sanitizers** | Rebuilds under ASan + UBSan + LeakSanitizer and re-runs the smoke test; fails on any leak, out-of-bounds, or UB |
| **pr-title** | Checks the PR title is a valid Conventional Commit (it becomes the squashed commit) |
| **release-please** | On push to `main`, maintains the release PR; on release, builds the `krudds7` binary **and the `libs7` static libs** and attaches them to the GitHub Release |

## Versioning and releases

Versioning is handled by [release-please](https://github.com/googleapis/release-please),
driven by [Conventional Commits](https://www.conventionalcommits.org/). We squash-merge, so a
PR's title *is* its commit message and the **pr-title** check enforces the format:

- `feat: …` → minor bump &nbsp;·&nbsp; `fix:`/`perf: …` → patch bump &nbsp;·&nbsp; `feat!:` or a `BREAKING CHANGE:` footer → major bump
- `chore:`/`docs:`/`ci:`/`refactor:`/`test:`/`build: …` → no version bump, but still recorded in `CHANGELOG.md`

On each push to `main`, release-please opens or updates a single **release PR** that rolls up
the unreleased commits: it bumps [`version.txt`](version.txt), regenerates `CHANGELOG.md`, and
updates `.release-please-manifest.json`. Merging that PR tags `vX.Y.Z`, cuts a GitHub Release,
and — in the same workflow run — builds and uploads the release assets (each with a matching
`.sha256`). CI reads `version.txt` and stamps it into the `krudds7` build (`KRUDDS7_VERSION`);
PR builds append a `-pr<N>+<sha>` suffix so they never collide with a real release.

### Release assets

| Asset | What it is |
|---|---|
| `krudds7-linux-x86_64` | The standalone `krudds7` CLI binary (REPL / script runner / `-p`/`-e` evaluator) |
| `krudds7-windows-x86_64.exe` | The same CLI, built natively for Windows with MinGW-w64 |
| `libs7-linux-x86_64.a` | s7 as a **native static library**, for linking the interpreter in-process |
| `libs7-windows-x86_64.a` | s7 as a **Windows static library** (MinGW-w64), for linking on Windows |
| `libs7-wasm32.a` | s7 as a **wasm32 static library** (built with `emcc`/`emar`), for linking into a wasm build |
| `s7.h` | The header every `libs7` archive exposes — one shared copy (target-independent) |

### Windows

The Windows assets are built on a `windows-latest` runner with **MinGW-w64** (MSYS2's `MINGW64`
environment), not cross-compiled from Linux — CI runs [`tools/smoke.sh`](tools/smoke.sh) against
the real `krudds7.exe`, so Windows-specific runtime behaviour is actually exercised rather than
merely compiled.

The binary is linked with `-static`, so it depends on nothing but `KERNEL32.dll` and `msvcrt.dll`
— both part of Windows — and runs on a machine with no MSYS2 or MinGW installed. CI asserts this
by checking that no import resolves inside the toolchain prefix. That check matters more than it
looks: every CI step runs *inside* MSYS2 with `/mingw64/bin` on `PATH`, so a dynamically linked
binary passes the entire smoke suite on the runner and then fails at load with
`STATUS_DLL_NOT_FOUND` on a real machine. v0.4.0 shipped exactly that bug.

MinGW rather than MSVC is deliberate. s7 and `krudds7.c` build unmodified under GCC, the archive
stays a normal `.a`, and the POSIX shell scripts that drive this repo keep working — the same
reasoning that makes a Windows port of [`kruddage/engine`](https://github.com/kruddage/engine)
(which is `-std=gnu11` throughout) tractable at all.

To build on Windows yourself, from an MSYS2 MINGW64 shell:

```sh
pacman -S mingw-w64-x86_64-gcc
./krudds7.sh build                                        # -> krudds7.exe
S7_LIB_TRIPLE=windows-x86_64 ./tools/build-lib.sh native dist
```

The `libs7-*.a` archives are the same vendored `third_party/s7.c` compiled with the same
feature defines as the CLI (`-DWITH_C_LOADER=0 -DWITH_MAIN=0`), just archived per target
instead of linked into `krudds7`. They exist so a consumer — notably
[`kruddage/engine`](https://github.com/kruddage/engine), which embeds s7 in-process in both
its native and wasm builds — can link a release asset instead of vendoring and compiling
`s7.c` itself. The `KRUDD-LOCAL PATCH` carried in `third_party/s7.c` (see
[`third_party/VENDOR.md`](third_party/VENDOR.md)) is baked into both archives, which is what
makes the wasm one safe to link. [`tools/build-lib.sh`](tools/build-lib.sh) produces these and
runs on every PR (the **ci · libs** job) so a broken archive fails before release, not at it.

> **Setup — `RELEASE_PLEASE_TOKEN`:** the release PR needs a bot token so its
> checks run. GitHub never re-triggers workflows on `GITHUB_TOKEN`-authored
> pushes, so a release PR opened with the default token never runs `ci.yml` and
> stays blocked on its required checks. Add a repo (or org) secret named
> `RELEASE_PLEASE_TOKEN` — a fine-grained PAT or GitHub App token with
> **contents: write** and **pull requests: write** on this repo — and
> release-please will use it (falling back to `GITHUB_TOKEN` when it's unset).

## License

krudds7's own files (the front door, build script, CI, and docs) are **0BSD** — the
Zero-Clause BSD license, the same maximally permissive license s7 itself uses, with no
conditions and no attribution requirement. See [`LICENSE`](LICENSE).

The vendored s7 sources keep their own upstream 0BSD notice (see
[`third_party/LICENSE.s7`](third_party/LICENSE.s7)); nothing changes at that boundary.
