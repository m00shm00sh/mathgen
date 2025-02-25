/*
 * engine.go - grammar engine
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
	"bufio"
	"io"
	"iter"
	"log"
	"math"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type empty struct{}

type Verbosity int

const (
	None Verbosity = iota
	Info
	Verbose
	Debug
)

type loggable struct {
	logger    *log.Logger
	verbosity Verbosity
}

type GeneratorBuilder struct {
	loggable
	fh      io.Reader
	seed    int64
	authors []string
}

func NewGeneratorBuilder() *GeneratorBuilder {
	return &GeneratorBuilder{
		loggable: loggable{
			logger:    log.Default(),
			verbosity: None,
		},
		fh:      os.Stdin,
		seed:    rand.Int63(),
		authors: []string{"AUTHOR"},
	}
}
func (b *GeneratorBuilder) SetInputStream(fh io.Reader) *GeneratorBuilder {
	b.fh = fh
	return b
}
func (b *GeneratorBuilder) SetLogger(l *log.Logger) *GeneratorBuilder {
	b.logger = l
	return b
}
func (b *GeneratorBuilder) SetVerbosity(v Verbosity) *GeneratorBuilder {
	b.verbosity = v
	return b
}
func (b *GeneratorBuilder) SetRngSeed(r int64) *GeneratorBuilder {
	b.seed = r
	return b
}
func (b *GeneratorBuilder) SetAuthors(a []string) *GeneratorBuilder {
	if a == nil {
		panic("nil authors")
	}
	b.authors = a
	return b
}

type Generator struct {
	loggable
	rules        map[string][]string
	numRules     map[string]int
	dupRules     map[string][]string
	handledFiles map[string]empty
	tokenRx      *regexp.Regexp
	rng          *rand.Rand
}

func (b *GeneratorBuilder) Build() *Generator {
	seed := b.seed
	g := Generator{
		loggable: b.loggable,
		rng:      rand.New(rand.NewSource(seed)),

		rules:        make(map[string][]string),
		numRules:     make(map[string]int),
		dupRules:     make(map[string][]string),
		handledFiles: make(map[string]empty),
	}
	g.logInfo("seed =", seed)
	g.rules["SEED"] = []string{strconv.FormatInt(seed, 10)}
	g.readRulesFile(b.fh)
	g.addAuthorsRule(b.authors)
	g.addYearRule()
	g.buildLookupRx()
	g.logDebug(g.rules)
	return &g
}

func fileIterator(l *log.Logger, fh io.Reader) iter.Seq[string] {
	sc := bufio.NewScanner(fh)
	return func(yield func(string) bool) {
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if len(line) > 0 && line[0] != '#' {
				if !yield(line) {
					break
				}
			}
		}
		if err := sc.Err(); err != nil {
			l.Panicln(err)
		}

	}
}

var rxNonduplicateRule = regexp.MustCompile(`([^+]*)!$`)
var rxWeightRule = regexp.MustCompile(`([^+]*)\+(\d+)$`)
var rxCheckSpecial = regexp.MustCompile(`(.*)([+#])$`)

func (g *Generator) appendRule(name string, ruleItem string) {
	items := g.rules[name]
	g.rules[name] = append(items, ruleItem)
}
func (g *Generator) appendDupRule(name string, ruleItem string) {
	items := g.dupRules[name]
	g.dupRules[name] = append(items, ruleItem)
}

func (g *Generator) logPanic(v ...any) {
	g.logger.Panic(v)
}
func (g *Generator) logInfo(v ...any) {
	if g.verbosity >= Info {
		a := slices.Concat([]any{"I:"}, v)
		g.logger.Println(a...)
	}
}
func (g *Generator) logVerbose(v ...any) {
	if g.verbosity >= Verbose {
		a := slices.Concat([]any{"V:"}, v)
		g.logger.Println(a...)
	}
}
func (g *Generator) logDebug(v ...any) {
	if g.verbosity >= Debug {
		a := slices.Concat([]any{"D:"}, v)
		g.logger.Println(a...)
	}
}

func (g *Generator) readRulesFile(fh io.Reader) {
	lineItr := fileIterator(g.logger, fh)
	var err error
	for line := range lineItr {
		words := strings.Fields(line)
		name := words[0]
		if len(words) < 2 {
			words = []string{}
		} else {
			words = words[1:]
		}
		var rule string

		// non-duplicate rule;
		// each expansion instance produces a different substitution
		if m := rxNonduplicateRule.FindStringSubmatch(name); m != nil {
			name = m[1]
			g.appendDupRule(name, "")
			continue
		}

		// include rule
		if strings.HasSuffix(name, ".include") {
			file := words[0]
			// guard against multiple includes (this allows main file to be included twice)
			if _, exists := g.handledFiles[file]; exists {
				g.logInfo("Skipping duplicate included file", file)
				continue
			}
			g.handledFiles[file] = empty{}
			g.logInfo("Opening included file", file)
			localReader, err := os.Open(file)
			if err != nil {
				g.logPanic("Couldn't open included file", file, err)
			}
			g.readRulesFile(localReader)
			continue
		}

		// multi-line rule
		if len(words) == 1 && words[0] == "{" {
			var seenEnd bool
			for line = range lineItr {
				if line == "}" {
					seenEnd = true
					break
				} else {
					rule = rule + "\n" + line
				}
			}
			if !seenEnd {
				g.logPanic(name, "EOF reached before end of rule")
			}
		} else {
			rule = strings.Join(words, " ")
		}
		// look for weight
		weight := 1
		if m := rxWeightRule.FindStringSubmatch(name); m != nil {
			name = m[1]
			weight, err = strconv.Atoi(m[2])
			if err != nil {
				g.logPanic(name, "int parse:", m[2], err)
			}
			g.logVerbose("weighting rule by ", weight, ":", name, "->", rule)
		}
		for weight > 0 {
			weight -= 1
			g.appendRule(name, rule)
		}
	}
}

func computeLookupRegexp(m map[string][]string) *regexp.Regexp {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i += 1
	}
	// must sort to get a longest match by descending order
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pat := strings.Join(keys, "|")
	rePat := `(?s)^(.*?)(` + pat + `)`
	return regexp.MustCompile(rePat)
}

func (g *Generator) buildLookupRx() {
	g.lookupRx = computeLookupRegexp(g.rules)
}

func (g *Generator) addYearRule() {
	yearRule := make([]string, 0)

	thisYear := time.Now().Year()

	// we wish to have entries for each of the last 100 years, with
	// more recent years being exponentially more likely
	nYears := 100
	nYearsF := float64(nYears)
	// newest year is this many times more likely than oldest;
	// convert to float now since it won't be used in an integer context
	r := float64(35)

	for i := range nYears { // don't use current year
		y := thisYear - nYears + i
		n := math.Pow(r, float64(i)/nYearsF)
		y_asStrSlice := []string{strconv.Itoa(y)}
		yearRule = slices.Concat(yearRule, slices.Repeat(y_asStrSlice, int(n)))
	}

	g.rules["SCI_YEAR"] = yearRule
}

func (g *Generator) addAuthorsRule(a []string) {
	lastA, otherA := a[len(a)-1], a[:len(a)-1]
	var sb strings.Builder
	if len(otherA) > 0 {
		sb.WriteString(strings.Join(otherA, ", "))
		sb.WriteString(" and ")
	}
	sb.WriteString(lastA)
	g.rules["AUTHOR_NAME"] = a
	g.rules["SCIAUTHORS"] = []string{sb.String()}
}

func pickRand(r *rand.Rand, s []string) string {
	n := len(s)
	return s[r.Intn(n)]
}

// (inTok) -> (pre, rule, post)
func (g *Generator) popFirstRule(inTok string) []string {
	var pre string
	var rule string
	var post string
	mi := g.lookupRx.FindStringSubmatchIndex(inTok)
	if len(mi) == 6 {
		pre = inTok[mi[2]:mi[3]]
		rule = inTok[mi[4]:mi[5]]
		post = inTok[mi[1]:]
		return []string{pre, rule, post}
	}
	return nil
}

func (g *Generator) GenerateString(startToken string) string {
	g.logDebugF("tokenRx = %v", g.tokenRx)
	s := g.expandRecursively(startToken)
	// is this necessary? might be needed during bibtex pass
	//clear(g.numRules)
	// need to separate dups created during rule reading from dups created during expansion
	//clear(g.dupRules)
	return s
}
func (g *Generator) GenerateText() string {
	return g.GenerateString("START")
}

func (g *Generator) expandRecursively(start string) string {
	/* check for special rules ending in + and #
	 * Rules ending in + generate a sequential integer
	 * The same rule ending in # chooses a random # from among preiously generated integers
	 * The stripped rule entry is the active counter which is used as either
	 * a thing to increment or an upper limit
	 */
	if m := rxCheckSpecial.FindStringSubmatch(start); m != nil {
		numRule := strings.TrimSpace(m[1])
		i := g.numRules[numRule]
		if m[2] == "+" {
			g.numRules[numRule] = i + 1
		} else if m[2] == "#" && i > 0 {
			i = g.rng.Intn(i)
		}
		return strconv.Itoa(i)
	}

	var fullToken string
	var doRepeat = true
	var count int
	for doRepeat {
		inputTok := pickRand(g.rng, g.rules[start])
		count += 1
		g.logDebug("expand:", start, "->", inputTok)

		doRepeat = false

		var components []string

		for {
			var pre, rule string
			pfr := g.popFirstRule(inputTok)
			if pfr != nil {
				pre, rule, inputTok = pfr[0], pfr[1], pfr[2]
			} else {
				break
			}
			ex := g.expandRecursively(rule)
			if len(pre) > 0 {
				components = append(components, pre)
			}
			if len(ex) > 0 {
				components = append(components, ex)
			}
		}
		if len(inputTok) > 0 {
			components = append(components, inputTok)
		}

		fullToken = strings.Join(components, "")

		dups := g.dupRules[start]
		if dups != nil {
			// make sure we haven't generated this exact token yet
			for _, d := range dups {
				if d == fullToken {
					doRepeat = true
				}
			}

			if !doRepeat {
				g.appendDupRule(start, fullToken)
			} else if count > 50 {
				doRepeat = false
			}
		}
	}

	return fullToken
}
