/*
 * verbosity.go - Verbosity type
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
	"fmt"
)

type Verbosity int

const (
	None Verbosity = iota
	Info
	Verbose
	Debug
)

func (v *Verbosity) String() string {
	switch *v {
	case None:
		return "none"
	case Info:
		return "info"
	case Verbose:
		return "verbose"
	case Debug:
		return "debug"
	default:
		panic(fmt.Errorf("unhandled Verbosity value %d", *v))
	}
}
func (v *Verbosity) Set(value string) error {
	switch value {
	case "none":
		*v = None
	case "info":
		*v = Info
	case "verbose":
		*v = Verbose
	case "debug":
		*v = Debug
	default:
		return fmt.Errorf("unhandled Verbosity string \"%s\"", value)
	}
	return nil
}
