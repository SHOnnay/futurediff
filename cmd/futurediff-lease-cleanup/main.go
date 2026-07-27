package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/leasecleanup"
	"os"
	"time"
)

func main() {
	root := flag.String("root", "", "FutureDiff data root")
	apply := flag.Bool("apply", false, "delete expired leases")
	confirm := flag.String("confirm", "", "exact confirmation phrase")
	flag.Parse()
	if *root == "" {
		fatal(fmt.Errorf("--root is required"))
	}
	r, err := leasecleanup.Run(*root, *apply, *confirm, time.Now())
	if err != nil {
		fatal(err)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(r); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
