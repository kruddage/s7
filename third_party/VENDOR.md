# s7 Scheme — vendored

`s7.c` / `s7.h` are third-party source, **not** krudds7-authored code, and are
committed to this repo at the pinned commit in `s7.artifact`. `sync.sh` checks
the sha256 of the committed files before any build compiles them, so every
build compiles the exact same bytes; it can also (re-)download them from the
pinned commit when re-vendoring (see Pin below). They keep their upstream
`SPDX-License-Identifier: 0BSD` header and are **not** re-stamped — s7 keeps its
own notice (see `LICENSE.s7`). This repo's own files are also 0BSD, so nothing
about the license changes at the boundary; the header just records that these
two files are Bill Schottstaedt's, not ours.

s7 is compiled straight into the `krudds7` binary (see `../src/krudds7.c`),
which embeds the interpreter and gives it a REPL and a script runner. s7 itself
is a single-file, dependency-free Scheme, so "vendored" here means the whole
interpreter — there is nothing else to build.

## Fetch

`../krudds7.sh` sources `sync.sh` before compiling anything that touches s7.
`sync.sh` is idempotent: since the committed `s7.c`/`s7.h` already match their
pinned checksum, no network I/O happens in the common case — fetching only
kicks in if the committed files are missing or a re-vendor bumped the pin.

## Pin

| Field    | Value |
|----------|-------|
| Version  | s7 **10.8** |
| Date     | 17-Apr-2024 (upstream `S7_DATE`) |
| License  | 0BSD (Zero-Clause BSD) — permissive, zero conditions |
| Upstream | https://ccrma.stanford.edu/software/snd/snd/s7.html |
| Mirror   | https://raw.githubusercontent.com/aboalang/s7, pinned by commit (see `s7.artifact`) |

s7 is a single-file, rolling release identified by version + date rather than a
git tag, so the pin is a mirror commit whose `s7.c`/`s7.h` match that
version+date exactly. Re-vendoring means bumping `s7.artifact` (version, date,
commit, checksums) — see the comment at the top of that file.

## Local patch

`s7.c` is **not byte-identical to the pinned upstream commit.** It carries one
local patch, inherited verbatim from [`kruddage/engine`](https://github.com/kruddage/engine)'s
vendored copy so the two repos compile the exact same s7. `S7_C_SHA256` in
`s7.artifact` is therefore the checksum of the *patched* file.

| Patch | Where | Why |
|---|---|---|
| Drop the `(string-ref var 0)` → `string_ref_p_p0` swap in the `p_pi` branch of the fx optimizer | `s7.c`, search `KRUDD-LOCAL PATCH` | Upstream stores an `s7_p_pp_t` and then calls it through `s7_p_pi_t`. Benign UB on a register ABI; a hard `indirect call signature mismatch` trap on **wasm**. |

**The patch is inert in `krudds7`.** krudds7 is a native binary, where the
mismatch is the benign-UB case, not the wasm trap — so the patch changes
nothing observable here. It is retained purely to keep the vendored bytes equal
to engine's pin. Re-vendoring may keep it (stay byte-identical to engine) or
drop it (take pristine upstream); either way, set `S7_C_SHA256` from whatever
`s7.c` ends up committed, or `sync.sh` will silently re-download and overwrite
it.

## License compatibility

0BSD imposes *no* downstream conditions (not even attribution), so vendoring s7
into this 0BSD repo is a no-op at the license boundary. If you fork krudds7
under a different license, 0BSD's zero conditions still impose nothing — s7
simply keeps its own header.

## Build-time feature configuration

Set in `../krudds7.sh` when compiling:

| Define            | Value | Rationale |
|-------------------|-------|-----------|
| `WITH_C_LOADER=0` | off   | Drops the `dlopen`-based external-C-library loader — surface krudds7 does not need. |
| `WITH_MAIN=0`     | off   | Keeps s7 a library; `krudds7.c` provides `main`, so s7's own `WITH_MAIN` REPL stays out of the link. |

s7 is compiled with `-w`: it is upstream code, so krudds7's warning flags are
not enforced against it.
