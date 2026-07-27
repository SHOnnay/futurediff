package main

import (
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/releaseverify"
	"os"
)

func main() {
	source := flag.String("source", "", "release directory or .tar.gz")
	output := flag.String("output", "-", "JSON report path or -")
	gh := flag.String("gh-binary", "gh", "GitHub CLI path")
	repo := flag.String("attestation-repo", "", "owner/repository for signed attestation")
	require := flag.Bool("require-signed-attestation", false, "require GitHub signed attestation")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	r, err := releaseverify.Verify(releaseverify.Options{Source: *source, GHBinary: *gh, AttestationRepo: *repo, RequireSignedAttestation: *require})
	if err != nil {
		fatal(err)
	}
	if err := releaseverify.WriteJSON(*output, r); err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, releaseverify.Summary(r))
	if !r.Verified {
		os.Exit(1)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
