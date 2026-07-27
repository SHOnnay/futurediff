package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/repoadmission"
	"github.com/SHOnnay/futurediff/internal/staging"
	"os"
)

func main() {
	policy := flag.String("policy", "", "repository admission policy JSON")
	repo := flag.String("repository", "", "optional repository to evaluate")
	dirty := flag.String("dirty-policy", "reject", "reject or stage_from_head")
	flag.Parse()
	if *policy == "" {
		fatal(fmt.Errorf("--policy is required"))
	}
	p, err := repoadmission.Load(*policy)
	if err != nil {
		fatal(err)
	}
	out := any(p)
	if *repo != "" {
		in, err := (staging.Manager{}).Inspect(*repo, staging.DirtyPolicy(*dirty))
		if err != nil {
			fatal(err)
		}
		d := p.Evaluate(in, staging.DirtyPolicy(*dirty))
		out = d
		if !d.Allowed {
			encode(out)
			os.Exit(2)
		}
	}
	encode(out)
}
func encode(v any) {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
