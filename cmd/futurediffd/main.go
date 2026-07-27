package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func main() {
	home, _ := os.UserHomeDir()
	defaultRoot := filepath.Join(home, ".futurediff")
	root := flag.String("root", defaultRoot, "FutureDiff data root")
	socket := flag.String("socket", "", "Unix socket path")
	runtimeKind := flag.String("runtime", "docker", "OCI runtime: docker or podman")
	runtimeBinary := flag.String("runtime-binary", "", "optional OCI runtime binary path")
	runtimeImage := flag.String("runtime-image", "", "digest-pinned image enabling enforced mode")
	credentialConfig := flag.String("credential-config", "", "0600 JSON credential metadata configuration; secret values remain in their configured source")
	githubAPIBase := flag.String("github-api-base", "https://api.github.com", "GitHub API base URL for the built-in draft-PR adapter")
	slackAPIBase := flag.String("slack-api-base", "https://slack.com/api", "Slack API base URL for the built-in message outbox")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	if err := os.MkdirAll(*root, 0o700); err != nil {
		log.Fatal(err)
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	var runner *runtimeoci.Runner
	if *runtimeImage != "" {
		kind := runtimeoci.Docker
		if *runtimeKind == "podman" {
			kind = runtimeoci.Podman
		} else if *runtimeKind != "docker" {
			log.Fatalf("unsupported runtime %q", *runtimeKind)
		}
		candidate := &runtimeoci.Runner{Kind: kind, Binary: *runtimeBinary, Policy: runtimeoci.DefaultPolicy(*runtimeImage), ScratchRoot: filepath.Join(*root, "oci-scratch")}
		backend, err := candidate.Ready(context.Background())
		if err != nil {
			log.Fatalf("OCI runtime is not enforced-ready: %v", err)
		}
		runner = candidate
		log.Printf("enforced OCI enabled: runtime=%s version=%s rootless=%t", backend.Kind, backend.Version, backend.Rootless)
	}
	githubRule, err := egress.RuleFromBase(*githubAPIBase, "GET", "POST")
	if err != nil {
		log.Fatalf("GitHub egress policy: %v", err)
	}
	slackRule, err := egress.RuleFromBase(*slackAPIBase, "GET", "POST")
	if err != nil {
		log.Fatalf("Slack egress policy: %v", err)
	}
	githubHTTP, err := egress.NewClient(egress.Policy{Rules: []egress.Rule{githubRule}})
	if err != nil {
		log.Fatalf("GitHub egress transport: %v", err)
	}
	slackHTTP, err := egress.NewClient(egress.Policy{Rules: []egress.Rule{slackRule}})
	if err != nil {
		log.Fatalf("Slack egress transport: %v", err)
	}
	var broker *credentials.Broker
	if *credentialConfig != "" {
		config, err := credentials.LoadConfig(*credentialConfig)
		if err != nil {
			log.Fatalf("credential config: %v", err)
		}
		broker, err = credentials.NewBroker(config, credentials.EnvironmentSource{}, repo, repo)
		if err != nil {
			log.Fatalf("credential broker: %v", err)
		}
		log.Printf("credential broker configured: adapters=%d credentials=%d", len(config.Adapters), len(config.Credentials))
	}
	svc := &app.Service{
		Ledger:        repo,
		Staging:       staging.Manager{RuntimeRoot: filepath.Join(*root, "runtime")},
		Verifier:      verification.Engine{AllowLocalCommands: false, OCI: runner},
		OCI:           runner,
		Credentials:   broker,
		GitHub:        &githubdraft.Adapter{BaseURL: *githubAPIBase, HTTPClient: githubHTTP},
		GitHubBranch:  &githubbranch.Adapter{},
		Slack:         &slackoutbox.Adapter{BaseURL: *slackAPIBase, HTTPClient: slackHTTP},
		CoordinatorID: fmt.Sprintf("daemon-%d", os.Getpid()),
	}
	server := &api.Server{Service: svc, SocketPath: *socket}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve() }()
	fmt.Printf("FutureDiff Go daemon listening on %s\n", *socket)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	case <-signals:
		if err := server.Close(); err != nil {
			log.Printf("daemon shutdown: %v", err)
		}
	}
}
