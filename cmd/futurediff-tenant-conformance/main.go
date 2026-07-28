package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/tenantconformance"
	"os"
)

func main() {
	dir := flag.String("work-dir", "", "optional work directory")
	flag.Parse()
	d := *dir
	cleanup := func() {}
	if d == "" {
		var err error
		d, err = os.MkdirTemp("", "futurediff-tenant-conformance-")
		if err != nil {
			fatal(err)
		}
		cleanup = func() { _ = os.RemoveAll(d) }
	}
	defer cleanup()
	r, err := tenantconformance.Run(d)
	if err != nil {
		fatal(err)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	if !r.Conformant {
		os.Exit(1)
	}
}
func fatal(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
