package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/policysim"
	"github.com/SHOnnay/futurediff/internal/verification"
	"os"
)

func main() {
	contractPath := flag.String("contract", "", "verification contract JSON")
	resultsPath := flag.String("results", "", "optional JSON object of check_id to status")
	assume := flag.Bool("assume-pass", false, "simulate unspecified checks as pass")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *contractPath == "" {
		fail(fmt.Errorf("--contract is required"))
	}
	b, e := os.ReadFile(*contractPath)
	if e != nil {
		fail(e)
	}
	c, e := verification.Parse(b)
	if e != nil {
		fail(e)
	}
	var results map[string]string
	if *resultsPath != "" {
		rb, e := os.ReadFile(*resultsPath)
		if e != nil {
			fail(e)
		}
		if e = json.Unmarshal(rb, &results); e != nil {
			fail(e)
		}
	}
	r, e := policysim.Explain(c, results, *assume)
	if e != nil {
		fail(e)
	}
	out, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(out))
	if r.Simulated && r.Outcome != "pass" {
		os.Exit(2)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
