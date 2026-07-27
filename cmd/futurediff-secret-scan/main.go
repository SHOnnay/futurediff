package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/SHOnnay/futurediff/internal/secretscan"
)

func main() {
	patch := flag.String("patch", "", "unified patch to scan")
	policyPath := flag.String("policy", "", "optional secret-scan policy JSON")
	flag.Parse()
	if *patch == "" {
		fmt.Fprintln(os.Stderr, "--patch is required")
		os.Exit(2)
	}
	scanner := secretscan.Default()
	if *policyPath != "" {
		policy, err := secretscan.LoadPolicy(*policyPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		scanner.Policy = policy
	}
	report, err := scanner.ScanPatchFile(*patch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if report.Blocking {
		os.Exit(3)
	}
}
