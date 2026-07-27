package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/policybundle"
	"github.com/SHOnnay/futurediff/internal/verification"
	"os"
	"strings"
)

func main() {
	contract := flag.String("contract", "", "verification contract JSON")
	policyID := flag.String("policy-id", "", "policy identity")
	labels := flag.String("labels", "", "comma-separated labels")
	out := flag.String("output", "", "output .fdpolicy")
	verify := flag.String("verify", "", "verify .fdpolicy")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		print(buildinfo.Current())
		return
	}
	if *verify != "" {
		b, e := policybundle.Verify(*verify)
		must(e)
		sha, e := policybundle.FileSHA256(*verify)
		must(e)
		print(map[string]any{"verified": true, "manifest": b.Manifest, "archive_sha256": sha})
		return
	}
	if *contract == "" || *policyID == "" || *out == "" {
		must(fmt.Errorf("--contract, --policy-id, and --output are required"))
	}
	raw, e := os.ReadFile(*contract)
	must(e)
	c, e := verification.Parse(raw)
	must(e)
	m, e := policybundle.Build(c, *policyID, strings.Split(*labels, ","), *out)
	must(e)
	sha, e := policybundle.FileSHA256(*out)
	must(e)
	print(map[string]any{"output": *out, "manifest": m, "archive_sha256": sha})
}
func must(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, "error:", e)
		os.Exit(1)
	}
}
func print(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
