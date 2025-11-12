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
	"cmp"
	"io"
	"io/fs"
	"iter"
	"log"
	"maps"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type empty struct{}

// builder for reading input and generating token rules
type GeneratorBuilder struct {
	loggable
	InputFilename        string
	AddBibtexPlaceholder bool
	dirs                 []string
	FS                   fs.FS
}

func NewGeneratorBuilder() *GeneratorBuilder {
	return &GeneratorBuilder{
		loggable: loggable{
			logger:    log.Default(),
			verbosity: None,
		},
		FS: nil,
	}
}

func (b *GeneratorBuilder) SetLogger(l *log.Logger) *GeneratorBuilder {
	b.loggable.SetLogger(l)
	return b
}
func (b *GeneratorBuilder) SetVerbosity(v Verbosity) *GeneratorBuilder {
	b.verbosity = v
	return b
}
func (b *GeneratorBuilder) AddInputDir(dir ...string) *GeneratorBuilder {
	if b.FS != nil {
		panic("AddInputDir should only be called when no custom FS provider is used")
	}
	b.dirs = append(b.dirs, dir...)
	return b
}

// built set of rules; a seed value and st
type Generator struct {
	loggable
	tokenRx      *regex
	handledFiles map[string]empty
	rules        map[string][]string
	dupRuleNames map[string]empty
}

type GeneratorWorker struct {
	Generator
	// track numbers for TOKEN+ and TOKEN* rules
	numRules map[string]int
	// track expansions for TOKEN! rules
	dupRules map[string][]string
	// track auxiliary rules where tokenRx was generated with placeholder-only tokens;
	// see placeholderRules
	auxRules map[string][]string
	// code that used default-generated seed may want to query its value
	seed int64
	rng  *rand.Rand
}

var (
	placeholderRules = []string{
		"SEED",
		"SCI_YEAR",
		"SCIAUTHORS", "AUTHOR_NAME",
	}
	defaultAuthors = []string{"AUTHOR"}
)

func (b *GeneratorBuilder) Build() *Generator {
	g := Generator{
		loggable:     b.loggable,
		handledFiles: make(map[string]empty),
		rules:        make(map[string][]string),
		dupRuleNames: make(map[string]empty),
	}
	if b.AddBibtexPlaceholder {
		g.addPlaceholderRules(append(placeholderRules, "CITE_LABEL_GIVEN"))
	} else {
		g.addPlaceholderRules(placeholderRules)
	}
	fileOpener := b.FS
	if fileOpener == nil {
		fileOpener = newOsFilereader(b.dirs)
	}
	g.readRulesFile(fileOpener, b.InputFilename)
	// discard unneeded handled files after outermost readRulesFile
	g.handledFiles = nil
	g.generateTokenRx()
	g.logDebugFunc(func() string {
		var b strings.Builder
		rKeys := slices.Collect(maps.Keys(g.rules))
		slices.SortFunc(rKeys, func(a, b string) int {
			return strings.Compare(a, b)
		})
		b.WriteString("dump rules\n")
		for _, k := range rKeys {
			b.WriteString("* rule: ")
			b.WriteString(k)
			b.WriteString(" -> ")
			b.WriteString(cleanupNewlines(strings.Join(g.rules[k], "|")))
			b.WriteRune('\n')
		}
		b.WriteString("* dupRule slots:")
		for _, k := range slices.Collect(maps.Keys(g.dupRuleNames)) {
			b.WriteRune(' ')
			b.WriteString(k)
		}
		return b.String()
	})
	return &g
}

func (g *Generator) addPlaceholderRules(names []string) {
	for _, s := range names {
		g.rules[s] = []string{}
	}
}

// Create a new instance of a worker. Pass seed=0 to use default seed and authors=nil to use a random author name.
func (g *Generator) NewWorker(seed int64, authors []string) *GeneratorWorker {
	if seed == 0 {
		seed = rand.Int63()
	}
	gw := GeneratorWorker{
		Generator: *g,
		numRules:  make(map[string]int),
		dupRules:  make(map[string][]string),
		auxRules:  make(map[string][]string),
		seed:      seed,
		rng:       rand.New(rand.NewSource(seed)),
	}
	// (*GeneratorBuilder).Build() gave us a set of key names to populate dupRules with
	for k := range g.dupRuleNames {
		g.logDebugF("dupRule %s", k)
		gw.appendDupRule(k, "")
	}

	g.logInfo("seed =", seed)
	gw.auxRules["SEED"] = []string{strconv.FormatInt(seed, 10)}
	if len(authors) > 0 {
		gw.addAuthorsRule(authors)
	} else {
		gw.addAuthorsRule(defaultAuthors)
	}
	gw.addYearRule()
	gw.logDebugFunc(func() string {
		var b strings.Builder
		rKeys := slices.Collect(maps.Keys(gw.auxRules))
		slices.SortFunc(rKeys, func(a, b string) int {
			return strings.Compare(a, b)
		})
		b.WriteString("dump auxrules\n")
		for _, k := range rKeys {
			b.WriteString("* rule: ")
			b.WriteString(k)
			b.WriteString(" -> ")
			b.WriteString(cleanupNewlines(strings.Join(gw.auxRules[k], "|")))
			b.WriteRune('\n')
		}
		return b.String()
	})
	return &gw
}
func (g *GeneratorWorker) Seed() int64 {
	return g.seed
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

func (g *Generator) appendRule(name string, ruleItem string) {
	items := g.rules[name]
	g.rules[name] = append(items, ruleItem)
}
func (g *GeneratorWorker) appendDupRule(name string, ruleItem string) {
	items := g.dupRules[name]
	g.dupRules[name] = append(items, ruleItem)
}

func getNonduplicateRule(s string) (bool, string) {
	// regexp /([^+]*)!$/
	if len(s) < 1 {
		return false, ""
	}
	iLast := len(s) - 1
	if s[iLast] != '!' {
		return false, ""
	}
	iPlus := strings.LastIndexByte(s, '+')
	return true, s[iPlus+1 : iLast]
}
func getWeightedRule(s string) (bool, string, int) {
	// regexp /([^+]*)\+(\d+)$/
	if len(s) < 2 {
		return false, "", 0
	}
	iLast := len(s) - 1
	iPlus := strings.LastIndexByte(s, '+')
	if iPlus == -1 || iPlus == iLast {
		return false, "", 0
	}
	if strings.IndexFunc(s[iPlus+1:iLast+1], func(r rune) bool {
		return !unicode.IsDigit(r)
	}) != -1 {
		return false, "", 0
	}
	var err error
	var digits int
	if digits, err = strconv.Atoi(s[iPlus+1:]); err != nil {
		return false, "", 0
	}
	iPrevPlus := strings.LastIndexByte(s[:iPlus], '+')
	return true, s[iPrevPlus+1 : iPlus], digits
}

func (g *Generator) readRulesFile(fs fs.FS, fName string) {
	g.logInfo("Opening file" + fName)
	fh, err := fs.Open(fName)
	if err != nil {
		g.logPanic("opening ", fName, err)
	}
	defer fh.Close()
	lineItr := fileIterator(g.logger, fh)
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
		if hasNonDupRule, ndName := getNonduplicateRule(name); hasNonDupRule {
			g.dupRuleNames[ndName] = empty{}
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
			g.logInfo("include:")
			g.readRulesFile(fs, file)
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
				g.logPanic(name, ":", "EOF reached before end of rule")
			}
		} else {
			rule = strings.Join(words, " ")
		}
		// look for weight
		weight := 1
		if isWeighted, newName, newWeight := getWeightedRule(name); isWeighted {
			name = newName
			weight = newWeight
			g.logVerboseF("weighting rule by %d : %s -> %s", weight, name, cleanupNewlines(rule))
		}
		for weight > 0 {
			weight -= 1
			g.appendRule(name, rule)
		}
	}
}

func generateTokenRegexpFromRules(m map[string][]string) *regex {
	keys := make([]string, len(m))
	i := 0
	for k := range maps.Keys(m) {
		keys[i] = k
		i += 1
	}
	// must sort to get a longest match by descending order
	slices.SortFunc(keys, func(a, b string) int {
		return -cmp.Compare(len(a), len(b))
	})
	pat := strings.Join(keys, "|")
	rePat := rxFlagS + `^(.*?)(` + pat + `)`
	return mustCompileRegex(rePat)
}

func (g *Generator) generateTokenRx() *regex {
	old := g.tokenRx
	g.tokenRx = generateTokenRegexpFromRules(g.rules)
	return old
}

func (g *GeneratorWorker) addYearRule() {
	yearRule := make([]string, 0)

	// doing the year rule every iteration is inefficient; should year be cached?
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

	g.auxRules["SCI_YEAR"] = yearRule
}

func (g *GeneratorWorker) addAuthorsRule(a []string) {
	lastA, otherA := a[len(a)-1], a[:len(a)-1]
	var sb strings.Builder
	if len(otherA) > 0 {
		sb.WriteString(strings.Join(otherA, ", "))
		sb.WriteString(" and ")
	}
	sb.WriteString(lastA)
	g.auxRules["AUTHOR_NAME"] = a
	g.auxRules["SCIAUTHORS"] = []string{sb.String()}
}

func pickRand(r *rand.Rand, s []string) string {
	n := len(s)
	return s[r.Intn(n)]
}

// (inTok) -> {pre, rule, post}
// this works on tokenRx without any of the aux maps so no need for GeneratorWorker
func (g *Generator) popFirstRule(inTok string) []string {
	var pre string
	var rule string
	var post string
	didMatch := false
	post = g.tokenRx.replaceAllStringSubmatchIndexFunc(inTok, 1, func(mi []int) string {
		unreachable(len(mi) != 6, "unexpected match")
		pre = inTok[mi[2]:mi[3]]  // $1
		rule = inTok[mi[4]:mi[5]] // $2
		didMatch = true
		return ""
	})
	if didMatch {
		return []string{pre, rule, post}
	}
	return nil
}

func (g *GeneratorWorker) GenerateString(startToken string) string {
	if _, ok := g.rules[startToken]; !ok {
		// this should only get triggered with malformed custom input files, and
		// custom input files are only used in debugging; panic is fine here
		panic("bad input: start token not found: " + startToken)
	}
	g.logDebugF("tokenRx = %v", g.tokenRx)
	s := g.expandRecursively(startToken)
	return s
}
func (g *GeneratorWorker) GenerateText() string {
	return g.GenerateString("START")
}

// If token ends with + or #, it needs sequential handling.
// If it needs sequential handling, return (true, c, tok), where c is sequence type ('+' or '#') and
// tok is token name.
// Otherwise, return (false, 0, "").
func getSequentialExpansionToken(s string) (bool, byte, string) {
	if len(s) < 1 {
		return false, 0, ""
	}
	iLast := len(s) - 1
	switch strings.IndexByte("+#", s[iLast]) {
	case 0, 1:
		return true, s[iLast], s[:iLast]
	case -1:
		fallthrough
	default:
		return false, 0, ""
	}
}

func (g *GeneratorWorker) expandRecursively(start string) string {
	/* check for special rules ending in + and #
	 * Rules ending in + generate a sequential integer
	 * The same rule ending in # chooses a random # from among preiously generated integers
	 * The stripped rule entry is the active counter which is used as either
	 * a thing to increment or an upper limit
	 */

	if isSeq, seqType, numRule := getSequentialExpansionToken(start); isSeq {
		i := g.numRules[numRule]
		switch {
		case seqType == '+':
			g.numRules[numRule] = i + 1
		case /* seqType == '#' && */ i > 0:
			i = g.rng.Intn(i)
		}
		return strconv.Itoa(i)
	}

	var fullToken string
	var doRepeat = true
	var count int
	for doRepeat {
		auxVals, isAux := g.auxRules[start]
		var ruleResult []string
		if isAux {
			g.logDebugF("auxRule %s\n", start)
			ruleResult = auxVals
		} else {
			ruleResult = g.rules[start]
		}
		inputTok := pickRand(g.rng, ruleResult)
		count += 1
		g.logDebugF("expand: %s -> %v", start, inputTok)

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

		if dups, hasDupRule := g.dupRules[start]; hasDupRule {
			g.logDebugF("dupRule %s", start)
			// make sure we haven't generated this exact token yet
			if slices.Contains(dups, fullToken) {
				doRepeat = true
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
