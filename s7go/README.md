# s7go — s7 Scheme for Go

`s7go` embeds the [s7 Scheme](https://ccrma.stanford.edu/software/snd/snd/s7.html)
interpreter in a Go program via cgo. It links the **exact** interpreter the
`krudds7` CLI ships — the same vendored, checksum-pinned
[`third_party/s7.c`](../third_party/s7.c) compiled with the same feature
defines — so the Scheme you run from Go behaves identically to the standalone
binary.

There is no shared library to install and nothing fetched at build time: cgo
compiles s7 straight into your binary, the same way
[`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) embeds SQLite. A C
compiler and `CGO_ENABLED=1` (the default) are the only requirements.

## Install

```sh
go get github.com/kruddage/s7/s7go
```

## Use

```go
package main

import (
	"fmt"

	s7 "github.com/kruddage/s7/s7go"
)

func main() {
	sc := s7.New()
	defer sc.Close()

	sc.Eval("(define (fib n) (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2)))))")

	n, _ := sc.EvalInt("(fib 20)")   // 6765
	fmt.Println(n)

	out, _ := sc.Eval("(map (lambda (x) (* x x)) '(1 2 3))")  // "(1 4 9)"
	fmt.Println(out)
}
```

## API

| Method | Returns |
|---|---|
| `New() *Scheme` | A fresh interpreter (panics only on OOM). |
| `(*Scheme) Eval(code) (string, error)` | Write form of the last value: `42`, `"hi"` (quoted), `(1 2 3)`. |
| `(*Scheme) EvalInt(code) (int64, error)` | Integer result, or an error if it isn't one. |
| `(*Scheme) EvalFloat(code) (float64, error)` | Any number as a float64. |
| `(*Scheme) EvalBool(code) (bool, error)` | Scheme truth value (only `#f` is false). |
| `(*Scheme) EvalString(code) (string, error)` | Contents of a Scheme string result (no quotes). |
| `(*Scheme) DefineFunc(name, fn) error` | Register a Go function callable from Scheme (see below). |
| `(*Scheme) LoadFile(path) error` | `(load path)`, with errors returned rather than printed. |
| `(*Scheme) Close() error` | Free the interpreter (idempotent; also finalized on GC). |
| `Version() string` | Embedded s7 version, e.g. `10.8 (17-Apr-2024)`. |

## Calling Go from Scheme

`DefineFunc` installs a Go function that Scheme can call like any other
procedure:

```go
sc.DefineFunc("go-add", func(args []any) (any, error) {
	return args[0].(int64) + args[1].(int64), nil
})

n, _ := sc.EvalInt("(go-add 2 3)")   // 5
```

The callback signature is `func(args []any) (any, error)`. Values cross the
boundary as these Go types:

| Scheme | Go |
|---|---|
| integer | `int64` |
| real / ratio | `float64` |
| string | `string` |
| boolean | `bool` |
| symbol | `s7.Symbol` (distinct from `string`, both directions) |
| list | `[]any` (elements converted recursively) |
| anything else (arg) | its write form as a `string` |

Return `nil` for Scheme `#<unspecified>`. Returning a Go value of an
unsupported type is an error. Returning an `error` **raises a Scheme error** at
the call site — catchable in Scheme with `catch`, and surfaced as a Go `error`
if the call happened inside `Eval`. A callback never crashes the process, and
the interpreter stays usable afterward.

Errors raised inside Scheme — unbound variables, wrong-type arguments, explicit
`(error ...)`, read errors — are caught and returned as a Go `error` carrying
s7's own formatted message. They never print to stderr or crash the process, and
the interpreter stays usable afterward. Definitions persist across `Eval` calls
because every form is evaluated in the global environment.

## Concurrency

One `Scheme` wraps one interpreter. Every method serializes on an internal
mutex, so a `Scheme` is safe to share across goroutines — but calls do not run
in parallel. For real parallelism, create one `Scheme` per goroutine; separate
instances are fully independent.

## Building

cgo compiles the vendored amalgamation once and caches it. Because it is a cgo
package:

- `CGO_ENABLED=1` is required (it is the default for native builds).
- A C toolchain must be available (`cc`/`clang`/`gcc`).
- Pure-Go cross-compilation (`CGO_ENABLED=0`) is not supported; cross-compiling
  needs a C cross-toolchain via `CC`.

## License

`s7go`'s own files are **0BSD**, matching the rest of this repo. The vendored s7
sources keep their upstream 0BSD notice (see
[`../third_party/LICENSE.s7`](../third_party/LICENSE.s7)).
