package main

import (
	"fmt"
	"github.com/m00shm00sh/mathgen/go/mathgen"
	"io"
	"os"
)

func main() {
	var fh io.Reader
	var err error
	if len(os.Args) >= 2 {
		fh, err = os.Open(os.Args[1])
		if err != nil {
			panic(err)
		}
	}
	gb := mathgen.NewGeneratorBuilder().SetStartToken("START")
	if fh != nil {
		gb = gb.SetInputStream(fh)
	}
	g := gb.Build()
	s := g.GenerateString()
	fmt.Println(s)
}
