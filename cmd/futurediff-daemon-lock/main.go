package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
)

func main() {
	root := flag.String("root", "", "FutureDiff data root")
	path := flag.String("lock-file", "", "daemon lock file; defaults to ROOT/daemon.lock")
	flag.Parse()
	if *root == "" && *path == "" {
		fatal(fmt.Errorf("--root or --lock-file is required"))
	}
	if *path == "" {
		*path = filepath.Join(*root, "daemon.lock")
	}
	status, err := daemonlock.Inspect(*path, time.Now())
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(status); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
