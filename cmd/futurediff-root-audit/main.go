package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SHOnnay/futurediff/internal/rootaudit"
)

func main() {
	root := flag.String("root", "", "FutureDiff data root")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "--root is required")
		os.Exit(2)
	}
	report := rootaudit.Audit(*root, os.Geteuid(), time.Now())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !report.Healthy {
		os.Exit(1)
	}
}
