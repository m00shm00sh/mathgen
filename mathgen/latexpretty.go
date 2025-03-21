/*
 * latexpretty.go - latex pretty printing
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
	"math/rand"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pretty printing mode
type Pretty int

const (
	Pnone Pretty = iota
	Platex
	Platexbook
	Pbibtex
)

func (g *GeneratorWorker) GeneratePrettyString(p Pretty) string {
	var startSym = "START"
	switch p {
	case Pbibtex:
		startSym = "BIBTEX_ENTRY"
	}
	s := g.GenerateString(startSym)
	switch p {
	case Platex, Platexbook:
		s = prettyPrintLatex(&g.loggable, s, p)
	case Pbibtex:
		s = prettyPrintBibtex(g.rng, s)
	}
	return s
}

// fix "a object"
// s/PATTERN/g[1]g[2]n g[3]/g
// ReplaceAllString(_, `$1$2n$3`)
var ppLatexRx1 = regexp.MustCompile(rxFlagI + `(\b)(a)((?:\s+)(?:\\[^\s\{]+\{)?(?:[aeiou]))`)

const ppLatexRx1Repl = `$1$2n $3`

// extract heading
var ppLatexExtractHeadingRx = regexp.MustCompile(`(\\(?:sci)?(?:(?:(?:sub)?section\*?)|(?:chapter|title)))\{(.*)\}`)

// replace "so then $$x=y$$." with "so then $$x=y.$$"
// ReplaceAllString(_, `$2$1`)
var ppLatexRx3 = regexp.MustCompile(`(\$\$|\\end\{align\*\})\s*([,.;:!?])`)

const ppLatexRx3Repl = `$2$1`

// fix "foo , bar"
// s/PATTERN/$1/g
// ReplaceAllString(_, `$1`)
var ppCommonRx1 = regexp.MustCompile(`\s+([,.\-!?\';:])`)

const ppCommonRx1Repl = `$1`

// fix "foo- bar"
// s/PATTERN/-/g
// ReplaceAllString(_, `-`)
var ppCommonRx2 = regexp.MustCompile(`-\s+`)

const ppCommonRx2Repl = `-`

func matchNonSpace(r rune) bool {
	return !unicode.IsSpace(r)
}

func prettyPrintLatex(l *loggable, s string, p Pretty) string {
	var sb strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if l.verbosity >= Info {
			sb.WriteString("% ")
			sb.WriteString(line)
			sb.WriteRune('\n')

		}
		// if the line is empty of non-trivial content, we have nothing to transform
		if len(strings.TrimSpace(line)) < 1 {
			sb.WriteRune('\n')
			continue
		}
		line = ppLatexRx1.ReplaceAllString(line, ppLatexRx1Repl)
		var newline string
		if mi := ppLatexExtractHeadingRx.FindStringSubmatchIndex(line); mi != nil {
			unreachable(len(mi) != 6, "unexpected match")
			command := strings.TrimSpace(line[mi[2]:mi[3]])
			title := strings.TrimSpace(line[mi[4]:mi[5]])
			title = enTitle(title)
			switch p {
			case Platexbook:
				shortTitle := "\\truncate{0.75\\textwidth}{" + title + "}"
				if strings.Contains(command, "title") {
					newline = command + "{" + title + "}"
				} else if strings.Contains(command, "chapter") {
					newline = command + "{" + title + "} " +
						"\n\\chaptermark{" + shortTitle + "}"
				} else {
					newline = command + "[" + shortTitle + "]{" + title + "}"
				}
			default:
				newline = command + "{" + title + "}"
			}
			if len(strings.TrimSpace(newline)) < 1 {
				panic("heading")
			}
		} else if strings.Contains(line, "\\index") {
			// don't mangle index entries
			newline = line
			if len(strings.TrimSpace(newline)) < 1 {
				panic("index")
			}
		} else if strings.ContainsFunc(line, matchNonSpace) {
			newline = enSentence(line)
		} else {
			panic("unreachable")
		}

		newline = strings.ReplaceAll(newline, "\\Em", "\\em")
		newline = ppCommonRx1.ReplaceAllString(newline, ppCommonRx1Repl)
		newline = ppCommonRx2.ReplaceAllString(newline, ppCommonRx2Repl)
		newline = ppLatexRx3.ReplaceAllString(newline, ppLatexRx3Repl)

		sb.WriteString(newline)
		if wN, _ := utf8.DecodeLastRuneInString(newline); wN != '\n' {
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

var lastNameRx = regexp.MustCompile(`([[:upper:]][^\s]*)\s*$`)

func getLastName(s string) string {
	if m := lastNameRx.FindStringSubmatchIndex(s); m != nil {
		return s[m[2]:m[3]]
	}
	return ""
}

// The original code suggests using Lingua::EN::NameParse as a TODO to sort properly.
// I don't really care to find or make a Go equivalent of that package.
func sortAuthors(r *rand.Rand, s string) string {
	// A few papers might have non-alphabetical authors
	if r.Float64() < 0.03 {
		return s
	}
	authors := strings.Split(s, " and ")
	// TODO: will the author count get high enough that getLastName should be cached?
	slices.SortFunc(authors, func(a, b string) int {
		c := strings.Compare(getLastName(a), getLastName(b))
		if c == 0 {
			c = strings.Compare(a, b)
		}
		return c
	})
	return strings.Join(authors, " and ")
}

// if m/PATTERN/ { (first, title, last) = (g[1], g[2], g[3]) }
var ppBibtexRx1 = regexp.MustCompile(`^(.*(?:title|journal)\s*=\s*\{)(.*)(\},\s*)$`)

// fix "a object"
// (this is a simplified form of ppLatexRx1 by removing the \{ matching, more or less)
// s/PATTERN/g[1]g[2]n g[3]/g
// ReplaceAllString(_, "$1$2n $3")
var ppBibtexRx2 = regexp.MustCompile(rxFlagI + `(\b)(a)\s+([aeiou])`)

const ppBibtexRx2Repl = `$1$2n $3`

// sort author lists
// if m/PATTERN/ { authors := sortAuthors(g[1]) }
var ppBibtexRx5 = regexp.MustCompile(`\s*author\s*=\s*\{(.*)\}`)

// protect all math with brackets
// ReplaceAllString(_, "\{$1\}")
var ppBibtexRx6 = regexp.MustCompile(`(\$.*?\$)`)

const ppBibtexRx6Repl = `{$1}`

func prettyPrintBibtex(r *rand.Rand, s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if m := ppBibtexRx1.FindStringSubmatchIndex(line); m != nil {
			first := line[m[2]:m[3]]
			title := line[m[4]:m[5]]
			last := line[m[6]:m[7]]
			title = ppBibtexRx2.ReplaceAllString(title, ppBibtexRx2Repl)
			title = ppCommonRx1.ReplaceAllString(title, ppCommonRx1Repl)
			title = ppCommonRx2.ReplaceAllString(title, ppCommonRx2Repl)
			title = enBracket(title)
			title = enTitle(title)
			line = strings.Join([]string{first, title, last}, "")
		}
		// sort author lists
		if m := ppBibtexRx5.FindStringSubmatchIndex(line); m != nil {
			authors := sortAuthors(r, line[m[2]:m[3]])
			line = strings.Join([]string{"author", "=", "{", authors, "},"}, " ")
		}
		// protect all math with brackets
		line = ppBibtexRx6.ReplaceAllString(line, ppBibtexRx6Repl)

		out.WriteString(line)
		out.WriteRune('\n')
	}
	return out.String()
}

var enBracketRx1 = regexp.MustCompile(`(^|[\s-])([[:upper:]])`)

const enBracketRx1Repl = `$1{$2}`

var enBracketRx2 = regexp.MustCompile(`(\\[[:alpha:]]+(?:\{[^\s\}]*\})?)`)

const enBracketRx2Repl = `{$1}`

func enBracket(s string) string {
	// enBracketRx1.SubAll(s, `$1\{$2}`)
	s = enBracketRx1.ReplaceAllString(s, enBracketRx1Repl)
	// enBracketRx2.SubAll(s, `\{$1}`)
	s = enBracketRx2.ReplaceAllString(s, enBracketRx2Repl)
	return s
}

// helpers for enTitle, enSentence
var splitWordsRx = regexp.MustCompile(`([\s-]+)`)

func splitWords(s string) []string {
	return splitStringCapturingSubmatch(splitWordsRx, s, -1)
}

func genSmallWords() map[string]empty {
	smallWordsA := strings.Fields(`
		a an at as and are
		but by
		ere
		for from
		in into is
		of on or over
		per
		the to that than
		until unto upon
		via
		with while whilst within without
	`)
	m := make(map[string]empty)
	for _, s := range smallWordsA {
		m[s] = empty{}
	}
	return m
}

var smallWords = genSmallWords()

func ucFirstIf(s string, condition func(rune) bool) (string, bool) {
	r, size := utf8.DecodeRuneInString(s)
	if condition(r) {
		return string(unicode.ToTitle(r)) + s[size:], true
	}
	return s, false
}

func enTitle(s string) string {
	isSmallWord := func(w string) bool {
		_, ok := smallWords[w]
		return ok
	}
	var b strings.Builder
	words := splitWords(s)
	for i, w := range words {
		unreachable(len(w) < 1, "empty word")
		newW, _ := ucFirstIf(w, func(w0 rune) bool {
			return unicode.IsLower(w0) && (i == 0 || i == len(words)-1 || !isSmallWord(w))
		})
		b.WriteString(newW)
	}
	return b.String()
}

func enSentence(s string) string {
	var b strings.Builder
	words := splitWords(s)
	start := true
	for _, w := range words {
		if len(w) < 1 {
			continue
		}
		//# allow $ because a sentence could start with math
		newW, didConvert := ucFirstIf(w, func(w0 rune) bool {
			return (w0 == '$' || unicode.IsLetter(w0)) && start
		})
		if didConvert {
			start = false
		}
		b.WriteString(newW)
		wN, _ := utf8.DecodeLastRuneInString(w)
		if strings.ContainsRune(".?!", wN) {
			start = true
		}
	}
	return b.String()
}
