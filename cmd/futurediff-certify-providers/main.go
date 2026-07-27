package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/certification"
)

func main() {
	output := flag.String("output", "-", "JSON report path or - for stdout")
	futurepackPath := flag.String("futurepack", "", "optional provider smoke futurepack output path")
	root := flag.String("root", "", "provider certification artifact root")
	githubBaseURL := flag.String("github-base-url", "https://api.github.com", "GitHub API base URL")
	githubOwner := flag.String("github-owner", "", "GitHub owner or organization for the controlled test repository")
	githubRepo := flag.String("github-repo", "", "GitHub repository for the controlled test repository")
	githubBase := flag.String("github-base", "", "GitHub base branch to freshness-check")
	githubExpectedSHA := flag.String("github-expected-sha", "", "stale expected SHA used to verify blocking")
	slackBaseURL := flag.String("slack-base-url", "https://slack.com/api", "Slack API base URL")
	slackChannel := flag.String("slack-channel", "", "Slack channel for the provider smoke message")
	slackText := flag.String("slack-text", "FutureDiff provider smoke benchmark", "Slack message text")
	slackThreadTS := flag.String("slack-thread-ts", "", "optional Slack thread timestamp")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	cfg := certification.LoadProviderConfigFromEnv()
	cfg.GitHubBaseURL = *githubBaseURL
	cfg.GitHubOwner = *githubOwner
	cfg.GitHubRepo = *githubRepo
	cfg.GitHubBaseBranch = *githubBase
	cfg.GitHubExpectedSHA = *githubExpectedSHA
	cfg.SlackBaseURL = *slackBaseURL
	cfg.SlackChannel = *slackChannel
	cfg.SlackText = *slackText
	cfg.SlackThreadTS = *slackThreadTS
	if *root == "" {
		home, _ := os.UserHomeDir()
		*root = filepath.Join(home, ".futurediff", "provider-certification")
	}
	report, err := certification.RunProviderSmoke(context.Background(), cfg, certification.ProviderOptions{Root: *root, FuturepackPath: *futurepackPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := certification.WriteProviderJSON(*output, report); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if !report.Certified {
		os.Exit(1)
	}
}
