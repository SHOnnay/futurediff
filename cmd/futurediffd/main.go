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
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/configattest"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/drain"
	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/quota"
	"github.com/SHOnnay/futurediff/internal/ratelimit"
	"github.com/SHOnnay/futurediff/internal/repoadmission"
	"github.com/SHOnnay/futurediff/internal/rootaudit"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
	"github.com/SHOnnay/futurediff/internal/secretscan"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/storageguard"
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
	secretPolicyPath := flag.String("secret-scan-policy", "", "optional secret-scan policy JSON")
	quotaPolicyPath := flag.String("quota-policy", "", "optional resource quota policy JSON")
	ratePolicyPath := flag.String("rate-policy", "", "optional per-principal request rate policy JSON")
	repositoryPolicyPath := flag.String("repository-policy", "", "optional repository admission policy JSON")
	storagePolicyPath := flag.String("storage-policy", "", "optional storage-pressure policy JSON")
	authorizationPolicyPath := flag.String("authorization-policy", "", "optional UID-to-operation authorization policy JSON")
	capabilityKeyringPath := flag.String("capability-keyring", "", "trusted Ed25519 keyring for one-time delegated capabilities")
	configSigningKeyring := flag.String("config-signing-keyring", "", "trusted Ed25519 keyring for configuration attestations")
	requireSignedConfigs := flag.Bool("require-signed-configs", false, "require valid sidecar attestations for every configured security file")
	requireSecureRoot := flag.Bool("require-secure-root", true, "fail startup when the FutureDiff data root has unsafe ownership, permissions, symlinks, or special files")
	githubAPIBase := flag.String("github-api-base", "https://api.github.com", "GitHub API base URL for the built-in draft-PR adapter")
	slackAPIBase := flag.String("slack-api-base", "https://slack.com/api", "Slack API base URL for the built-in message outbox")
	pidFile := flag.String("pid-file", "", "daemon PID file")
	lockFile := flag.String("lock-file", "", "exclusive daemon lock file")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "maximum graceful drain duration")
	allowedPeerUIDs := flag.String("allowed-peer-uids", strconv.Itoa(os.Geteuid()), "comma-separated Unix UIDs allowed to access the daemon socket")
	disablePeerAuth := flag.Bool("disable-peer-auth", false, "disable kernel-authenticated local peer authorization (unsafe; not recommended)")
	version := flag.Bool("version", false, "print build information")
	requireIntegrity := flag.Bool("require-integrity", false, "fail closed at startup unless the offline ledger diagnosis proves the ledger is healthy")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if !*disablePeerAuth {
		if err := peerauth.CheckSupport(); err != nil {
			log.Fatalf("peer authentication: %v", err)
		}
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	if *pidFile == "" {
		*pidFile = filepath.Join(*root, "futurediff.pid")
	}
	if *lockFile == "" {
		*lockFile = filepath.Join(*root, "daemon.lock")
	}
	if err := os.MkdirAll(*root, 0o700); err != nil {
		log.Fatal(err)
	}
	if *requireSecureRoot {
		report := rootaudit.Audit(*root, os.Geteuid(), time.Now())
		if !report.Healthy {
			for _, check := range report.Checks {
				if check.Status == "fail" {
					log.Printf("data-root security failure: %s: %s", check.ID, check.Message)
				}
			}
			log.Fatal("FutureDiff data-root security audit failed")
		}
	}
	instanceLock, repo, err := openLedgerForStartup(*root, *lockFile, *requireIntegrity)
	if err != nil {
		log.Fatal(err)
	}
	defer instanceLock.Release()
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
	var configTrust *operatorapproval.Keyring
	if *configSigningKeyring != "" {
		ring, err := operatorapproval.LoadKeyring(*configSigningKeyring)
		if err != nil {
			log.Fatalf("configuration signing keyring: %v", err)
		}
		configTrust = &ring
	} else if *requireSignedConfigs {
		log.Fatal("--require-signed-configs requires --config-signing-keyring")
	}
	verifyConfig := func(kind, path string) {
		if path == "" {
			return
		}
		sidecar := configattest.SidecarPath(path)
		_, statErr := os.Stat(sidecar)
		if statErr != nil {
			if os.IsNotExist(statErr) && !*requireSignedConfigs {
				return
			}
			log.Fatalf("configuration attestation for %s: %v", kind, statErr)
		}
		if configTrust == nil {
			log.Fatalf("configuration attestation for %s exists but no --config-signing-keyring is configured", kind)
		}
		if _, err := configattest.VerifySidecar(*configTrust, path, kind, time.Now()); err != nil {
			log.Fatalf("configuration attestation for %s: %v", kind, err)
		}
	}
	verifyConfig("credential_config", *credentialConfig)
	verifyConfig("approval_keyring", *approvalKeyring)
	verifyConfig("approval_quorum_policy", *approvalQuorumPolicy)
	verifyConfig("evidence_key", *evidenceKey)
	verifyConfig("evidence_keyring", *evidenceKeyring)
	verifyConfig("secret_scan_policy", *secretPolicyPath)
	verifyConfig("quota_policy", *quotaPolicyPath)
	verifyConfig("rate_policy", *ratePolicyPath)
	verifyConfig("repository_policy", *repositoryPolicyPath)
	verifyConfig("storage_policy", *storagePolicyPath)
	verifyConfig("authorization_policy", *authorizationPolicyPath)
	verifyConfig("capability_keyring", *capabilityKeyringPath)

	var authorizer *authorization.Authorizer
	if *authorizationPolicyPath != "" {
		policy, err := authorization.Load(*authorizationPolicyPath)
		if err != nil {
			log.Fatalf("authorization policy: %v", err)
		}
		authorizer, err = authorization.Compile(policy)
		if err != nil {
			log.Fatalf("authorization policy: %v", err)
		}
		log.Printf("authorization policy configured: digest=%s bindings=%d", authorizer.Digest(), len(policy.Bindings))
	}
	var capabilityKeys *operatorapproval.Keyring
	if *capabilityKeyringPath != "" {
		ring, err := operatorapproval.LoadKeyring(*capabilityKeyringPath)
		if err != nil {
			log.Fatalf("capability keyring: %v", err)
		}
		capabilityKeys = &ring
		if authorizer == nil {
			log.Fatal("--capability-keyring requires --authorization-policy")
		}
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
	secretScanner := secretscan.Default()
	if *secretPolicyPath != "" {
		policy, err := secretscan.LoadPolicy(*secretPolicyPath)
		if err != nil {
			log.Fatalf("secret-scan policy: %v", err)
		}
		secretScanner.Policy = policy
	}
	quotaPolicy := quota.Default()
	if *quotaPolicyPath != "" {
		loaded, err := quota.Load(*quotaPolicyPath)
		if err != nil {
			log.Fatalf("quota policy: %v", err)
		}
		quotaPolicy = loaded
	}
	ratePolicy := ratelimit.Default()
	if *ratePolicyPath != "" {
		loaded, err := ratelimit.Load(*ratePolicyPath)
		if err != nil {
			log.Fatalf("rate policy: %v", err)
		}
		ratePolicy = loaded
	}
	var repositoryPolicy *repoadmission.Policy
	if *repositoryPolicyPath != "" {
		loaded, err := repoadmission.Load(*repositoryPolicyPath)
		if err != nil {
			log.Fatalf("repository policy: %v", err)
		}
		repositoryPolicy = &loaded
	}
	var storageGuard *storageguard.Guard
	if *storagePolicyPath != "" {
		loaded, err := storageguard.Load(*storagePolicyPath)
		if err != nil {
			log.Fatalf("storage policy: %v", err)
		}
		storageGuard = &storageguard.Guard{Root: *root, Policy: loaded, CacheTTL: 2 * time.Second}
		status, err := storageGuard.Status(time.Now())
		if err != nil {
			log.Fatalf("storage policy preflight: %v", err)
		}
		if !status.Healthy {
			log.Printf("storage policy starts unhealthy; mutations will be blocked: %v", status.Findings)
		}
	}
	rateLimiter, err := ratelimit.New(ratePolicy)
	if err != nil {
		log.Fatalf("rate limiter: %v", err)
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
		Verifier:               verification.Engine{AllowLocalCommands: false, OCI: runner, SecretScanner: secretScanner},
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
		Quotas:                 quotaPolicy,
		RepositoryPolicy:       repositoryPolicy,
	}
	if err := writePIDFile(*pidFile, os.Getpid()); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(*pidFile)
	peerUIDs, err := parsePeerUIDs(*allowedPeerUIDs)
	if err != nil {
		log.Fatalf("allowed peer UIDs: %v", err)
	}
	auditRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatalf("operator audit root: %v", err)
	}
	server := &api.Server{Service: svc, SocketPath: *socket, Maintenance: &maintenance.Manager{Path: filepath.Join(*root, "maintenance.json")}, Drain: drain.New(), RequirePeerCredentials: !*disablePeerAuth, AllowedPeerUIDs: peerUIDs, RateLimiter: rateLimiter, StorageGuard: storageGuard, Authorizer: authorizer, CapabilityKeyring: capabilityKeys, OperatorAudit: &operatoraudit.Store{Root: auditRoot}}
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

func parsePeerUIDs(raw string) (map[uint32]struct{}, error) {
	result := map[uint32]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid UID %q: %w", part, err)
		}
		result[uint32(value)] = struct{}{}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one allowed peer UID is required")
	}
	return result, nil
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
