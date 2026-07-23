// SPDX-License-Identifier: 0BSD

package s7

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEval(t *testing.T) {
	sc := New()
	defer sc.Close()

	cases := []struct{ code, want string }{
		{"(+ 40 2)", "42"},
		{"(map (lambda (x) (* x x)) '(1 2 3))", "(1 4 9)"},
		{`(string-append "foo" "bar")`, `"foobar"`},
		{"(list 1 2 3)", "(1 2 3)"},
		{"(if #f #f)", ""}, // unspecified -> empty string
	}
	for _, c := range cases {
		got, err := sc.Eval(c.code)
		if err != nil {
			t.Errorf("Eval(%q) error: %v", c.code, err)
			continue
		}
		if got != c.want {
			t.Errorf("Eval(%q) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestMultipleForms(t *testing.T) {
	sc := New()
	defer sc.Close()

	// Definitions must persist across forms and across Eval calls (global env).
	if _, err := sc.Eval("(define x 10) (define (double n) (* n 2))"); err != nil {
		t.Fatalf("define: %v", err)
	}
	got, err := sc.EvalInt("(double x)")
	if err != nil {
		t.Fatalf("EvalInt: %v", err)
	}
	if got != 20 {
		t.Errorf("(double x) = %d, want 20", got)
	}
}

func TestTypedEvals(t *testing.T) {
	sc := New()
	defer sc.Close()

	if n, err := sc.EvalInt("(* 6 7)"); err != nil || n != 42 {
		t.Errorf("EvalInt = %d, %v; want 42, nil", n, err)
	}
	if f, err := sc.EvalFloat("(/ 3 2)"); err != nil || f != 1.5 {
		t.Errorf("EvalFloat = %v, %v; want 1.5, nil", f, err)
	}
	if f, err := sc.EvalFloat("(+ 1 2)"); err != nil || f != 3.0 {
		t.Errorf("EvalFloat(int) = %v, %v; want 3, nil", f, err)
	}
	if b, err := sc.EvalBool("(> 3 2)"); err != nil || !b {
		t.Errorf("EvalBool = %v, %v; want true, nil", b, err)
	}
	if b, err := sc.EvalBool("#f"); err != nil || b {
		t.Errorf("EvalBool(#f) = %v, %v; want false, nil", b, err)
	}
	if s, err := sc.EvalString(`(symbol->string 'hello)`); err != nil || s != "hello" {
		t.Errorf("EvalString = %q, %v; want \"hello\", nil", s, err)
	}
}

func TestTypeMismatch(t *testing.T) {
	sc := New()
	defer sc.Close()

	if _, err := sc.EvalInt(`"not a number"`); err == nil {
		t.Error("EvalInt of a string: expected error, got nil")
	}
	if _, err := sc.EvalString("42"); err == nil {
		t.Error("EvalString of an integer: expected error, got nil")
	}
}

func TestSchemeErrors(t *testing.T) {
	sc := New()
	defer sc.Close()

	for _, code := range []string{
		"(this-is-not-defined)",
		"(car 5)",
		`(error 'my-tag "boom ~A" 42)`,
		"(+ 1", // unbalanced / read error
	} {
		if _, err := sc.Eval(code); err == nil {
			t.Errorf("Eval(%q): expected error, got nil", code)
		}
	}

	// A recoverable error must not wedge the interpreter: it keeps working.
	if _, err := sc.Eval("(undefined-thing)"); err == nil {
		t.Fatal("expected error")
	}
	if got, err := sc.Eval("(+ 1 2)"); err != nil || got != "3" {
		t.Errorf("after error, Eval = %q, %v; want 3, nil", got, err)
	}
}

func TestLoadFile(t *testing.T) {
	sc := New()
	defer sc.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "prog.scm")
	if err := os.WriteFile(path, []byte("(define loaded-value 123)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sc.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if n, err := sc.EvalInt("loaded-value"); err != nil || n != 123 {
		t.Errorf("loaded-value = %d, %v; want 123, nil", n, err)
	}

	if err := sc.LoadFile(filepath.Join(dir, "does-not-exist.scm")); err == nil {
		t.Error("LoadFile of missing file: expected error, got nil")
	}
}

func TestClosed(t *testing.T) {
	sc := New()
	sc.Close()
	sc.Close() // idempotent

	if _, err := sc.Eval("(+ 1 1)"); err != ErrClosed {
		t.Errorf("Eval after Close = %v; want ErrClosed", err)
	}
}

func TestVersion(t *testing.T) {
	if v := Version(); !strings.Contains(v, ".") {
		t.Errorf("Version() = %q, want something like \"10.8 (17-Apr-2024)\"", v)
	}
}

// Separate instances are independent and can be used from separate goroutines.
func TestConcurrentInstances(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sc := New()
			defer sc.Close()
			if _, err := sc.EvalInt("(+ 1 1)"); err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}
