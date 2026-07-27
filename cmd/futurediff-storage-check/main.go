package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/storageguard"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	policyPath := flag.String("policy", "", "storage policy JSON")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	if *policyPath == "" {
		fail(fmt.Errorf("policy required"))
	}
	p, e := storageguard.Load(*policyPath)
	if e != nil {
		fail(e)
	}
	s, e := storageguard.Evaluate(*root, p, nil, time.Now())
	if e != nil {
		fail(e)
	}
	emit(s)
	if !s.Healthy {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
