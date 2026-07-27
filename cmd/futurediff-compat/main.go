package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/compatibility"
	"os"
)

func main() {
	manifest := flag.String("manifest", "", "compatibility manifest")
	out := flag.String("output", "", "optional report output")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		print(buildinfo.Current())
		return
	}
	if *manifest == "" {
		fail(fmt.Errorf("--manifest is required"))
	}
	r, e := compatibility.Run(*manifest)
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
func print(v any)  { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
