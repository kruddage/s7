#!/bin/sh
# SPDX-License-Identifier: 0BSD
#
# tools/smoke.sh — a behavioural smoke test for the krudds7 binary.
#
# Exercises each front-door mode (-p, -e, script file, stdin) and the things
# most likely to regress: cross-form global definitions, error reporting +
# non-zero exit, and the interactive-vs-piped echo split. Not a unit test of s7
# itself (that's upstream's job) — a check that our wrapper behaves.
#
#   sh tools/smoke.sh ./krudds7
set -eu

bin=${1:-./krudds7}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

check() {
	# check "label" "expected" "actual"
	if [ "$2" = "$3" ]; then
		echo "ok   - $1"
	else
		echo "FAIL - $1"
		echo "       expected: [$2]"
		echo "       actual:   [$3]"
		fails=$((fails + 1))
	fi
}

# -p evaluates and prints the value.
check "-p arithmetic" "42" "$("$bin" -p '(+ 40 2)')"
check "-p string"     '"abcd"' "$("$bin" -p '(string-append "ab" "cd")')"

# -e runs for effect and prints nothing on its own.
check "-e side effect only" "hi" "$("$bin" -e '(display "hi")')"
check "-e no auto-print"     ""   "$("$bin" -e '(+ 1 2)')"

# A top-level define in one form must be visible to a later form (global scope,
# not swallowed by the catch wrapper).
prog='(define (sq x) (* x x))
(display (sq 9))'
check "cross-form global define" "81" "$("$bin" -e "$prog")"

# Script file.
printf '(define n 10)\n(display (* n n))\n' > "$tmp/s.scm"
check "script file" "100" "$("$bin" "$tmp/s.scm")"

# Program on stdin (piped: runs like a script, no echoed values).
check "stdin program" "7" "$(printf '(display (+ 3 4))\n' | "$bin" -)"

# --version prints both krudds7 and the embedded s7 version.
case "$("$bin" --version)" in
	krudds7\ *s7\ *) echo "ok   - --version format" ;;
	*) echo "FAIL - --version format"; fails=$((fails + 1)) ;;
esac

# An error is reported and yields a non-zero exit status.
if "$bin" -e '(car 1)' >/dev/null 2>&1; then
	echo "FAIL - error exits non-zero"; fails=$((fails + 1))
else
	echo "ok   - error exits non-zero"
fi

# A clean run exits zero.
if "$bin" -e '(+ 1 1)' >/dev/null 2>&1; then
	echo "ok   - success exits zero"
else
	echo "FAIL - success exits zero"; fails=$((fails + 1))
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "smoke: all checks passed"
else
	echo "smoke: $fails check(s) failed"
	exit 1
fi
