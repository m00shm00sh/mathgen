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
