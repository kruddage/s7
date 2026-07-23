// SPDX-License-Identifier: 0BSD

// Package s7 embeds the s7 Scheme interpreter in a Go program.
//
// s7 is a small, R7RS-ish Scheme written by Bill Schottstaedt (CCRMA/Stanford).
// This package links the exact interpreter the krudds7 CLI ships — the same
// vendored, checksum-pinned third_party/s7.c compiled with the same feature
// defines — through cgo. There is no shared library to install and nothing to
// download at build time: cgo compiles s7 straight into your binary. A C
// toolchain and CGO_ENABLED=1 (the default) are the only requirements.
//
// A Scheme holds one interpreter. It is safe for concurrent use — every method
// serializes on an internal mutex — but that also means calls do not run in
// parallel; use one Scheme per goroutine if you want real concurrency.
//
//	sc := s7.New()
//	defer sc.Close()
//
//	n, err := sc.EvalInt("(+ 40 2)")   // 42, nil
//	s, err := sc.Eval("(map (lambda (x) (* x x)) '(1 2 3))")  // "(1 4 9)", nil
//
// Errors raised inside Scheme (unbound variables, wrong types, explicit
// (error ...)) are caught and returned as a Go error carrying s7's own
// formatted message, rather than being printed or crashing the process.
package s7

/*
#cgo CFLAGS: -I${SRCDIR}/../third_party -DWITH_C_LOADER=0 -DWITH_MAIN=0 -w -O2
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include "s7.h"

// Expose s7's compile-time version macros as functions so Go can read them
// without depending on cgo's handling of string-valued macros.
static const char *s7go_version(void) { return S7_VERSION; }
static const char *s7go_date(void)    { return S7_DATE; }
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ErrClosed is returned by every method of a Scheme after Close has been called.
var ErrClosed = errors.New("s7: interpreter is closed")

// Scheme is a single s7 interpreter instance. Create one with New and release
// it with Close. The zero value is not usable.
type Scheme struct {
	mu       sync.Mutex
	sc       *C.s7_scheme
	marker   C.s7_pointer // gensym distinguishing a caught error from a real value
	evalWrap *C.char      // cached C string of the eval wrapper
	srcName  *C.char      // cached C string "*s7go-src*"
	closed   bool
}

// The evaluation wrapper. It reads every top-level form out of the string bound
// to *s7go-src* and evaluates each in the global environment, returning the last
// value — mirroring how the krudds7 CLI evaluates a program so behaviour matches.
//
// The whole loop runs inside (catch #t ...). On error the handler returns a pair
// (marker . "formatted message") so the Go side can tell "raised an error" apart
// from "legitimately returned a value" via an un-forgeable gensym marker. The
// inner catch around format guarantees the message slot is always a string even
// if the error info is not a well-formed format arglist.
const evalWrapSrc = `(catch #t
  (lambda ()
    (let ((p (open-input-string *s7go-src*)))
      (let loop ((form (read p)) (val (if #f #f)))
        (if (eof-object? form)
            val
            (loop (read p) (eval form (rootlet)))))))
  (lambda (tag info)
    (cons *s7go-error-marker*
          (catch #t
            (lambda () (apply format #f info))
            (lambda args (object->string tag))))))`

// New creates and initializes a fresh s7 interpreter. It never returns nil; if
// s7 cannot be initialized it panics, since that indicates the process is out of
// memory and nothing useful can follow.
func New() *Scheme {
	sc := C.s7_init()
	if sc == nil {
		panic("s7: s7_init failed (out of memory)")
	}

	s := &Scheme{
		sc:       sc,
		evalWrap: C.CString(evalWrapSrc),
		srcName:  C.CString("*s7go-src*"),
	}

	// A gensym error marker: nothing an evaluated form can produce is eq? to
	// it, so it reliably flags the "an error was caught" path. Protect it from
	// the GC and expose it to the wrapper under a well-known name.
	marker := C.s7_eval_c_string(sc, cGensym)
	C.s7_gc_protect(sc, marker)
	s.marker = marker
	C.s7_define_variable(sc, cMarkerName, marker)

	runtime.SetFinalizer(s, (*Scheme).Close)
	return s
}

// Cached C strings used at startup. Defined once to avoid per-call allocation.
var (
	cGensym     = C.CString(`(gensym "s7go-error")`)
	cMarkerName = C.CString("*s7go-error-marker*")
)

// Close frees the interpreter and all memory it owns. It is safe to call more
// than once; subsequent calls are no-ops. After Close, every other method
// returns ErrClosed. A Scheme is also finalized automatically if it becomes
// unreachable without Close, but relying on the finalizer is discouraged.
func (s *Scheme) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	runtime.SetFinalizer(s, nil)
	C.s7_free(s.sc)
	C.free(unsafe.Pointer(s.evalWrap))
	C.free(unsafe.Pointer(s.srcName))
	s.sc = nil
	return nil
}

// evalRaw evaluates code and returns the raw s7 value of the last form. The
// caller must hold s.mu and must consume the result before making any further
// s7 allocation, since the value is not rooted against garbage collection.
func (s *Scheme) evalRaw(code string) (C.s7_pointer, error) {
	if s.closed {
		return nil, ErrClosed
	}

	// Inject the source as a Scheme string bound to *s7go-src*. Passing it as a
	// value (rather than splicing it into the wrapper text) means no escaping is
	// needed and embedded NULs survive.
	var cbuf unsafe.Pointer
	if len(code) > 0 {
		cbuf = C.CBytes([]byte(code))
		defer C.free(cbuf)
	}
	src := C.s7_make_string_with_length(s.sc, (*C.char)(cbuf), C.s7_int(len(code)))
	C.s7_define_variable(s.sc, s.srcName, src)

	res := C.s7_eval_c_string(s.sc, s.evalWrap)
	if bool(C.s7_is_pair(res)) && bool(C.s7_is_eq(C.s7_car(res), s.marker)) {
		return nil, errors.New(s.errMessage(res))
	}
	return res, nil
}

// errMessage extracts the formatted error string from a (marker . message) pair.
func (s *Scheme) errMessage(pair C.s7_pointer) string {
	msg := C.s7_cdr(pair)
	if bool(C.s7_is_string(msg)) {
		return "s7: " + C.GoString(C.s7_string(msg))
	}
	return "s7: error"
}

// writeForm returns the external (write) representation of v, e.g. 42, "hi"
// (with quotes), (1 2 3). The returned C string is owned by us and freed here.
func (s *Scheme) writeForm(v C.s7_pointer) string {
	cs := C.s7_object_to_c_string(s.sc, v)
	if cs == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cs))
	return C.GoString(cs)
}

// Eval evaluates one or more Scheme forms and returns the external (write)
// representation of the last value: `42`, `"hi"` (quoted), `(1 2 3)`. Use the
// typed Eval* helpers when you want a Go int/float/bool/string instead. A Scheme
// error is returned as a Go error and the string result is empty.
func (s *Scheme) Eval(code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.evalRaw(code)
	if err != nil {
		return "", err
	}
	if res == C.s7_unspecified(s.sc) {
		return "", nil
	}
	return s.writeForm(res), nil
}

// EvalInt evaluates code and returns the result as an int64. It errors if the
// result is not an integer.
func (s *Scheme) EvalInt(code string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.evalRaw(code)
	if err != nil {
		return 0, err
	}
	if !bool(C.s7_is_integer(res)) {
		return 0, fmt.Errorf("s7: result is not an integer: %s", s.writeForm(res))
	}
	return int64(C.s7_integer(res)), nil
}

// EvalFloat evaluates code and returns the result as a float64. Any Scheme
// number is accepted (integers and ratios are converted); a non-number errors.
func (s *Scheme) EvalFloat(code string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.evalRaw(code)
	if err != nil {
		return 0, err
	}
	if !bool(C.s7_is_number(res)) {
		return 0, fmt.Errorf("s7: result is not a number: %s", s.writeForm(res))
	}
	return float64(C.s7_number_to_real(s.sc, res)), nil
}

// EvalBool evaluates code and returns its Scheme truth value: only #f is false,
// every other value (including 0 and the empty list) is true.
func (s *Scheme) EvalBool(code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.evalRaw(code)
	if err != nil {
		return false, err
	}
	return bool(C.s7_boolean(s.sc, res)), nil
}

// EvalString evaluates code, requires the result to be a Scheme string, and
// returns its contents (no surrounding quotes). A non-string result errors — use
// Eval if you want the write form of an arbitrary value.
func (s *Scheme) EvalString(code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.evalRaw(code)
	if err != nil {
		return "", err
	}
	if !bool(C.s7_is_string(res)) {
		return "", fmt.Errorf("s7: result is not a string: %s", s.writeForm(res))
	}
	return C.GoString(C.s7_string(res)), nil
}

// LoadFile loads and evaluates a Scheme file by path, as (load path) would. A
// missing file or an error raised while loading is returned as a Go error.
func (s *Scheme) LoadFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	// Route through evalRaw so file-load errors are caught and reported the same
	// way as Eval errors, instead of printing to stderr or longjmp'ing out.
	cpath := escapeForScheme(path)
	_, err := s.evalRaw("(load " + cpath + ")")
	return err
}

// Version returns the embedded s7 version and build date, e.g.
// "10.8 (17-Apr-2024)". It reflects the vendored interpreter this package was
// compiled against and does not depend on any interpreter instance.
func Version() string {
	return C.GoString(C.s7go_version()) + " (" + C.GoString(C.s7go_date()) + ")"
}

// escapeForScheme renders a Go string as a Scheme string literal, escaping the
// two characters that matter inside "..." — backslash and double-quote.
func escapeForScheme(str string) string {
	out := make([]byte, 0, len(str)+2)
	out = append(out, '"')
	for i := 0; i < len(str); i++ {
		if str[i] == '\\' || str[i] == '"' {
			out = append(out, '\\')
		}
		out = append(out, str[i])
	}
	out = append(out, '"')
	return string(out)
}
