/*
 * driver.go - TeX driver and general file handler
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
	"archive/zip"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

type OutputMode int

const (
	Raw OutputMode = iota
	Pdf
	Zip
	ZipAll
)

var (
	modeNames = map[OutputMode]string{
		Raw:    "raw",
		Pdf:    "pdf",
		Zip:    "zip",
		ZipAll: "zipall",
	}
	modeNamesFromStr = backMap(modeNames)
)

// flag interface for OutputMode
func (o *OutputMode) String() string {
	mn, ok := modeNames[*o]
	if !ok {
		panic("invalid OutputMode")
	}
	return mn
}
func (o *OutputMode) Set(s string) error {
	m, ok := modeNamesFromStr[s]
	if !ok {
		return fmt.Errorf("invalid OutputMode string: %s", s)
	}
	*o = m
	return nil
}
func ModeNames() []string {
	return slices.Collect(maps.Keys(modeNamesFromStr))
}

type Product int

const (
	Blurb Product = iota
	Article
	Book
)

// lazy load products as necessary with once as our guard
type productEntry struct {
	once      sync.Once
	generator *Generator
}

var (
	productGen = map[Product]*productEntry{
		Blurb:   new(productEntry),
		Article: new(productEntry),
		Book:    new(productEntry),
	}
	productStr = map[Product]string{
		Blurb:   "blurb",
		Article: "article",
		Book:    "book",
	}
	productFromStr = backMap(productStr)
	productPretty  = map[Product]Pretty{
		Article: Platex,
		Book:    Platexbook,
		Blurb:   Pnone,
	}
)

func getGenerator(l loggable, p Product) *Generator {
	productGen[p].once.Do(func() {
		b := NewGeneratorBuilder()
		if l.logger == nil {
			b.loggable.verbosity = l.verbosity
		} else {
			b.loggable = l
		}
		b.AddBibtexPlaceholder = p != Blurb
		ruleFileName := filepath.Join(workDir, "sci"+productStr[p]+".in")
		fh, err := os.Open(ruleFileName)
		if err != nil {
			// panic because if we have a problem here, there's no useful way to continue
			panic(fmt.Errorf("open %s: %w", ruleFileName, err))
		}
		b.Input = fh
		g := b.Build()
		productGen[p].generator = g
	})
	return productGen[p].generator
}

// flag interface for Product
func (p *Product) String() string {
	pn, ok := productStr[*p]
	if !ok {
		panic("invalid Product")
	}
	return pn
}
func (p *Product) Set(s string) error {
	pv, ok := productFromStr[s]
	if !ok {
		return fmt.Errorf("invalid Product string: %s", s)
	}
	*p = pv
	return nil
}
func ProductNames() []string {
	return slices.Collect(maps.Keys(productFromStr))
}

const (
	defaultMode      = Pdf
	defaultProduct   = Article
	defaultVerbosity = None
	bibName          = "scigenbibfile.bib"
)

var (
	workDir = mustGetWd()
)

type Driver struct {
	loggable
	Product
	OutputMode
	Seed    int64
	Authors []string
}

func NewDriver() *Driver {
	return &Driver{
		Product:    defaultProduct,
		OutputMode: defaultMode,
	}
}

func generateReadmeText(p Product, name string) (string, error) {
	var b strings.Builder
	switch p {
	case Article:
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
	case Book:
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
		unreachable(true, "generateReadmeText should not be executed")
	}
	return b.String(), nil
}

// driver validation error type
type illegalArg string

const (
	blurbAndNotRaw            illegalArg = "product is blurb but output mode isn't raw"
	zipAllAndNotVerboseEnough illegalArg = "zipAll requires verbosity of verbose or higher"
	cannotProduceOutput       illegalArg = "don't know what kind of output to produce"
)

func (ia illegalArg) Error() string {
	return string(ia)
}

func (d *Driver) checkParams() error {
	if d.Product == Blurb && d.OutputMode != Raw {
		return blurbAndNotRaw
	}
	// ZipAll is useless unless debugging (and also a security hole because logs expose OS details)
	if d.OutputMode == ZipAll && d.verbosity < Verbose {
		return zipAllAndNotVerboseEnough
	}
	return nil
}

func getWorkDir(seed int64) (string, error) {
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

func makeZip(l *loggable, out io.Writer, seed int64, fNames []string) error {
	seedS := strconv.FormatInt(seed, 10)
	zDir := "mathgen-" + seedS
	zw := zip.NewWriter(out)
	for _, fName := range fNames {
		var err error
		var outF io.Writer
		var inF *os.File
		l.logVerboseF("makeZip: begin %s", fName)
		if inF, err = os.Open(fName); err != nil {
			return fmt.Errorf("Zip: OpenInput %s: %w", fName, err)
		}
		zfName := zDir + "/" + filepath.Base(fName)
		if outF, err = zw.Create(zfName); err != nil {
			return fmt.Errorf("Zip: CreateEntry %s: %w", zfName, err)
		}
		if _, err = io.Copy(outF, inF); err != nil {
			return fmt.Errorf("Zip: Copy %s: %w", fName, err)
		}
		if err = inF.Close(); err != nil {
			return fmt.Errorf("Zip: CloseInput %s: %w", fName, err)
		}
	}
	l.logVerboseF("makeZip: finish")
	if err := zw.Close(); err != nil {
		return fmt.Errorf("Zip: Close: %w", err)
	}
	return nil
}
func writeToFh(l *loggable, contents string, fh io.Writer) error {
	l.logVerboseF("writeToFh: (len=%d)", len(contents))
	if _, err := fh.Write([]byte(contents)); err != nil {
		return fmt.Errorf("writeToFh: %w", err)
	}
	return nil
}
func writeToFile(l *loggable, contents, filename string) error {
	l.logVerboseF("writeToFile: %s", filename)
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("writeToFile: open %s: %w", filename, err)
	}
	if err = writeToFh(l, contents, f); err != nil {
		return fmt.Errorf("writeToFile: write %s: %w", filename, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("writeToFile: close %s: %w", filename, err)
	}
	return nil
}

func runApp(l *loggable, dir string, cmd string, a ...string) error {
	l.logVerboseF("runApp: %s %v", cmd, a)
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

// generate output;
// if error occurs prior to output pipe generation, the error item is returned;
// if error occurs during pipe write, the error is propagated through the pipe
func (d *Driver) GenerateOutput() (io.Reader, error) {
	var err error
	if err = d.checkParams(); err != nil {
		return nil, err
	}
	generator := getGenerator(d.loggable, d.Product).NewWorker(d.Seed, d.Authors)
	var logP *loggable
	if d.loggable.logger != nil {
		logP = &d.loggable
	} else {
		logP = &loggable{
			logger:    log.Default(),
			verbosity: d.verbosity,
		}
	}
	seed := generator.Seed()
	text := generator.GeneratePrettyString(productPretty[d.Product])
	if d.OutputMode == Raw {
		ifh, ofh := io.Pipe()
		go func() {
			e := writeToFh(logP, text, ofh)
			ofh.CloseWithError(e)
		}()
		return ifh, nil
	}

	var workDir string
	workDir, err = getWorkDir(seed)
	if err != nil {
		return nil, fmt.Errorf("setupdir: %w", err)
	}
	logP.logVerboseF("workdir: %s", workDir)
	defer os.RemoveAll(workDir)
	// for files, remove reliance on workdir by using a wrapper that prepends dir to path
	workFile := func(basename string) string { return filepath.Join(workDir, basename) }
	workFileV := func(basenames ...string) []string {
		ret := make([]string, len(basenames))
		for i, bn := range basenames {
			ret[i] = filepath.Join(workDir, bn)
		}
		return ret
	}
	basename := "mathgen-" + strconv.FormatInt(seed, 10)

	if err = writeToFile(logP, text, workFile(basename+".tex")); err != nil {
		return nil, err
	}
	bibText := generator.GenerateBibtex(text)
	if err = writeToFile(logP, bibText, workFile(bibName)); err != nil {
		return nil, err
	}
	if err = runApp(logP, workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return nil, err
	}
	if err = runApp(logP, workDir, "bibtex", basename); err != nil {
		return nil, err
	}
	if d.Product == Book {
		if err = runApp(logP, workDir, "makeindex", basename+".idx"); err != nil {
			return nil, err
		}
	}
	if err = runApp(logP, workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return nil, err
	}
	if err = runApp(logP, workDir, "pdflatex", "-halt-on-error", basename); err != nil {
		return nil, err
	}
	var readmeText string
	if readmeText, err = generateReadmeText(d.Product, basename); err != nil {
		return nil, fmt.Errorf("generateReadmeText: %w", err)
	}
	if err = writeToFile(logP, readmeText, workFile("README")); err != nil {
		return nil, err
	}

	if d.OutputMode == Pdf {
		var ret io.Reader
		ret, err = os.Open(workFile(basename + ".pdf"))
		return ret, err
	}
	// make a list of files to populate the zip file with
	// for Zip, this will be ${workDir}/(${basename}.tex,${basename}.pdf,${bibname},README)
	// for ZipAll, this will be ${workDir}/*
	var zipFiles []string
	switch d.OutputMode {
	case Zip:
		zipFiles = workFileV(basename+".tex", basename+".pdf", bibName, "README")
	case ZipAll:
		zipFiles, err = filepath.Glob(workFile("*"))
		if err != nil {
			return nil, fmt.Errorf("glob: %w", err)
		}
	default:
		return nil, cannotProduceOutput
	}
	zipName := workFile("out.zip")
	var zipFh *os.File
	zipFh, err = os.Create(zipName)
	if err != nil {
		return nil, fmt.Errorf("zipopen: %w", err)
	}
	err = makeZip(logP, zipFh, seed, zipFiles)
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	zipFh.Close()
	return os.Open(zipName)
}
