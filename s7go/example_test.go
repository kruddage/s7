// SPDX-License-Identifier: 0BSD

package s7_test

import (
	"fmt"

	s7 "github.com/kruddage/s7/s7go"
)

func Example() {
	sc := s7.New()
	defer sc.Close()

	// Define some Scheme, then call into it from Go.
	if _, err := sc.Eval("(define (fib n) (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2)))))"); err != nil {
		panic(err)
	}

	n, err := sc.EvalInt("(fib 10)")
	if err != nil {
		panic(err)
	}
	fmt.Println(n)

	// Any value comes back as its write form via Eval.
	out, _ := sc.Eval("(map (lambda (x) (* x x)) '(1 2 3 4))")
	fmt.Println(out)

	// Output:
	// 55
	// (1 4 9 16)
}
