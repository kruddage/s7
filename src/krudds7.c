/* SPDX-License-Identifier: 0BSD */
/*
 * krudds7 — the front door to s7 Scheme.
 *
 * This file is the whole program: it embeds the vendored s7 interpreter
 * (../third_party/s7.c, see ../third_party/VENDOR.md) and gives it a command
 * line — a REPL when run interactively, a script runner when handed files, and
 * a one-liner evaluator with -e. s7 is compiled as a library (WITH_MAIN=0), so
 * this main() is the only entry point; the insides are all s7's.
 *
 * Built by ../krudds7.sh with the system C compiler; the version string is
 * stamped in through -DKRUDDS7_VERSION at build time (from version.txt).
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
/*
 * isatty()/fileno() are the only POSIX calls this file makes, and on Windows
 * they live in <io.h> rather than <unistd.h> (which MinGW does not ship). MinGW
 * declares both under their unprefixed names, so the single use site in repl()
 * needs no further conditionals.
 */
#include <io.h>
#else
#include <unistd.h>
#endif

#include "s7.h"

#ifndef KRUDDS7_VERSION
#define KRUDDS7_VERSION "0.0.0-dev"
#endif

#define PROMPT      "krudds7> "
#define CONT_PROMPT "    ...> "

/*
 * A unique, un-forgeable value returned by the error handler wrapped around
 * every evaluation. It is a gensym created at startup, so no ordinary program
 * value can be eq? to it — that lets errored() tell "the form raised an error"
 * apart from "the form legitimately returned something".
 */
static s7_pointer g_error_marker;

/*
 * Read every top-level form out of the string bound to *krudds7-src* and
 * evaluate each one in the global environment ((rootlet)), returning the last
 * value. The whole loop runs inside a catch: any error is reported with s7's own
 * formatted message (to stderr) and the marker is returned.
 *
 * Evaluating via (eval form (rootlet)) rather than by splicing the source into a
 * (lambda () ...) body is deliberate — a lambda body would make top-level
 * (define ...)s local to that body, so the REPL would forget every definition.
 */
static const char *EVAL_WRAP =
	"(catch #t"
	"  (lambda ()"
	"    (let ((p (open-input-string *krudds7-src*)))"
	"      (let loop ((form (read p)) (val (if #f #f)))"
	"        (if (eof-object? form)"
	"            val"
	"            (loop (read p) (eval form (rootlet)))))))"
	"  (lambda (tag info)"
	"    (apply format (current-error-port) info)"
	"    (newline (current-error-port))"
	"    *krudds7-error-marker*))";

/* Same idea for loading a file: (load path) already evaluates at global scope. */
static const char *LOAD_WRAP =
	"(catch #t"
	"  (lambda () (load *krudds7-load-file*))"
	"  (lambda (tag info)"
	"    (apply format (current-error-port) info)"
	"    (newline (current-error-port))"
	"    *krudds7-error-marker*))";

static int errored(s7_scheme *sc, s7_pointer v)
{
	return s7_is_eq(v, g_error_marker);
}

/* Evaluate the Scheme source in `src` (all its forms). Returns the last value,
 * or the error marker if any form raised. */
static s7_pointer eval_src(s7_scheme *sc, const char *src)
{
	s7_define_variable(sc, "*krudds7-src*", s7_make_string(sc, src));
	return s7_eval_c_string(sc, EVAL_WRAP);
}

/* Evaluate a Scheme expression given on the command line (-e / -p). With
 * `print`, echo the resulting value (the -p path); otherwise run it purely for
 * effect (the -e path, matching ruby/perl/node's silent -e). */
static int eval_string(s7_scheme *sc, const char *code, int print)
{
	s7_pointer v = eval_src(sc, code);

	if (errored(sc, v))
		return 1;
	if (print && v != s7_unspecified(sc)) {
		char *s = s7_object_to_c_string(sc, v);

		if (s) {
			printf("%s\n", s);
			free(s);
		}
	}
	return 0;
}

/* The script-runner path: load a Scheme file by path. */
static int load_file(s7_scheme *sc, const char *path)
{
	s7_pointer v;

	s7_define_variable(sc, "*krudds7-load-file*", s7_make_string(sc, path));
	v = s7_eval_c_string(sc, LOAD_WRAP);
	return errored(sc, v) ? 1 : 0;
}

/*
 * Whether `buf` holds at least one complete top-level form — a cheap paren
 * balance that skips over string literals, ; line comments, #| block comments |#,
 * and #\c character literals so their delimiters don't miscount. Returns true
 * once every opener has closed (depth <= 0) and the buffer isn't blank, which is
 * the REPL's cue to stop reading continuation lines and evaluate.
 */
static int form_complete(const char *buf)
{
	int depth = 0;
	int has_content = 0;
	const char *p = buf;

	while (*p) {
		if (*p == ';') {
			while (*p && *p != '\n')
				p++;
			continue;
		}
		if (p[0] == '#' && p[1] == '|') {
			p += 2;
			while (*p && !(p[0] == '|' && p[1] == '#'))
				p++;
			if (*p)
				p += 2;
			continue;
		}
		if (p[0] == '#' && p[1] == '\\') {
			/* Character literal: the delimiter after #\ is data. */
			has_content = 1;
			p += 2;
			if (*p)
				p++;
			continue;
		}
		if (*p == '"') {
			has_content = 1;
			p++;
			while (*p && *p != '"') {
				if (*p == '\\' && p[1])
					p++;
				p++;
			}
			if (*p == '"')
				p++;
			continue;
		}
		if (*p == '(' || *p == '[') {
			depth++;
			has_content = 1;
		} else if (*p == ')' || *p == ']') {
			if (depth > 0)
				depth--;
			has_content = 1;
		} else if (*p != ' ' && *p != '\t' && *p != '\n' && *p != '\r') {
			has_content = 1;
		}
		p++;
	}
	return has_content && depth <= 0;
}

/*
 * Read-eval loop over stdin. Reads lines, accumulating them until they form a
 * complete expression, evaluates it, and — only when stdin is a terminal —
 * prints the value with a prompt. Piped/redirected input runs like a script: no
 * prompts, no echoed values. Exits on EOF (Ctrl-D). Errors are reported by the
 * catch wrapper and do not end the loop.
 */
static int repl(s7_scheme *sc)
{
	char *acc = NULL;
	size_t acc_len = 0;
	char line[4096];
	int interactive = isatty(fileno(stdin));
	int status = 0;

	if (interactive)
		fprintf(stderr, "krudds7 %s — s7 %s (%s). Ctrl-D to exit.\n",
			KRUDDS7_VERSION, S7_VERSION, S7_DATE);

	for (;;) {
		if (interactive) {
			fputs(acc_len ? CONT_PROMPT : PROMPT, stderr);
			fflush(stderr);
		}
		if (!fgets(line, sizeof(line), stdin)) {
			if (interactive)
				fputc('\n', stderr);
			break;
		}

		size_t ln = strlen(line);
		char *grown = realloc(acc, acc_len + ln + 1);

		if (!grown) {
			fprintf(stderr, "krudds7: out of memory\n");
			free(acc);
			return 1;
		}
		acc = grown;
		memcpy(acc + acc_len, line, ln + 1);
		acc_len += ln;

		if (!form_complete(acc))
			continue;

		s7_pointer v = eval_src(sc, acc);

		if (errored(sc, v)) {
			status = 1;
		} else if (interactive && v != s7_unspecified(sc)) {
			char *s = s7_object_to_c_string(sc, v);

			if (s) {
				printf("%s\n", s);
				free(s);
			}
		}
		fflush(stdout);
		free(acc);
		acc = NULL;
		acc_len = 0;
	}

	free(acc);
	return status;
}

static void usage(FILE *out)
{
	fprintf(out,
		"krudds7 %s — the s7 Scheme interpreter\n"
		"\n"
		"Usage:\n"
		"  krudds7                 start an interactive REPL\n"
		"  krudds7 FILE [FILE...]  load and run each Scheme file, then exit\n"
		"  krudds7 -e EXPR         evaluate EXPR for effect, then exit\n"
		"  krudds7 -p EXPR         evaluate EXPR and print its value, then exit\n"
		"  krudds7 -               read a program from stdin\n"
		"\n"
		"Options:\n"
		"  -e EXPR         evaluate EXPR for its side effects (no value printed)\n"
		"  -p EXPR         evaluate EXPR and print its value\n"
		"  -v, --version   print version (krudds7 and embedded s7) and exit\n"
		"  -h, --help      print this help and exit\n",
		KRUDDS7_VERSION);
}

int main(int argc, char **argv)
{
	s7_scheme *sc;
	int i;
	int status = 0;
	int ran_something = 0;

	sc = s7_init();
	if (!sc) {
		fprintf(stderr, "krudds7: failed to initialise s7\n");
		return 1;
	}

	/* The error marker: a gensym, so nothing an evaluated form produces can
	 * be eq? to it. Bound as a variable so the wrapped error handler can
	 * return it (see EVAL_WRAP / LOAD_WRAP). */
	g_error_marker = s7_eval_c_string(sc, "(gensym \"krudds7-error\")");
	s7_gc_protect(sc, g_error_marker);
	s7_define_variable(sc, "*krudds7-error-marker*", g_error_marker);
	s7_define_variable(sc, "*krudds7-version*",
			   s7_make_string(sc, KRUDDS7_VERSION));

	for (i = 1; i < argc; i++) {
		const char *arg = argv[i];

		if (!strcmp(arg, "-h") || !strcmp(arg, "--help")) {
			usage(stdout);
			s7_free(sc);
			return 0;
		}
		if (!strcmp(arg, "-v") || !strcmp(arg, "--version")) {
			printf("krudds7 %s (s7 %s, %s)\n",
			       KRUDDS7_VERSION, S7_VERSION, S7_DATE);
			s7_free(sc);
			return 0;
		}
		if (!strcmp(arg, "-e") || !strcmp(arg, "-p")) {
			int print = !strcmp(arg, "-p");

			if (i + 1 >= argc) {
				fprintf(stderr, "krudds7: %s needs an expression\n", arg);
				s7_free(sc);
				return 2;
			}
			if (eval_string(sc, argv[++i], print))
				status = 1;
			ran_something = 1;
			continue;
		}
		if (!strcmp(arg, "-")) {
			if (repl(sc))
				status = 1;
			ran_something = 1;
			continue;
		}
		if (arg[0] == '-' && arg[1] != '\0') {
			fprintf(stderr, "krudds7: unknown option '%s'\n", arg);
			usage(stderr);
			s7_free(sc);
			return 2;
		}
		/* A bare path: load it as a script. */
		if (load_file(sc, arg))
			status = 1;
		ran_something = 1;
	}

	if (!ran_something)
		status = repl(sc);

	s7_free(sc);
	return status;
}
