/*
 * util.go - internal utility functions
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
	"os"
	"strings"
)

// if should-be-unreachable condition b holds true, panic msg
func unreachable(b bool, msg string) {
	if b {
		panic(msg)
	}
}

func cleanupNewlines(s string) string {
	return strings.ReplaceAll(s, "\n", "&")
}

func mustGetWd() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir
}

// backmap constructor for bimaps
func backMap[Map ~map[K]V, K comparable, V comparable](m Map) map[V]K {
	bm := make(map[V]K)
	for k := range m {
		v := m[k]
		bm[v] = k
	}
	return bm
}
