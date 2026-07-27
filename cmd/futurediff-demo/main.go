package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/demo"
)

func main() {
	root := flag.String("root", "", "demo directory; temporary when omitted")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	report, err := demo.Run(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	if !report.LiveCheckoutSafe {
		os.Exit(1)
	}
}
