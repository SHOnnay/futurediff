package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/drain"
	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
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
	approvalKeyring := flag.String("approval-keyring", "", "trusted Ed25519 operator approval keyring")
	requireSignedApprovals := flag.Bool("require-signed-approvals", false, "reject unsigned approval requests")
	approvalQuorumPolicy := flag.String("approval-quorum-policy", "", "optional approval quorum policy JSON")
	evidenceKey := flag.String("evidence-key", "", "0600 AES-256-GCM key file for runtime evidence encryption")
	evidenceKeyring := flag.String("evidence-keyring", "", "0600 evidence keyring supporting rotation")
	githubAPIBase := flag.String("github-api-base", "https://api.github.com", "GitHub API base URL for the built-in draft-PR adapter")
	slackAPIBase := flag.String("slack-api-base", "https://slack.com/api", "Slack API base URL for the built-in message outbox")
	pidFile := flag.String("pid-file", "", "daemon PID file")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "maximum graceful drain duration")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	if *pidFile == "" {
		*pidFile = filepath.Join(*root, "futurediff.pid")
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
	var approvalKeys *operatorapproval.Keyring
	if *approvalKeyring != "" {
		ring, err := operatorapproval.LoadKeyring(*approvalKeyring)
		if err != nil {
			log.Fatalf("approval keyring: %v", err)
		}
		approvalKeys = &ring
		log.Printf("operator approval keyring configured: keys=%d signed_required=%t", len(ring.Keys), *requireSignedApprovals)
	} else if *requireSignedApprovals {
		log.Fatal("--require-signed-approvals requires --approval-keyring")
	}
	var approvalQuorum *operatorapproval.QuorumPolicy
	if *approvalQuorumPolicy != "" {
		if approvalKeys == nil {
			log.Fatal("--approval-quorum-policy requires --approval-keyring")
		}
		policy, err := operatorapproval.LoadQuorumPolicy(*approvalQuorumPolicy)
		if err != nil {
			log.Fatalf("approval quorum policy: %v", err)
		}
		approvalQuorum = &policy
		log.Printf("operator approval quorum configured: threshold=%d", policy.Threshold)
	}
	if *evidenceKey != "" && *evidenceKeyring != "" {
		log.Fatal("--evidence-key and --evidence-keyring are mutually exclusive")
	}
	var evidenceCipher evidencecrypto.FileCipher
	if *evidenceKeyring != "" {
		ring, err := evidencecrypto.LoadKeyring(*evidenceKeyring)
		if err != nil {
			log.Fatalf("evidence keyring: %v", err)
		}
		evidenceCipher = ring
		log.Printf("runtime evidence encryption configured: active_key_id=%s", ring.ActiveKeyID())
	} else if *evidenceKey != "" {
		cipher, err := evidencecrypto.Load(*evidenceKey)
		if err != nil {
			log.Fatalf("evidence key: %v", err)
		}
		evidenceCipher = cipher
		log.Printf("runtime evidence encryption configured: key_id=%s", cipher.KeyID)
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
		Ledger:                 repo,
		Staging:                staging.Manager{RuntimeRoot: filepath.Join(*root, "runtime")},
		Verifier:               verification.Engine{AllowLocalCommands: false, OCI: runner},
		OCI:                    runner,
		Credentials:            broker,
		GitHub:                 &githubdraft.Adapter{BaseURL: *githubAPIBase, HTTPClient: githubHTTP},
		GitHubBranch:           &githubbranch.Adapter{},
		Slack:                  &slackoutbox.Adapter{BaseURL: *slackAPIBase, HTTPClient: slackHTTP},
		CoordinatorID:          fmt.Sprintf("daemon-%d", os.Getpid()),
		ApprovalKeys:           approvalKeys,
		ApprovalQuorum:         approvalQuorum,
		RequireSignedApprovals: *requireSignedApprovals || approvalQuorum != nil,
		EvidenceCipher:         evidenceCipher,
	}
	if err := writePIDFile(*pidFile, os.Getpid()); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(*pidFile)
	server := &api.Server{Service: svc, SocketPath: *socket, Maintenance: &maintenance.Manager{Path: filepath.Join(*root, "maintenance.json")}, Drain: drain.New()}
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
	case sig := <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		defer cancel()
		if err := server.DrainAndClose(ctx, "signal:"+sig.String()); err != nil {
			log.Printf("daemon shutdown: %v", err)
		}
	}
}

func writePIDFile(path string, pid int) error {
	if existing, err := os.ReadFile(path); err == nil {
		oldPID, parseErr := strconv.Atoi(strings.TrimSpace(string(existing)))
		if parseErr == nil && oldPID > 1 {
			err = syscall.Kill(oldPID, 0)
			if err == nil || err == syscall.EPERM {
				return fmt.Errorf("daemon pid file belongs to live process %d", oldPID)
			}
			if err != syscall.ESRCH {
				return fmt.Errorf("check existing daemon pid %d: %w", oldPID, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
