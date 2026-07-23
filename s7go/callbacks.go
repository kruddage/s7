// SPDX-License-Identifier: 0BSD

package s7

/*
#include <stdlib.h>
#include "s7.h"

// Declaration only: the definition lives in bridge.c. A file that uses //export
// (this one) may not contain C function definitions in its preamble, only
// declarations.
void s7go_register_dispatch(s7_scheme *sc);
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// Func is a Go function callable from Scheme, installed with (*Scheme).DefineFunc.
//
// It receives the call's arguments already converted to Go values and returns a
// value to hand back to Scheme, or an error. Returning an error raises a Scheme
// error at the call site (catchable in Scheme, and surfaced as a Go error if the
// call happened inside Eval).
//
// Argument values are one of: int64, float64, string, bool, Symbol, or []any
// (for a Scheme list, elements converted by the same rules). A Scheme value of
// any other type arrives as its textual (write) form as a string. Ratios arrive
// as float64.
//
// The return value may be nil (Scheme #<unspecified>), bool, int, int64,
// float64, string, Symbol, or []any (a list of the same). Any other type is an
// error.
type Func func(args []any) (any, error)

// Symbol is a Go representation of a Scheme symbol, distinguishing 'foo (a
// Symbol) from "foo" (a string) in both directions of a callback.
type Symbol string

// Global registry mapping a callback id to its Go function. Ids are handed out
// by a monotonic counter and are unique across all interpreters, so the single
// C dispatch entry point can route a call to the right Go function regardless of
// which Scheme instance made it. Guarded by cbMu; the per-instance id list on
// Scheme drives cleanup in Close.
var (
	cbMu  sync.Mutex
	cbReg = map[int64]Func{}
	cbSeq int64
)

var (
	cCbGensym     = C.CString(`(gensym "s7go-cb-error")`)
	cCbMarkerName = C.CString("*s7go-cb-error*")
)

// initCallbacks wires up the callback machinery for a fresh interpreter: a
// gensym error marker (so a Go error routed back through Scheme is un-forgeable)
// and the single dispatch function. Called once from New.
func (s *Scheme) initCallbacks() {
	m := C.s7_eval_c_string(s.sc, cCbGensym)
	C.s7_gc_protect(s.sc, m)
	C.s7_define_variable(s.sc, cCbMarkerName, m)
	C.s7go_register_dispatch(s.sc)
}

// DefineFunc installs a Go function under name so Scheme code can call it like
// any other procedure. Definitions persist for the life of the interpreter (or
// until name is redefined). name must be a plain Scheme identifier.
//
//	sc.DefineFunc("go-add", func(args []any) (any, error) {
//	    return args[0].(int64) + args[1].(int64), nil
//	})
//	sc.EvalInt("(go-add 2 3)") // 5, nil
func (s *Scheme) DefineFunc(name string, fn Func) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if fn == nil {
		return errors.New("s7: DefineFunc requires a non-nil function")
	}
	if !validIdent(name) {
		return fmt.Errorf("s7: %q is not a valid Scheme identifier", name)
	}

	cbMu.Lock()
	cbSeq++
	id := cbSeq
	cbReg[id] = fn
	cbMu.Unlock()

	// Install a thin Scheme wrapper that tags the call with its id and, on the
	// error path, raises in Scheme space. The dispatch function must never
	// longjmp (s7_error) itself: it runs on a cgo-exported Go frame, and a
	// longjmp across it would corrupt the Go runtime. So a Go error comes back
	// as a (marker . message) pair and the wrapper — pure Scheme — raises it.
	def := fmt.Sprintf(`(define (%s . s7go-args)
  (let ((s7go-r (s7go-dispatch %d s7go-args)))
    (if (and (pair? s7go-r) (eq? (car s7go-r) *s7go-cb-error*))
        (error 'go-error "~A" (cdr s7go-r))
        s7go-r)))`, name, id)

	if _, err := s.evalRaw(def); err != nil {
		cbMu.Lock()
		delete(cbReg, id)
		cbMu.Unlock()
		return err
	}
	s.cbIDs = append(s.cbIDs, id)
	return nil
}

//export s7goDispatch
func s7goDispatch(sc *C.s7_scheme, args C.s7_pointer) C.s7_pointer {
	// args is (id . (arg-list . ())): the id, then the caller's argument list.
	id := int64(C.s7_integer(C.s7_car(args)))
	argList := C.s7_car(C.s7_cdr(args))

	cbMu.Lock()
	fn := cbReg[id]
	cbMu.Unlock()
	if fn == nil {
		return cbErrorPair(sc, "internal error: unknown s7go callback id")
	}

	res, err := fn(fromSchemeList(sc, argList))
	if err != nil {
		return cbErrorPair(sc, err.Error())
	}
	out, cerr := toScheme(sc, res)
	if cerr != nil {
		return cbErrorPair(sc, cerr.Error())
	}
	return out
}

// cbErrorPair builds the (marker . message) pair the Scheme wrapper recognizes
// as "the Go callback failed". The marker is fetched from this interpreter's
// *s7go-cb-error* binding (permanently GC-protected in initCallbacks), so no
// per-callback state is needed to find it.
func cbErrorPair(sc *C.s7_scheme, msg string) C.s7_pointer {
	marker := C.s7_name_to_value(sc, cCbMarkerName)
	str := makeString(sc, msg)
	loc := C.s7_gc_protect(sc, str)
	pair := C.s7_cons(sc, marker, str)
	C.s7_gc_unprotect_at(sc, loc)
	return pair
}

// fromSchemeList converts a proper Scheme list to a []any. An improper tail is
// ignored.
func fromSchemeList(sc *C.s7_scheme, lst C.s7_pointer) []any {
	out := []any{}
	for bool(C.s7_is_pair(lst)) {
		out = append(out, fromScheme(sc, C.s7_car(lst)))
		lst = C.s7_cdr(lst)
	}
	return out
}

// fromScheme converts a single Scheme value to a Go value per Func's contract.
func fromScheme(sc *C.s7_scheme, v C.s7_pointer) any {
	switch {
	case bool(C.s7_is_integer(v)): // integer? before real? (integers are real)
		return int64(C.s7_integer(v))
	case bool(C.s7_is_real(v)):
		return float64(C.s7_number_to_real(sc, v))
	case bool(C.s7_is_string(v)):
		return C.GoString(C.s7_string(v))
	case bool(C.s7_is_boolean(v)):
		return bool(C.s7_boolean(sc, v))
	case bool(C.s7_is_null(sc, v)):
		return []any{}
	case bool(C.s7_is_pair(v)):
		return fromSchemeList(sc, v)
	case bool(C.s7_is_symbol(v)):
		return Symbol(C.GoString(C.s7_symbol_name(v)))
	default:
		// Anything else (char, hash-table, procedure, …) arrives as its write
		// form, so a callback never receives a silently-dropped argument.
		cs := C.s7_object_to_c_string(sc, v)
		if cs == nil {
			return nil
		}
		defer C.free(unsafe.Pointer(cs))
		return C.GoString(cs)
	}
}

// toScheme converts a Go value returned by a callback to a Scheme value. List
// construction protects intermediate cells from the GC, since a partially-built
// list held only in a Go/C local is not a GC root.
func toScheme(sc *C.s7_scheme, v any) (C.s7_pointer, error) {
	switch x := v.(type) {
	case nil:
		return C.s7_unspecified(sc), nil
	case bool:
		if x {
			return C.s7_t(sc), nil
		}
		return C.s7_f(sc), nil
	case int:
		return C.s7_make_integer(sc, C.s7_int(x)), nil
	case int64:
		return C.s7_make_integer(sc, C.s7_int(x)), nil
	case float64:
		return C.s7_make_real(sc, C.s7_double(x)), nil
	case string:
		return makeString(sc, x), nil
	case Symbol:
		cs := C.CString(string(x))
		defer C.free(unsafe.Pointer(cs))
		return C.s7_make_symbol(sc, cs), nil
	case []any:
		return toSchemeList(sc, x)
	default:
		return nil, fmt.Errorf("s7: cannot convert Go value of type %T to Scheme", v)
	}
}

// toSchemeList builds a proper Scheme list from xs. Each converted element is
// GC-protected until the list holding it is itself protected, and the
// accumulator's protected slot is updated as it grows, so a GC triggered by any
// allocation mid-build cannot collect a cell that is already part of the result.
func toSchemeList(sc *C.s7_scheme, xs []any) (C.s7_pointer, error) {
	elems := make([]C.s7_pointer, len(xs))
	locs := make([]C.s7_int, 0, len(xs))
	unprotectAll := func() {
		for _, l := range locs {
			C.s7_gc_unprotect_at(sc, l)
		}
	}

	for i, x := range xs {
		e, err := toScheme(sc, x)
		if err != nil {
			unprotectAll()
			return nil, err
		}
		elems[i] = e
		locs = append(locs, C.s7_gc_protect(sc, e))
	}

	acc := C.s7_nil(sc)
	accLoc := C.s7_gc_protect(sc, acc)
	for i := len(elems) - 1; i >= 0; i-- {
		acc = C.s7_cons(sc, elems[i], acc)
		C.s7_gc_protect_via_location(sc, acc, accLoc)
	}
	C.s7_gc_unprotect_at(sc, accLoc)
	unprotectAll()
	return acc, nil
}

// makeString builds a Scheme string from a Go string, preserving length so
// embedded NULs survive.
func makeString(sc *C.s7_scheme, str string) C.s7_pointer {
	var cbuf unsafe.Pointer
	if len(str) > 0 {
		cbuf = C.CBytes([]byte(str))
		defer C.free(cbuf)
	}
	return C.s7_make_string_with_length(sc, (*C.char)(cbuf), C.s7_int(len(str)))
}

// validIdent reports whether name is safe to splice into a (define (name ...))
// form: non-empty and free of whitespace and the delimiter characters that
// would end the identifier or the surrounding form.
func validIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || strings.ContainsRune("()[]{}\"';`,|\\#", r) {
			return false
		}
	}
	return true
}
