package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"os"
)

func main() {
	baselinePath := flag.String("baseline", "", "baseline contract JSON")
	candidatePath := flag.String("candidate", "", "candidate contract JSON; defaults to local current contract")
	socket := flag.String("socket", "", "load candidate contract from daemon socket")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *baselinePath == "" {
		fail(fmt.Errorf("--baseline is required"))
	}
	base := loadFile(*baselinePath)
	if e := apicontract.Validate(base); e != nil {
		fail(fmt.Errorf("baseline: %w", e))
	}
	candidate := apicontract.Current()
	if *candidatePath != "" && *socket != "" {
		fail(fmt.Errorf("use only one of --candidate or --socket"))
	}
	if *candidatePath != "" {
		candidate = loadFile(*candidatePath)
	} else if *socket != "" {
		raw, e := api.NewClient(*socket).Do("GET", "/v1/contract", nil)
		if e != nil {
			fail(e)
		}
		if e = json.Unmarshal(raw, &candidate); e != nil {
			fail(e)
		}
	}
	if e := apicontract.Validate(candidate); e != nil {
		fail(fmt.Errorf("candidate: %w", e))
	}
	r := apicontract.Diff(base, candidate)
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	if !r.Compatible {
		os.Exit(2)
	}
}
func loadFile(path string) apicontract.Contract {
	b, e := os.ReadFile(path)
	if e != nil {
		fail(e)
	}
	var c apicontract.Contract
	if e = json.Unmarshal(b, &c); e != nil {
		fail(e)
	}
	return c
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
