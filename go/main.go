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
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/m00shm00sh/mathgen/go/mathgen"
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
	modesA := strings.Fields(`pdf zip fullzip view raw`)
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
 fullzip: Zip file with LaTex/BiBTeX source and generated files
 view: invoke viewer on PDF file
 raw: output raw TeX/txt only (required for product=blurb)`)
	flag.StringVar(&viewer, "viewer", defaultViewer, "program to use as PDF viewer")
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

func setupDir() (string, error) {
	pid := strconv.Itoa(os.Getpid())
	seedS := strconv.FormatInt(seed, 10)
	var err error
	patternBase := []string{"mathgen-go.", pid, "-", seedS, "."}
	mktempPattern := strings.Join(patternBase, "")
	dir, err := os.MkdirTemp(os.TempDir(), mktempPattern)
	if err != nil {
		return "", fmt.Errorf("mkdirtemp: %v", err)
	}
	return dir, nil
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

func copyPdf(out io.Writer, inPdfName string) error {
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

func generateOutput(g *mathgen.GeneratorWorker) error {
	var err error
	text := g.GeneratePrettyString(products[product])
	var workDir string
	var ofh io.Writer
	// get output fh before chdir
	if ofh, err = outputFh(); err != nil {
		return err
	}
	defer func() {
		// close file in all cases
		ofhFile, isFile := ofh.(*os.File)
		if isFile {
			if err = ofhFile.Close(); err != nil {
				panic(fmt.Errorf("close %s: %w", output, err))
			}
		}
		if err == nil {
			return
		}
		// undo write in error case
		if isFile {
			if err = os.Remove(output); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					panic(fmt.Errorf("close %s: %w", output, err))
				}
			}
		}
	}()
	if mode == "raw" {
		err = writeToFh(text, ofh)
		return err
	}
	workDir, err = setupDir()
	if err != nil {
		return fmt.Errorf("setupdir: %w", err)
	}
	if verbosity < mathgen.Verbose {
		defer os.RemoveAll(workDir)
	} else {
		printVerboseF("workdir: %s", workDir)
	}
	// for files, remove reliance on workdir by using a wrapper that prepends dir to path
	workFile := func(basename string) string { return filepath.Join(workDir, basename) }
	basename := "mathgen-" + strconv.FormatInt(seed, 10)
	if err = writeToFile(text, workFile(basename+".tex")); err != nil {
		return err
	}
	bibText := g.GenerateBibtex(text)
	if err = writeToFile(bibText, workFile(bibName)); err != nil {
		return err
	}
	if err = runApp(workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	if err = runApp(workDir, "bibtex", basename); err != nil {
		return err
	}
	if product == "book" {
		if err = runApp(workDir, "makeindex", basename+".idx"); err != nil {
			return err
		}
	}
	if err = runApp(workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	if err = runApp(workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return err
	}
	var readmeText string
	if readmeText, err = generateReadmeText(product, basename); err != nil {
		return fmt.Errorf("generateReadmeText: %w", err)
	}
	if err = writeToFile(readmeText, workFile("README")); err != nil {
		return err
	}

	switch mode {
	case "pdf":
		if err = copyPdf(ofh, workFile(basename+".pdf")); err != nil {
			return err
		}
	case "zip":
		if err = makeZip(ofh, []string{
			basename + ".tex", basename + ".pdf", bibName, "README",
		}); err != nil {
			return err
		}
	case "fullzip":
		var files []string
		files, err = filepath.Glob(workFile(basename + ".???"))
		if err != nil {
			return fmt.Errorf("glob: %w", err)
		}
		if err = makeZip(ofh, append(files, []string{bibName, "README"}...)); err != nil {
			return err
		}
	case "view":
		runApp(workDir, viewer, basename+".pdf") // ignore error
	}
	return nil
	// [defer] (restore cwd)
	// [defer] (rm -rf workdir)
	// [defer] (close output && delete if error)
}

func main() {
	doArgs()
	gb := mathgen.NewGeneratorBuilder()
	printVerboseF("seed = %d", seed)
	if verbosity != mathgen.None {
		gb.SetVerbosity(verbosity)
	}
	ruleFileName := fmt.Sprintf("%s/sci%s.in", dataDir, product)
	fh, err := os.Open(ruleFileName)
	if err != nil {
		panic(fmt.Errorf("open %s: %w", ruleFileName, err))
	}
	gb.Input = fh
	if product != "blurb" {
		gb.AddBibtexPlaceholder = true
	}
	g := gb.Build()
	gw := g.NewWorker(seed, authors)
	seed = gw.Seed()
	if err = generateOutput(gw); err != nil {
		// we're in main and have no desire for more refined error handling
		panic(err)
	}
}
