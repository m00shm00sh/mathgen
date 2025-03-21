/*
 * fs.go - fs abstraction
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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type osFileOpener struct {
	dirs []string
}

func newOsFilereader(aux []string) *osFileOpener {
	dirs := []string{mustGetWd()}
	return &osFileOpener{dirs: append(dirs, aux...),}
}

func (o *osFileOpener) Open(name string) (fs.File, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		return nil, errors.New("directory in include not allowed")
	}
	for _, dir := range o.dirs {
		tryPath := filepath.Join(dir, name)
		if fh, err := os.Open(tryPath); err == nil {
			return fh, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fs.ErrNotExist
}


