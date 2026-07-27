package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/threatmodel"
	"os"
	"time"
)

func main() {
	out := flag.String("output", "", "optional report output")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		printJSON(buildinfo.Current())
		return
	}
	r, err := threatmodel.Run(time.Now())
	if err != nil {
		fail(err)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	b = append(b, '\n')
	if *out != "" {
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			fail(err)
		}
	} else {
		fmt.Print(string(b))
	}
	if !r.Secure {
		os.Exit(1)
	}
}
func fail(err error)  { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
