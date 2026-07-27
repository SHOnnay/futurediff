package main

import (
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/platformsupport"
	"os"
)

func main() {
	output := flag.String("output", "-", "JSON report path or -")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	r := platformsupport.BuildReport()
	if err := platformsupport.WriteJSON(*output, r); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, platformsupport.Summary(r.Current))
	if r.Current.Level == platformsupport.Unsupported {
		os.Exit(1)
	}
}
