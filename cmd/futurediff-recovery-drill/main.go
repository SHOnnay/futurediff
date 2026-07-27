package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/recoverydrill"
	"os"
)

func main() {
	input := flag.String("input", "", "optional recovery scenario JSON")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	var v any
	passed := true
	if *input == "" {
		r := recoverydrill.SelfTest()
		v = r
		passed = r.Passed
	} else {
		b, e := os.ReadFile(*input)
		if e != nil {
			fail(e)
		}
		var in recoverydrill.Input
		if e = json.Unmarshal(b, &in); e != nil {
			fail(e)
		}
		p, e := recoverydrill.Decide(in)
		if e != nil {
			fail(e)
		}
		v = map[string]any{"input": in, "plan": p}
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
	if !passed {
		os.Exit(2)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
