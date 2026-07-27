package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/providercert"
	"os"
	"strings"
	"time"
)

type values []string

func (v *values) String() string     { return strings.Join(*v, ",") }
func (v *values) Set(s string) error { *v = append(*v, s); return nil }
func main() {
	var targets values
	flag.Var(&targets, "target", "github or slack; repeat")
	confirm := flag.String("confirm-provider-mutations", "", "required exact confirmation phrase")
	nonce := flag.String("nonce", "", "unique run marker")
	out := flag.String("output", "-", "JSON report path or -")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	ghOwner := flag.String("github-owner", "", "dedicated test repo owner")
	ghRepo := flag.String("github-repo", "", "dedicated test repo name")
	ghToken := flag.String("github-token-env", "", "environment variable name containing token")
	ghAPI := flag.String("github-api", "https://api.github.com", "GitHub API base")
	slChannel := flag.String("slack-channel", "", "dedicated test channel ID")
	slToken := flag.String("slack-token-env", "", "environment variable name containing token")
	slAPI := flag.String("slack-api", "https://slack.com/api", "Slack API base")
	version := flag.Bool("version", false, "print build info")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	r, err := providercert.Run(ctx, providercert.Options{Targets: targets, Confirmation: *confirm, Nonce: *nonce, GitHubOwner: *ghOwner, GitHubRepo: *ghRepo, GitHubTokenEnv: *ghToken, GitHubAPIBase: *ghAPI, SlackChannel: *slChannel, SlackTokenEnv: *slToken, SlackAPIBase: *slAPI}, providercert.Dependencies{})
	if err != nil {
		fatal(err)
	}
	if err := providercert.WriteJSON(*out, r); err != nil {
		fatal(err)
	}
	if !r.Certified {
		os.Exit(1)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
