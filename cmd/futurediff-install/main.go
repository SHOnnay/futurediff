package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/installer"
)

func main() {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	source := flag.String("source-dir", filepath.Dir(exe), "directory containing FutureDiff binaries")
	prefix := flag.String("prefix", filepath.Join(home, ".local"), "installation prefix")
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	socket := flag.String("socket", "", "daemon socket path")
	service := flag.String("service", string(installer.DefaultService()), "none, systemd-user, or launchd-user")
	runtimeKind := flag.String("runtime", "", "optional docker or podman runtime")
	image := flag.String("runtime-image", "", "optional digest-pinned runtime image")
	creds := flag.String("credential-config", "", "optional absolute credential metadata path")
	approvalKeyring := flag.String("approval-keyring", "", "optional absolute operator approval keyring")
	requireSigned := flag.Bool("require-signed-approvals", false, "require signed operator approvals")
	approvalQuorum := flag.String("approval-quorum-policy", "", "optional absolute approval quorum policy")
	evidenceKey := flag.String("evidence-key", "", "optional absolute AES-GCM evidence key")
	evidenceKeyring := flag.String("evidence-keyring", "", "optional absolute evidence keyring")
	secretPolicy := flag.String("secret-scan-policy", "", "optional absolute secret-scan policy")
	quotaPolicy := flag.String("quota-policy", "", "optional absolute quota policy")
	ratePolicy := flag.String("rate-policy", "", "optional absolute rate policy")
	configSigningKeyring := flag.String("config-signing-keyring", "", "optional trusted keyring for signed configuration attestations")
	requireSignedConfigs := flag.Bool("require-signed-configs", false, "require signed attestations for configured security files")
	allowedPeerUIDs := flag.String("allowed-peer-uids", "", "optional comma-separated Unix peer UIDs")
	disablePeerAuth := flag.Bool("disable-peer-auth", false, "disable kernel peer authentication")
	dry := flag.Bool("dry-run", false, "print plan without writing")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	opts := installer.Options{SourceDir: *source, Prefix: *prefix, DataRoot: *root, Socket: *socket, Service: installer.ServiceKind(*service), Runtime: *runtimeKind, RuntimeImage: *image, CredentialConfig: *creds, ApprovalKeyring: *approvalKeyring, ApprovalQuorumPolicy: *approvalQuorum, RequireSignedApprovals: *requireSigned, EvidenceKey: *evidenceKey, EvidenceKeyring: *evidenceKeyring, SecretScanPolicy: *secretPolicy, QuotaPolicy: *quotaPolicy, RatePolicy: *ratePolicy, ConfigSigningKeyring: *configSigningKeyring, RequireSignedConfigs: *requireSignedConfigs, AllowedPeerUIDs: *allowedPeerUIDs, DisablePeerAuth: *disablePeerAuth, DryRun: *dry}
	plan, err := installer.BuildPlan(opts)
	if err != nil {
		fatal(err)
	}
	if err := installer.EncodePlan(os.Stdout, plan); err != nil {
		fatal(err)
	}
	if err := installer.Apply(plan); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
