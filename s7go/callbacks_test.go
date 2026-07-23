// SPDX-License-Identifier: 0BSD

package s7

import (
	"errors"
	"strings"
	"testing"
)

func TestDefineFuncBasic(t *testing.T) {
	sc := New()
	defer sc.Close()

	err := sc.DefineFunc("go-add", func(args []any) (any, error) {
		return args[0].(int64) + args[1].(int64), nil
	})
	if err != nil {
		t.Fatalf("DefineFunc: %v", err)
	}

	if n, err := sc.EvalInt("(go-add 2 3)"); err != nil || n != 5 {
		t.Errorf("(go-add 2 3) = %d, %v; want 5, nil", n, err)
	}
	// Callable from within other Scheme code, repeatedly.
	if n, err := sc.EvalInt("(apply + (map (lambda (x) (go-add x 1)) '(10 20 30)))"); err != nil || n != 63 {
		t.Errorf("mapped go-add = %d, %v; want 63, nil", n, err)
	}
}

func TestDefineFuncArgTypes(t *testing.T) {
	sc := New()
	defer sc.Close()

	var got []any
	sc.DefineFunc("capture", func(args []any) (any, error) {
		got = args
		return nil, nil
	})
	if _, err := sc.Eval(`(capture 42 3.5 "hi" #t 'sym '(1 2))`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("got %d args, want 6: %#v", len(got), got)
	}
	if got[0].(int64) != 42 {
		t.Errorf("arg0 = %#v, want int64(42)", got[0])
	}
	if got[1].(float64) != 3.5 {
		t.Errorf("arg1 = %#v, want float64(3.5)", got[1])
	}
	if got[2].(string) != "hi" {
		t.Errorf("arg2 = %#v, want \"hi\"", got[2])
	}
	if got[3].(bool) != true {
		t.Errorf("arg3 = %#v, want true", got[3])
	}
	if got[4].(Symbol) != "sym" {
		t.Errorf("arg4 = %#v, want Symbol(\"sym\")", got[4])
	}
	lst, ok := got[5].([]any)
	if !ok || len(lst) != 2 || lst[0].(int64) != 1 || lst[1].(int64) != 2 {
		t.Errorf("arg5 = %#v, want []any{1,2}", got[5])
	}
}

func TestDefineFuncReturnTypes(t *testing.T) {
	sc := New()
	defer sc.Close()

	cases := []struct {
		name string
		ret  any
		call string
		want string
	}{
		{"r-int", int64(7), "(r-int)", "7"},
		{"r-float", 2.5, "(r-float)", "2.5"},
		{"r-str", "hello", "(r-str)", `"hello"`},
		{"r-bool", false, "(r-bool)", "#f"},
		{"r-sym", Symbol("foo"), "(r-sym)", "foo"},
		{"r-list", []any{int64(1), "two", true}, "(r-list)", `(1 "two" #t)`},
		{"r-nil", nil, "(r-nil)", ""}, // unspecified -> empty
	}
	for _, c := range cases {
		ret := c.ret
		if err := sc.DefineFunc(c.name, func(args []any) (any, error) { return ret, nil }); err != nil {
			t.Fatalf("DefineFunc(%s): %v", c.name, err)
		}
		got, err := sc.Eval(c.call)
		if err != nil {
			t.Errorf("%s: %v", c.call, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.call, got, c.want)
		}
	}
}

func TestDefineFuncRoundTripList(t *testing.T) {
	sc := New()
	defer sc.Close()

	// Take a list, double each element, hand it back — exercises list in and out.
	sc.DefineFunc("go-double-all", func(args []any) (any, error) {
		in := args[0].([]any)
		out := make([]any, len(in))
		for i, v := range in {
			out[i] = v.(int64) * 2
		}
		return out, nil
	})
	got, err := sc.Eval("(go-double-all '(1 2 3 4))")
	if err != nil {
		t.Fatal(err)
	}
	if got != "(2 4 6 8)" {
		t.Errorf("= %q, want (2 4 6 8)", got)
	}
}

func TestDefineFuncError(t *testing.T) {
	sc := New()
	defer sc.Close()

	sc.DefineFunc("go-boom", func(args []any) (any, error) {
		return nil, errors.New("kaboom")
	})

	// A Go error surfaces as a Go error out of Eval, carrying the message.
	_, err := sc.Eval("(go-boom)")
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("Eval error = %v; want one containing \"kaboom\"", err)
	}

	// And it's an ordinary catchable Scheme error, not a process-killer.
	got, err := sc.Eval(`(catch #t (lambda () (go-boom)) (lambda (tag info) "caught"))`)
	if err != nil {
		t.Fatalf("catch around go-boom: %v", err)
	}
	if got != `"caught"` {
		t.Errorf("caught = %q, want \"caught\"", got)
	}

	// The interpreter keeps working after a callback error.
	if n, err := sc.EvalInt("(+ 1 1)"); err != nil || n != 2 {
		t.Errorf("after callback error: %d, %v", n, err)
	}
}

func TestDefineFuncBadReturn(t *testing.T) {
	sc := New()
	defer sc.Close()

	sc.DefineFunc("go-bad", func(args []any) (any, error) {
		return map[string]int{"x": 1}, nil // unsupported type
	})
	_, err := sc.Eval("(go-bad)")
	if err == nil || !strings.Contains(err.Error(), "cannot convert") {
		t.Errorf("Eval = %v; want a conversion error", err)
	}
}

func TestDefineFuncValidation(t *testing.T) {
	sc := New()
	defer sc.Close()

	if err := sc.DefineFunc("", func([]any) (any, error) { return nil, nil }); err == nil {
		t.Error("empty name: expected error")
	}
	if err := sc.DefineFunc("bad name", func([]any) (any, error) { return nil, nil }); err == nil {
		t.Error("name with space: expected error")
	}
	if err := sc.DefineFunc("go-x", nil); err == nil {
		t.Error("nil func: expected error")
	}
}

func TestDefineFuncClosedCleanup(t *testing.T) {
	sc := New()
	sc.DefineFunc("go-x", func([]any) (any, error) { return int64(1), nil })

	cbMu.Lock()
	before := len(cbReg)
	cbMu.Unlock()
	if before == 0 {
		t.Fatal("expected a registered callback")
	}

	sc.Close()

	cbMu.Lock()
	after := len(cbReg)
	cbMu.Unlock()
	if after != before-1 {
		t.Errorf("registry size after Close = %d, want %d", after, before-1)
	}

	if err := sc.DefineFunc("go-y", func([]any) (any, error) { return nil, nil }); err != ErrClosed {
		t.Errorf("DefineFunc after Close = %v, want ErrClosed", err)
	}
}

// TestDefineFuncGCStress exercises the GC-protection in toSchemeList: a callback
// returns freshly-built lists while Scheme forces collection between calls and
// churns the heap, so any unprotected intermediate cell would be reclaimed and
// corrupt the result (or crash).
func TestDefineFuncGCStress(t *testing.T) {
	sc := New()
	defer sc.Close()

	sc.DefineFunc("go-range", func(args []any) (any, error) {
		n := int(args[0].(int64))
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = int64(i) // each element is a fresh Scheme allocation
		}
		return out, nil
	})

	// Build a list of 200, force gc, sum it; repeat many times with heap churn.
	got, err := sc.EvalInt(`
      (let loop ((i 0) (total 0))
        (if (= i 500)
            total
            (begin
              (gc)
              (let* ((junk (make-list 100 'x))    ; churn the heap
                     (lst (go-range 200))
                     (s (apply + lst)))
                (loop (+ i 1) (+ total (if (= s 19900) 1 0)))))))`)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Errorf("correct sums = %d/500 (a wrong sum means a collected cell)", got)
	}
}
