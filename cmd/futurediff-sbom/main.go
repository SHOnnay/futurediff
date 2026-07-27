package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/release"
)

func main() {
	root := flag.String("root", ".", "repository root")
	output := flag.String("output", "-", "SPDX JSON output path or -")
	includeFiles := flag.Bool("files", true, "include file checksums")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	doc, err := release.GenerateSPDX(*root, *includeFiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := release.WriteSPDX(*output, doc); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
