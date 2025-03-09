/*
 * debugdriver.go - Debug version of driver for (manually) testing engine behavior on command line
 * Mathgen, Golang port.
 * Copyright (C) 2025 Andrey V.
 *
 * Adapted from mathgen.pl from mathgen (https://thatsmathematics.com/mathgen/).
 * Portions may be copyright (C) Nathaniel Eldredge.
 *
 * This, and the original code, are licensed under GPL 2.
 */
/*
  * debugdriver is subject to the following limitations:
  * - product is not an enumeration - it is a path to a file;
      (an option shall specify whether to chdir into file's dir or not so that .include can behave as expected)
	- output is an enumeration of Tex or Bib;
		- Raw is raw output
		- Platex is LaTex output suitable for article
		- Platexbook is LaTeX output suitable for book
		- Bib is Bibtex output; preceding Tex source is commented as appropriate;
		  if the Tex scanning recognized nothing (due to producing non-Tex text, perhaps) and
		  there is no bibliography to generate, there may be an error
*/
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/m00shm00sh/mathgen/go/mathgen"
)

type omode int

const (
	om_raw omode = iota
	om_platex
	om_platexbook
	om_bib
)

func (om *omode) Set(s string) error {
	switch s {
	case "r", "raw":
		*om = om_raw
	case "l", "latex", "platex":
		*om = om_platex
	case "lb", "latexbook", "platexbook":
		*om = om_platexbook
	case "b", "bib", "bibtex":
		*om = om_bib
	default:
		return errors.New("")
	}
	return nil
}
func (om *omode) String() string {
	switch *om {
	case om_raw:
		return "raw"
	case om_platex:
		return "latex"
	case om_platexbook:
		return "latexbook"
	case om_bib:
		return "bibtex"
	default:
		panic("unrecognized omode")
	}
}

var (
	omPretty = map[omode]mathgen.Pretty{
		om_raw:        mathgen.Pnone,
		om_platex:     mathgen.Platex,
		om_platexbook: mathgen.Platexbook,
		om_bib:        mathgen.Pbibtex,
	}
)

type authorsT []string

func (a *authorsT) Set(s string) error {
	*a = append(*a, s)
	return nil
}
func (a *authorsT) String() string {
	return strings.Join(*a, ", ")
}

var (
	productStrOrPath     string
	verbosity            mathgen.Verbosity = mathgen.Debug
	addBibtexPlaceholder bool
	outMode              omode = om_raw
	seed                 int64
	authors              authorsT
)

func pInfoF(f string, v ...any) {
	if verbosity >= mathgen.Info {
		fmt.Printf("I: "+f+"\n", v...)
	}
}
func pDebugF(f string, v ...any) {
	if verbosity >= mathgen.Debug {
		fmt.Printf("V: "+f+"\n", v...)
	}
}

func getGenerator() *mathgen.Generator {
	s := productStrOrPath
	if err := (new(mathgen.Product)).Set(s); err == nil {
		pDebugF("getGenerator: detected valid product; using product expansion")
		if wd, err := os.Getwd(); err != nil {
			panic(fmt.Errorf("getwd: %w", err))
		} else {
			pDebugF("getGenerator: invalid product; treating as path")
			s = filepath.Join(wd, fmt.Sprintf("sci%s.in", s))
		}
	}

	b := mathgen.NewGeneratorBuilder()
	if file, err := os.Open(s); err != nil {
		panic(fmt.Errorf("open %s: %w", s, err))
	} else {
		b.Input = file
	}
	if addBibtexPlaceholder {
		b.AddBibtexPlaceholder = true
	}
	b.SetVerbosity(verbosity)
	return b.Build()
}

// generate output to stdout
func generateOutput() {
	generator := getGenerator().NewWorker(seed, authors)

	text := generator.GeneratePrettyString(omPretty[outMode])
	var bib string
	if outMode == om_bib {
		bib = generator.GenerateBibtex(text)
	}
	if len(bib) > 0 {
		for s := range strings.Split(text, "\n") {
			fmt.Printf("LaTeX-Source %s\n", s)
		}
		fmt.Println(bib)
	} else {
		fmt.Println(text)
	}
}

func doArgs() {
	flag.Var(&authors, "a",
		`specify an author of the paper (can be specified multiple times)
Default: One random author`)
	flag.Var(&outMode, "om",
		`what to output
 (r)aw: raw output
 p((l)atex): (article-)prettified LaTeX
 p((l)atex(b)ook): book-prettified LaTeX
 ((b)ib)tex: BiBTeX with commented LaTeX source
 `)
	flag.StringVar(&productStrOrPath, "pf", "", "what to generate")
	flag.Int64Var(&seed, "s", 0, "PRNG seed")
	flag.Var(&verbosity, "v", "enable verbosity features")
	flag.Parse()
	if len(productStrOrPath) < 1 {
		panic("product or file empty or missing")
	}
}

func main() {
	doArgs()
	generateOutput()
}
