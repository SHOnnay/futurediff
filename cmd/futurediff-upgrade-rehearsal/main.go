package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/upgraderehearsal"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	out := flag.String("output", "", "optional JSON report")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		print(buildinfo.Current())
		return
	}
	r, e := upgraderehearsal.Run(*root)
	b, _ := json.MarshalIndent(r, "", "  ")
	if *out != "" {
		_ = os.WriteFile(*out, append(b, '\n'), 0o600)
	}
	fmt.Println(string(b))
	if e != nil {
		fmt.Fprintln(os.Stderr, "error:", e)
		os.Exit(1)
	}
}
func print(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
