/*
 * main.go - command-line frontend
 * Mathgen, Golang port.
 * Copyright (C) 2025 Andrey V.
 *
 * Adapted from mathgen.pl from mathgen (https://thatsmathematics.com/mathgen/).
 * Portions may be copyright (C) Nathaniel Eldredge.
 *
 * This, and the original code, are licensed under GPL 2.
 */
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/m00shm00sh/mathgen/go/mathgen"
)

const (
	defaultViewer = "xdg-open"
)

type authorsT []string

// hacky inherit of mathgen.OutputMode
type umodeV int

const (
	useMGMode umodeV = iota
	view
)

var (
	umodeFromstr = map[string]umodeV{
		"view": view,
	}
	// not worth exporting backMap from mathgen.util for just this
	umodeStr = map[umodeV]string{
		view: "view",
	}
)

type umode struct {
	mgMode mathgen.OutputMode
	v      umodeV
}

func (m *umode) Set(s string) error {
	if e := m.mgMode.Set(s); e == nil {
		m.v = useMGMode
		return nil
	}
	if v := umodeFromstr[s]; v != 0 {
		m.v = v
		return nil
	}
	return fmt.Errorf("unrecognized mode %s", s)
}
func (m *umode) String() string {
	if m.v == useMGMode {
		return m.mgMode.String()
	}
	if s := umodeStr[m.v]; len(s) == 0 {
		panic("bad umode")
	} else {
		return s
	}
}

var (
	// these are non-local because their values are set by cmdline args
	authors        authorsT
	mode           umode = umode{v: view}
	viewer         string
	product        mathgen.Product = mathgen.Article
	outputFilename string
	seed           int64
	verbosity      mathgen.Verbosity = mathgen.None
)

func (a *authorsT) String() string {
	return strings.Join(*a, ", ")
}
func (a *authorsT) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func printVerboseF(format string, v ...any) {
	if verbosity >= mathgen.Verbose {
		fmt.Printf("V: "+format, v...)
		fmt.Println()
	}
}

func doArgs() {
	flag.Var(&authors, "author",
		`specify an author of the paper (can be specified multiple times)
Default: One random author`)
	flag.Var(&mode, "mode",
		`what to output
 pdf: PDF file
 zip: Zip file with LaTeX/BiBTeX source and PDF
 fullzip: Zip file with LaTex/BiBTeX source and generated files
 view: invoke viewer on PDF file
 raw: output raw TeX/txt only (required for product=blurb)`)
	flag.StringVar(&viewer, "viewer", defaultViewer, "program to use as PDF viewer")
	flag.StringVar(&outputFilename, "output", "",
		`specify output file
 use - for stdout`)
	flag.Var(&product, "product", "what to generate")
	flag.Int64Var(&seed, "seed", 0, "PRNG seed")
	flag.Var(&verbosity, "verbosity", "enable verbosity features")
	flag.Parse()
	if mode.v != view && len(outputFilename) == 0 {
		fmt.Fprintf(flag.CommandLine.Output(), "need output file when mode != view\n")
		flag.Usage()
		os.Exit(2)
	}
}

func outputFh() (io.Writer, error) {
	if mode.v == view && len(outputFilename) == 0 {
		pid := strconv.Itoa(os.Getpid())
		var err error
		patternBase := []string{"mathgen-go.", pid, "*.pdf"}
		mktempPattern := strings.Join(patternBase, "")
		ofh, err := os.CreateTemp(os.TempDir(), mktempPattern)
		if err != nil {
			return nil, fmt.Errorf("mktemp: %v", err)
		}
		outputFilename = ofh.Name()
	}
	switch outputFilename {
	case "-":
		return os.Stdout, nil
	case "":
		return nil, nil
	default:
		fh, err := os.Create(outputFilename)
		if err != nil {
			return nil, fmt.Errorf("CreateOutput %s: %w", outputFilename, err)
		}
		return fh, nil
	}
}

func runApp(dir string, cmd string, a ...string) error {
	printVerboseF("runApp: %s %v", cmd, a)
	c := exec.Command(cmd, a...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		if execErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s\n[dir=%s] %s failed: %s", out, dir, c, execErr.Error())
		}
		return err
	}
	return nil
}

func generateOutput() error {
	var err error
	var ofh io.Writer
	d := mathgen.NewDriver()
	defer func() {
		// close file in all cases
		ofhFile, isFile := ofh.(*os.File)
		if isFile {
			if err = ofhFile.Close(); err != nil {
				panic(fmt.Errorf("close %s: %w", outputFilename, err))
			}
		}
		if err == nil {
			return
		}
		// undo write in error case
		if isFile {
			if err = os.Remove(outputFilename); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					panic(fmt.Errorf("close %s: %w", outputFilename, err))
				}
			}
		}
	}()
	if mode.v == view {
		d.OutputMode = mathgen.Pdf
	} else {
		d.OutputMode = mode.mgMode
	}
	d.Product = product
	d.Seed = seed
	d.SetVerbosity(verbosity)
	d.Authors = authors
	var ifh io.Reader
	ifh, err = d.GenerateOutput()
	if err != nil {
		return err
	}
	ofh, err = outputFh()
	if _, err = io.Copy(ofh, ifh); err != nil {
		return err
	}
	switch mode.v {
	case view:
		runApp("", viewer, outputFilename)
	}
	return nil
}

func main() {
	doArgs()
	if err := generateOutput(); err != nil {
		// we're in main and have no use for more refined error handling
		panic(err)
	}
}
