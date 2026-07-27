package main

import (
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/agentbench"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"os"
)

type values []string

func (v *values) String() string     { return fmt.Sprint([]string(*v)) }
func (v *values) Set(s string) error { *v = append(*v, s); return nil }
func main() {
	var inputs values
	flag.Var(&inputs, "input", "measured run JSON file; repeat")
	baseline := flag.String("baseline", "direct", "baseline mode")
	jsonOut := flag.String("json", "agent-benchmark.json", "JSON report")
	mdOut := flag.String("markdown", "agent-benchmark.md", "Markdown report")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one --input is required")
		os.Exit(2)
	}
	runs, err := agentbench.Load(inputs)
	if err != nil {
		fatal(err)
	}
	report := agentbench.Build(runs, *baseline)
	if err := agentbench.WriteJSON(*jsonOut, report); err != nil {
		fatal(err)
	}
	if err := agentbench.WriteMarkdown(*mdOut, report); err != nil {
		fatal(err)
	}
	fmt.Printf("runs=%d modes=%d\n", len(report.Runs), len(report.Aggregates))
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
