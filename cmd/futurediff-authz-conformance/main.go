package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/authzconformance"
	"os"
	"time"
)

func main() {
	policyPath := flag.String("policy", "", "authorization policy JSON")
	flag.Parse()
	if *policyPath == "" {
		fatal(fmt.Errorf("--policy is required"))
	}
	p, err := authorization.Load(*policyPath)
	if err != nil {
		fatal(err)
	}
	dir, err := os.MkdirTemp("", "futurediff-authz-conformance-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(dir)
	r := authzconformance.Run(p, dir, time.Now())
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	if !r.Conformant {
		os.Exit(1)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
