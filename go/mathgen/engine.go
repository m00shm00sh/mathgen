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
	"errors"
	"io"
	"io/fs"
	"iter"
	"log"
	"maps"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type empty struct{}

// builder for reading input and generating token rules
type GeneratorBuilder struct {
	loggable
	Input                io.Reader
	AddBibtexPlaceholder bool
	dirs                 []string
}

func NewGeneratorBuilder() *GeneratorBuilder {
	return &GeneratorBuilder{
		loggable: loggable{
			logger:    log.Default(),
			verbosity: None,
		},
		Input: os.Stdin,
		dirs:  []string{mustGetWd()},
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
	b.dirs = append(b.dirs, dir...)
	return b
}

// built set of rules; a seed value and st
type Generator struct {
	loggable
	tokenRx      *regexp.Regexp
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
	if b.Input == nil {
		panic("empty rules input")
	}
	g.readRulesFile(b.Input, b.dirs)
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

var readRulesNoDuplicateRuleRx = regexp.MustCompile(`([^+]*)!$`)
var readRulesWeightedRuleRx = regexp.MustCompile(`([^+]*)\+(\d+)$`)

func (g *Generator) appendRule(name string, ruleItem string) {
	items := g.rules[name]
	g.rules[name] = append(items, ruleItem)
}
func (g *GeneratorWorker) appendDupRule(name string, ruleItem string) {
	items := g.dupRules[name]
	g.dupRules[name] = append(items, ruleItem)
}

// Open a file using a list of candidate paths to free us from needing to Chdir.
// TODO: should we log ENOENT attempts at open?
func openFileWithPath(basename string, dirs []string) (*os.File, error) {
	if strings.ContainsRune(basename, os.PathSeparator) {
		return nil, errors.New("directory in include not allowed")
	}
	for _, dir := range dirs {
		tryPath := filepath.Join(dir, basename)
		if fh, err := os.Open(tryPath); err == nil {
			return fh, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fs.ErrNotExist
}

func (g *Generator) readRulesFile(fh io.Reader, dirs []string) {
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
		if m := readRulesNoDuplicateRuleRx.FindStringSubmatch(name); m != nil {
			name = m[1]
			g.dupRuleNames[name] = empty{}
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
			localReader, err := openFileWithPath(file, dirs)
			if err != nil {
				g.logPanic("Couldn't open included file", file, err)
			}
			g.readRulesFile(localReader, dirs)
			localReader.Close()
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
		if m := readRulesWeightedRuleRx.FindStringSubmatch(name); m != nil {
			name = m[1]
			weight, err = strconv.Atoi(m[2])
			if err != nil {
				g.logPanic(name, "int parse:", m[2], err)
			}
			g.logVerboseF("weighting rule by %d : %s -> %s", weight, name, cleanupNewlines(rule))
		}
		for weight > 0 {
			weight -= 1
			g.appendRule(name, rule)
		}
	}
}

func generateTokenRegexpFromRules(m map[string][]string) *regexp.Regexp {
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
	return regexp.MustCompile(rePat)
}

func (g *Generator) generateTokenRx() *regexp.Regexp {
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
	post = replaceAllStringSubmatchIndexFunc(g.tokenRx, inTok, 1, func(mi []int) string {
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
	g.logDebugF("tokenRx = %v", g.tokenRx)
	s := g.expandRecursively(startToken)
	return s
}
func (g *GeneratorWorker) GenerateText() string {
	return g.GenerateString("START")
}

var expandRecursivelyCheckSpecialRuleRx = regexp.MustCompile(`(.*)([+#])$`)

func (g *GeneratorWorker) expandRecursively(start string) string {
	/* check for special rules ending in + and #
	 * Rules ending in + generate a sequential integer
	 * The same rule ending in # chooses a random # from among preiously generated integers
	 * The stripped rule entry is the active counter which is used as either
	 * a thing to increment or an upper limit
	 */
	if m := expandRecursivelyCheckSpecialRuleRx.FindStringSubmatch(start); m != nil {
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
