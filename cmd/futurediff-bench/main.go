package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SHOnnay/futurediff/internal/benchmark"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
)

func main() {
	scenarios := flag.String("scenarios", "examples/benchmark", "scenario directory")
	jsonOut := flag.String("json", "benchmark-report.json", "JSON report output")
	markdownOut := flag.String("markdown", "benchmark-report.md", "Markdown report output")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	loaded, err := benchmark.LoadDir(*scenarios)
	if err != nil {
		fail(err)
	}
	report, err := benchmark.Run(loaded)
	if err != nil {
		fail(err)
	}
	if err := benchmark.WriteJSON(*jsonOut, report); err != nil {
		fail(err)
	}
	if *markdownOut != "" {
		if err := os.WriteFile(*markdownOut, []byte(benchmark.Markdown(report)), 0o644); err != nil {
			fail(err)
		}
	}
	fmt.Printf("scenarios=%d report_digest=%s\n", len(report.Results), report.ReportDigest)
}
func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
