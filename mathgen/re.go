/*
 * re.go - regex functions
 * Mathgen, Golang port.
 * Copyright (C) 2025 Andrey V.
 *
 * Adapted from mathgen.pl from mathgen (https://thatsmathematics.com/mathgen/).
 * Portions may be copyright (C) Nathaniel Eldredge.
 *
 * This, and the original code, are licensed under GPL 2.
 */
package mathgen

import (
	"regexp"
	"strings"
)

type regex struct { *regexp.Regexp }

func mustCompileRegex(expr string) *regex {
	return &regex { regexp.MustCompile(expr) }
}

/*
regexp.(*Regexp).ReplaceAllString lets us replace with submatches but it doesn't give us a way
* to process submatches or perform side effects on successful match like we can do with the following
* in Python:
*	p = re(PATTERN)
* 	s_ = None
*	def repl(m: Matcher):
*		nonlocal s_
*		s = m.group(2)
*		return m.group(1).lower()
*	s = re.sub(m, s)
* We have regexp.(*Regexp)ReplaceAllString but it only works with "simple" substitutions like `<$1>`.
* And ReplaceAllStringFunc gives us $& but not any subgroups (or $`, or $').
* We work around this by using a function that works on indices and extracts the (sub)match slices itself.
* It is assumed that repl has a non-local reference to the input string.

* Disappointing that there's re.replaceAll but the submatch form never got an exported version.
* NOTE: There is no version for []byte, unlike regexp API. This may change if there is demand.
*/
func (re *regex) replaceAllStringSubmatchIndexFunc(s string, n int, repl func([]int) string) string {
	last := 0
	var b strings.Builder
	for _, m := range re.FindAllStringSubmatchIndex(s, n) {
		b.WriteString(s[last:m[0]])
		b.WriteString(repl(m))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}
func (re *regex) splitStringCapturingSubmatch(s string, n int) []string {
	var w []string
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, n) {
		if len(m) != 4 {
			panic("this function should be used with a regex containing exactly one capture group")
		}
		w = append(w, s[last:m[0]], s[m[2]:m[3]])
		last = m[1]
	}
	w = append(w, s[last:])
	return w
}

const rxFlagI = `(?i)`
const rxFlagS = `(?s)`
