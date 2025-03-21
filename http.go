/*
 * http.go - http frontend
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"text/template"

	"github.com/joho/godotenv"

	"github.com/m00shm00sh/mathgen/mathgen"
)

func writePlaintext(s string, w http.ResponseWriter) {
	w.WriteHeader(200)
	io.WriteString(w, s)
}
func writeJson(v any, w http.ResponseWriter) {
	data, err := json.Marshal(v)
	if err != nil {
		write500(err, w)
		return
	}
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
func write400(e error, w http.ResponseWriter) {
	w.WriteHeader(400)
	io.WriteString(w, e.Error())
}
func write429(s string, w http.ResponseWriter) {
	w.WriteHeader(429)
	io.WriteString(w, s)
}
func write500(e error, w http.ResponseWriter) {
	w.WriteHeader(500)
	io.WriteString(w, e.Error())
}

var (
	mode2mime = map[mathgen.OutputMode]string{
		mathgen.Raw: "text/plain",
		mathgen.Pdf: "application/pdf",
		mathgen.Zip: "application/zip",
	}
	concurrentRenderCount atomic.Int32
	completedRenderCount atomic.Int32
	rateLimit int32
)

func renderFactory(p mathgen.Product, m mathgen.OutputMode) func (http.ResponseWriter, *http.Request) {
	return func (w http.ResponseWriter, r *http.Request) {
		if rateLimit > 0 && concurrentRenderCount.Load() >= rateLimit {
			write429("rate limit exceeded", w)
			return
		}
		qs := r.URL.Query()
		authors := qs["author"]
		seedS := qs.Get("seed")
		if seedS == "" {
			seedS = "0"
		}
		d := mathgen.NewDriver()
		d.Product = p
		d.OutputMode = m
		d.Authors = authors
		if seed, e := strconv.ParseInt(seedS, 10, 64); e != nil {
			write400(fmt.Errorf("decoding seed: %w", e), w)
			return
		} else {
			d.Seed = seed
		}
		concurrentRenderCount.Add(1)
		defer concurrentRenderCount.Add(-1)
		if fh, e := d.GenerateOutput(); e != nil {
			write500(fmt.Errorf("render: %w", e), w)
			return
		} else {
			w.Header().Set("Content-type", mode2mime[m])
			w.WriteHeader(200)
			io.Copy(w, fh) // ignore errors and don't bother logging bytes sent
			completedRenderCount.Add(1)	
		}
	}	
}

type statsT struct {
	Concurrent int32 `json:"concurrent"`
	Completed int32 `json:"completed"`
}



func doStats(w http.ResponseWriter, r *http.Request) {
	k := r.URL.Query().Get("key")
	switch k {
	case "concurrent":
		writePlaintext(strconv.FormatInt(int64(concurrentRenderCount.Load()), 10), w)
	case "completed":
		writePlaintext(strconv.FormatInt(int64(completedRenderCount.Load()), 10), w)
	case "":
		st := statsT {
			Concurrent: concurrentRenderCount.Load(),
			Completed: completedRenderCount.Load(),
		}
		writeJson(st, w)
	default:
		write400(errors.New("bad key"), w)
	}
}
		
func main() {
	godotenv.Load()
	if envRatelimit := os.Getenv("RATE_LIMIT"); envRatelimit != "" {
		if rateLimit_, e := strconv.ParseInt(envRatelimit, 10, 31); e != nil {
			panic(fmt.Errorf("read ratelimit: %w", e))
		} else {
			rateLimit = int32(rateLimit_)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func (w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.New("html").Parse(
`<!DOCTYPE html>
<html>
	<head><title>Mathgen: randomly generated math papers</title></head>
	<body>
	<p>
		A port of <a href="https://thatsmathematics.com/mathgen/">Mathgen</a> to Golang.<br/>
		Code available <a href="https://github.com/m00shm00sh/mathgen"/>here</a>.
	</p>
	<h2>Endpoints</h2>
	<table>
	{{range .}}
		<tr><td><a href="{{.Link}}">{{.Link}}</a></td><td>{{.Text}}</td></tr>
	{{end}}
	</table>
	<br/>
	<h2>Query parameters</h2>
	<ul>
		<li><code>author</code>: author name; use FAMOUS_AUTHOR for a random celebrity</li>
	</ul>
	</body>
</html>`))
		var b strings.Builder
		if err := t.Execute(&b, []struct{
			Link string
			Text string
			}{	{ "/article.pdf", "article (PDF)" },
				{ "/article.zip", "article (ZIP sources)" },
				{ "/book.pdf", "book (PDF) (please be patient)" },
				{ "/book.zip", "book (ZIP sources) (please be patient)" },
				{ "/blurb", "blurb (raw text)" },
				{ "/stats", "misc statistics" },
		}); err != nil {
			write500(err, w)
		} else {
			writePlaintext(b.String(), w)
		}
	})
	mux.HandleFunc("GET /article.pdf", renderFactory(mathgen.Article, mathgen.Pdf))
	mux.HandleFunc("GET /article.zip", renderFactory(mathgen.Article, mathgen.Zip))
	mux.HandleFunc("GET /book.pdf", renderFactory(mathgen.Book, mathgen.Pdf))
	mux.HandleFunc("GET /book.zip", renderFactory(mathgen.Book, mathgen.Zip))
	mux.HandleFunc("GET /blurb", renderFactory(mathgen.Blurb, mathgen.Raw))
	mux.HandleFunc("GET /stats", doStats)
	
	s := http.Server{Handler: mux, Addr: ":8080"}
	println("listening on :8080")
	s.ListenAndServe()
}
