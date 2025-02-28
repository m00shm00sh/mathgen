/*
 * main.go - frontend
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
	"archive/zip"
	"flag"
	"fmt"
	"github.com/m00shm00sh/mathgen/go/mathgen"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

const (
	defaultMode    = "view"
	defaultViewer  = "xdg-open"
	defaultProduct = "article"
	bibName        = "scigenbibfile.bib"
	dataDir        = "."
)

type empty struct{}

func genModes() map[string]empty {
	modesA := strings.Fields(`pdf zip dir view raw`)
	m := make(map[string]empty)
	for _, w := range modesA {
		m[w] = empty{}
	}
	return m
}

type authorsT []string

var (
	// constant-ish
	products = map[string]string{ // key = product; value = pretty
		"article": "latex",
		"book":    "latexbook",
		"blurb":   "",
	}
	modes = genModes()
	// these can be moved into some New function eventually
	authors   authorsT
	mode      string
	viewer    string
	dir       string
	output    string
	product   string
	seed      int64
	verbosity mathgen.Verbosity = mathgen.None
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

func generateReadmeText(p, name string) (string, error) {
	var b strings.Builder
	switch p {
	case "article":
		t := template.Must(template.New("tArticle").Parse(
			`To recompile this file, run:

pdflatex {{.}}
bibtex {{.}}
pdflatex {{.}}
pdflatex {{.}}

You need the following packages installed:

AMS-LaTeX
fullpage
mathrsfs
natbib
truncate
`))
		if err := t.Execute(&b, name); err != nil {
			return "", err
		}
	case "book":
		t := template.Must(template.New("tBook").Parse(
			`To recompile this file, run:

pdflatex {{.}}
bibtex {{.}}
makeindex {{.}}.idx
pdflatex {{.}}
pdflatex {{.}}

You need the following packages installed:

AMS-LaTeX
geometry
mathrsfs
natbib
txfonts
hyphenat
textcase
hyperref
truncate
titlesec
makeidx
url
tocbibind

The output is set to 6x9 inch paper and is suitable for lulu.com.
`))
		if err := t.Execute(&b, name); err != nil {
			return "", err
		}
	default:
	}
	return b.String(), nil
}

func doArgs() {
	flag.Var(&authors, "author",
		`specify an author of the paper (can be specified multiple times)
Default: One random author`)
	flag.StringVar(&mode, "mode", defaultMode,
		`what to output
 pdf: PDF file
 zip: Zip file with LaTeX/BiBTeX source and PDF
 dir: leave source and PDF in directory specified with --dir
 view: invoke viewer on PDF file
 raw: output raw TeX/txt only (required for product=blurb)`)
	flag.StringVar(&viewer, "viewer", defaultViewer, "program to use as PDF viewer")
	flag.StringVar(&dir, "dir", "", "specify output directory when mode=dir")
	flag.StringVar(&output, "output", "",
		`specify output file when mode=pdf|zip|raw
 use - for stdout`)
	flag.StringVar(&product, "product", defaultProduct, "what to generate")
	flag.Int64Var(&seed, "seed", 0, "PRNG seed")
	flag.Var(&verbosity, "verbosity", "enable verbosity features")
	flag.Parse()

	if _, ok := modes[mode]; !ok {
		fmt.Fprintf(flag.CommandLine.Output(), "unrecognized mode \"%s\"\n", mode)
		flag.Usage()
		os.Exit(2)
	}
	if _, ok := products[product]; !ok {
		fmt.Fprintf(flag.CommandLine.Output(), "unrecognized product \"%s\"\n", product)
		flag.Usage()
		os.Exit(2)
	}
	// blurb mode is a hack because the output is text not tex
	if product == "blurb" && mode != "raw" {
		fmt.Fprintf(flag.CommandLine.Output(), "--product=blurb requires --mode=raw\n")
		flag.Usage()
		os.Exit(2)
	}
	if slices.Contains([]string{"pdf", "zip", "raw"}, mode) && len(output) == 0 {
		fmt.Fprintf(flag.CommandLine.Output(), "--output requires --mode=raw\n")
		flag.Usage()
		os.Exit(2)
	}
}

// if true, must defer os.RemoveAll(dir)
func setupDir() bool {
	if len(dir) == 0 {
		pid := strconv.Itoa(os.Getpid())
		seedS := strconv.FormatInt(seed, 10)
		var err error
		patternBase := []string{"mathgen-go.", pid, "-", seedS, "."}
		mktempPattern := strings.Join(patternBase, "")
		dir, err = os.MkdirTemp(os.TempDir(), mktempPattern)
		if err != nil {
			panic(fmt.Sprintf("mkdirtemp: %v", err))
		}
		if verbosity >= mathgen.Verbose {
			return false
		}
		return true
	}
	return false
}

func outputFh() (io.Writer, error) {
	switch output {
	case "-":
		return os.Stdout, nil
	case "":
		return nil, nil
	default:
		fh, err := os.Create(output)
		if err != nil {
			return nil, fmt.Errorf("CreateOutput %s: %w", output, err)
		}
		return fh, nil
	}
}

func mustGetWd() string {
	thisDir, err := os.Getwd()
	if err != nil {
		panic(fmt.Errorf("getwd: %w", err))
	}
	return thisDir
}
func requireInWorkDir() {
	thisDir := mustGetWd()
	if thisDir != dir {
		panic(fmt.Errorf("expected dir %s but got %s", dir, thisDir))
	}
}
func copyPdf(out io.Writer, inPdfName string) error {
	requireInWorkDir()
	var err error
	var in *os.File
	in, err = os.Open(inPdfName)
	if err != nil {
		return fmt.Errorf("copyPdf: open input %s: %w", inPdfName, err)
	}
	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("copyPdf: copy: %w", err)
	}
	if err = in.Close(); err != nil {
		return fmt.Errorf("copyPdf: close input: %w", err)
	}
	return nil
}
func makeZip(out io.Writer, files []string) error {
	printVerboseF("makeZip")
	requireInWorkDir()
	zw := zip.NewWriter(out)
	for _, fName := range files {
		var err error
		var outF io.Writer
		var inF *os.File
		printVerboseF("makeZip: begin %s", fName)
		if inF, err = os.Open(fName); err != nil {
			return fmt.Errorf("Zip: OpenInput %s: %w", fName, err)
		}
		if outF, err = zw.Create(fName); err != nil {
			return fmt.Errorf("Zip: CreateEntry %s: %w", fName, err)
		}
		if _, err = io.Copy(outF, inF); err != nil {
			return fmt.Errorf("Zip: Copy %s: %w", fName, err)
		}
		if err = inF.Close(); err != nil {
			return fmt.Errorf("Zip: CloseInput %s: %w", fName, err)
		}
	}
	printVerboseF("makeZip: finish")
	if err := zw.Close(); err != nil {
		return fmt.Errorf("Zip: Close: %w", err)
	}
	return nil
}
func writeToFh(contents string, fh io.Writer) error {
	printVerboseF("writeToFh: (len=%d)", len(contents))
	if _, err := fh.Write([]byte(contents)); err != nil {
		return fmt.Errorf("writeToFh: %w", err)
	}
	return nil
}
func writeToFile(contents, filename string) error {
	printVerboseF("writeToFile: %s", filename)
	if mode != "raw" {
		requireInWorkDir()
	}
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("writeToFile: open %s: %w", filename, err)
	}
	if err = writeToFh(contents, f); err != nil {
		return fmt.Errorf("writeToFile: write %s: %w", filename, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("writeToFile: close %s: %w", filename, err)
	}
	return nil
}

func runApp(cmd string, a ...string) error {
	printVerboseF("runApp: %s %v", cmd, a)
	c := exec.Command(cmd, a...)
	out, err := c.CombinedOutput()
	if err != nil {
		if execErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s\n[dir=%s] %s failed: %s", out, dir, c, execErr.Error())
		}
		return err
	}
	return nil
}

func generateOutput(g *mathgen.Generator) error {
	var err error
	text := g.GeneratePrettyString(products[product])
	if mode == "raw" {
		var ofh io.Writer
		if ofh, err = outputFh(); err != nil {
			return err
		}
		if err = writeToFh(text, ofh); err != nil {
			return err
		}
		ofhFile, ok := ofh.(*os.File)
		if ok {
			if err = ofhFile.Close(); err != nil {
				return fmt.Errorf("close %s: %w", output, err)
			}
		}
		return nil
	}
	/* if dir was unspecified, we used os.MkdirTemp(), which invoked os.Mkdir(), so
	 * cleanup should be done even if os.Chdir() fails
	 */
	if setupDir() {
		defer os.RemoveAll(dir)
	}
	printVerboseF("dir: %s", dir)
	oldDir := mustGetWd()
	if err = os.Chdir(dir); err != nil {
		/* dir may be user controlled without validation, and if it was validated, it would
		 * be prone a TOCTOU;
		 * fail as if invalid argument instead of unexpected state
		 */
		return fmt.Errorf("chdir %s: %v", dir, err)
	}
	defer os.Chdir(oldDir)
	basename := "mathgen-" + strconv.FormatInt(seed, 10)
	if err = writeToFile(text, basename+".tex"); err != nil {
		return err
	}
	bibText := g.GenerateBibtex(text)
	if err = writeToFile(bibText, bibName); err != nil {
		return err
	}
	if err = runApp("pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	if err = runApp("bibtex", basename); err != nil {
		return err
	}
	if product == "book" {
		if err = runApp("makeindex", basename+".idx"); err != nil {
			return err
		}
	}
	if err = runApp("pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	if err = runApp("pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	var readmeText string
	if readmeText, err = generateReadmeText(product, basename); err != nil {
		return fmt.Errorf("generateReadmeText: %w", err)
	}
	if err = writeToFile(readmeText, "README"); err != nil {
		return err
	}

	// use ofh when mode is pdf or zip
	var ofh io.Writer
	if ofh, err = outputFh(); err != nil {
		return err
	}
	switch mode {
	case "pdf":
		if err = copyPdf(ofh, basename+".pdf"); err != nil {
			return err
		}
	case "zip":
		if err = makeZip(ofh, []string{
			basename + ".tex", basename + ".pdf", bibName, "README",
		}); err != nil {
			return err
		}
	}
	ofhFile, ok := ofh.(*os.File)
	if ok {
		if err = ofhFile.Close(); err != nil {
			return fmt.Errorf("close %s: %w", output, err)
		}
	}
	if mode == "view" {
		runApp(viewer, basename+".pdf") // discard error
	}

	return nil
	// (defer restores cwd here)
}

func main() {
	doArgs()
	gb := mathgen.NewGeneratorBuilder()
	if seed > 0 {
		gb.SetRngSeed(seed)
	} else {
		seed = gb.RngSeed()
	}
	printVerboseF("seed = %d", seed)
	if len(authors) > 0 {
		gb.SetAuthors(authors)
	}
	if verbosity != mathgen.None {
		gb.SetVerbosity(verbosity)
	}
	ruleFileName := fmt.Sprintf("%s/sci%s.in", dataDir, product)
	fh, err := os.Open(ruleFileName)
	if err != nil {
		panic(fmt.Errorf("open %s: %w", ruleFileName, err))
	}
	gb.SetInputStream(fh)
	g := gb.Build()
	if err = generateOutput(g); err != nil {
		// we're in main and have no desire for more refined error handling
		panic(err)
	}
}
