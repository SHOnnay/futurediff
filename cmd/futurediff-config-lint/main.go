package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/configlint"
	"os"
)

func main() {
	kind := flag.String("kind", "auto", "auto, credentials, verification, agent-run, installer-plan, opencode, rate-policy, repository-policy, config-attestation, or json")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: futurediff-config-lint [--kind kind] <path>")
		os.Exit(1)
	}
	r := configlint.Lint(flag.Arg(0), *kind)
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	if !r.Valid {
		os.Exit(2)
	}
}
