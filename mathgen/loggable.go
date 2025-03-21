/*
 * loggable.go - internal logging
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
	"log"
	"slices"
)

type loggable struct {
	logger    *log.Logger
	verbosity Verbosity
}

func (l *loggable) SetLogger(log *log.Logger) *loggable {
	l.logger = log
	return l
}
func (l *loggable) SetVerbosity(v Verbosity) *loggable {
	l.verbosity = v
	return l
}

func (l *loggable) logPanic(v ...any) {
	l.logger.Panic(v...)
}
func (l *loggable) logInfo(v ...any) {
	if l.verbosity >= Info {
		a := slices.Concat([]any{"I:"}, v)
		l.logger.Println(a...)
	}
}
func (l *loggable) logVerboseF(fmt string, v ...any) {
	if l.verbosity >= Verbose {
		l.logger.Printf("V: "+fmt, v...)
	}
}
func (l *loggable) logDebugF(fmt string, v ...any) {
	if l.verbosity >= Debug {
		l.logger.Printf("D: "+fmt, v...)
	}
}
func (l *loggable) logDebugFunc(f func() string) {
	if l.verbosity >= Debug {
		l.logger.Println("D:", f())
	}
}
