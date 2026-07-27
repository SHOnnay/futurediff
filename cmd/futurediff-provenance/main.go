package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/release"
)

type values []string

func (v *values) String() string     { return strings.Join(*v, ",") }
func (v *values) Set(s string) error { *v = append(*v, s); return nil }

func main() {
	var artifacts values
	flag.Var(&artifacts, "artifact", "artifact path; repeat for multiple subjects")
	output := flag.String("output", "-", "output JSONL file or -")
	builder := flag.String("builder-id", "", "builder identity URI")
	invocation := flag.String("invocation-id", os.Getenv("GITHUB_RUN_ID"), "build invocation identity")
	source := flag.String("source-uri", os.Getenv("GITHUB_SERVER_URL")+"/"+os.Getenv("GITHUB_REPOSITORY"), "source repository URI")
	sourceDigest := flag.String("source-digest", os.Getenv("GITHUB_SHA"), "source Git commit")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *builder == "" {
		if ref := os.Getenv("GITHUB_WORKFLOW_REF"); ref != "" {
			*builder = "https://github.com/" + ref
		} else {
			*builder = "https://futurediff.dev/builders/local-go/v1"
		}
	}
	now := time.Now().UTC()
	stmt, err := release.GenerateProvenance(release.ProvenanceOptions{Artifacts: artifacts, BuilderID: *builder, InvocationID: *invocation, SourceURI: strings.TrimSuffix(*source, "/"), SourceDigest: *sourceDigest, StartedOn: now, FinishedOn: now})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := release.WriteProvenance(*output, stmt); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
