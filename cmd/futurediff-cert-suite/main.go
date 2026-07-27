package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/certsuite"
)

type values []string

func (v *values) String() string         { return strings.Join(*v, ",") }
func (v *values) Set(value string) error { *v = append(*v, value); return nil }

func main() {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	binDir := filepath.Dir(exe)
	var targets values
	flag.Var(&targets, "target", "certification target; repeat or use comma-separated local,oci,github,slack,opencode,hermes,attestation,all")
	output := flag.String("output", "-", "JSON report path or -")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall certification timeout")
	scratch := flag.String("scratch", filepath.Join(home, ".futurediff", "certification-suite"), "certification scratch root")
	mcpBinary := flag.String("mcp-binary", filepath.Join(binDir, "futurediff-mcp"), "absolute futurediff-mcp path")
	socket := flag.String("socket", filepath.Join(home, ".futurediff", "futurediff.sock"), "daemon Unix socket path")
	ociRuntime := flag.String("oci-runtime", "docker", "docker or podman")
	ociBinary := flag.String("oci-binary", "", "optional OCI runtime binary")
	ociImage := flag.String("oci-image", "", "digest-pinned OCI certification image")
	githubOwner := flag.String("github-owner", "", "dedicated GitHub test-repository owner")
	githubRepo := flag.String("github-repo", "", "dedicated GitHub test-repository name")
	githubTokenEnv := flag.String("github-token-env", "", "environment-variable name containing the GitHub token")
	githubAPI := flag.String("github-api", "https://api.github.com", "GitHub API base")
	slackChannel := flag.String("slack-channel", "", "dedicated Slack test channel ID")
	slackTokenEnv := flag.String("slack-token-env", "", "environment-variable name containing the Slack token")
	slackAPI := flag.String("slack-api", "https://slack.com/api", "Slack API base")
	openCode := flag.String("opencode-binary", "", "OpenCode binary path")
	hermes := flag.String("hermes-binary", "", "Hermes binary path")
	gh := flag.String("gh-binary", "gh", "GitHub CLI binary")
	artifact := flag.String("attestation-artifact", "", "release artifact to verify")
	attestationRepo := flag.String("attestation-repo", "", "GitHub owner/repository for attestation verification")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := certsuite.Run(ctx, certsuite.Options{
		Targets: targets, ScratchRoot: *scratch, MCPBinary: *mcpBinary, DaemonSocket: *socket,
		OCIRuntime: *ociRuntime, OCIBinary: *ociBinary, OCIImage: *ociImage,
		GitHubOwner: *githubOwner, GitHubRepo: *githubRepo, GitHubTokenEnv: *githubTokenEnv, GitHubAPIBase: *githubAPI,
		SlackChannel: *slackChannel, SlackTokenEnv: *slackTokenEnv, SlackAPIBase: *slackAPI,
		OpenCodeBinary: *openCode, HermesBinary: *hermes,
		GHBinary: *gh, AttestationArtifact: *artifact, AttestationRepo: *attestationRepo,
	}, certsuite.Dependencies{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := certsuite.WriteJSON(*output, report); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if *output != "-" {
		fmt.Fprintln(os.Stderr, certsuite.Summary(report))
	}
	if !report.Certified {
		os.Exit(1)
	}
}
