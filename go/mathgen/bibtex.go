/*
 * bibtex.go - bibtex generator
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
	"maps"
	"regexp"
	"strings"
)

var bibtexExtractCitationRx = regexp.MustCompile(rxFlagI + `(cite\:\d+)[,\}]`)

func (g *Generator) GenerateBibtex(text string) string {
	labels := make(map[string]empty)
	for _, m := range bibtexExtractCitationRx.FindAllStringSubmatchIndex(text, -1) {
		labels[text[m[2]:m[3]]] = empty{}
	}

	var b strings.Builder
	// create the rule entry and save&regenerate tokenRx once for efficiency
	g.rules["CITE_LABEL_GIVEN"] = []string{""}
	defer delete(g.rules, "CITE_LABEL_GIVEN")
	oldRx := g.generateTokenRx()
	defer func() { g.tokenRx = oldRx }()
	for k := range maps.Keys(labels) {
		g.rules["CITE_LABEL_GIVEN"][0] = k
		b.WriteString(g.GeneratePrettyString("bibtex"))
	}
	return b.String()
}
