package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type ServiceKind string

const (
	ServiceNone    ServiceKind = "none"
	ServiceSystemd ServiceKind = "systemd-user"
	ServiceLaunchd ServiceKind = "launchd-user"
)

var DefaultBinaries = []string{
	"futurediff", "futurediffd", "futurediff-mcp", "futurediff-admin",
	"futurediff-bench", "futurediff-certify", "futurediff-cert-suite",
	"futurediff-demo", "futurediff-integrate", "futurediff-provenance",
	"futurediff-sbom", "futurediff-install", "futurediff-platform",
	"futurediff-agent-bench", "futurediff-verify-release", "futurediff-provider-cert",
	"futurediff-audit", "futurediff-prune", "futurediff-doctor", "futurediff-api-contract",
	"futurediff-export", "futurediff-restore", "futurediff-replay", "futurediff-config-lint", "futurediff-api-diff",
	"futurediff-effectspec", "futurediff-policy-explain", "futurediff-recovery-drill", "futurediff-metrics", "futurediff-support-bundle",
	"futurediff-approval", "futurediff-policy-bundle", "futurediff-diff", "futurediff-upgrade-rehearsal", "futurediff-compat",
	"futurediff-maintenance", "futurediff-evidence", "futurediff-timeline", "futurediff-threat-test",
	"futurediff-config-snapshot", "futurediff-approval-quorum", "futurediff-incident", "futurediff-drain",
	"futurediff-operator-receipt", "futurediff-retention-policy", "futurediff-effect-graph", "futurediff-slo", "futurediff-readiness",
	"futurediff-secret-scan", "futurediff-quota", "futurediff-api-audit",
	"futurediff-daemon-lock", "futurediff-rate-policy", "futurediff-config-sign", "futurediff-root-audit",
	"futurediff-ledger-maintain", "futurediff-integrity-checkpoint", "futurediff-lease-cleanup", "futurediff-repository-policy",
	"futurediff-expire", "futurediff-idempotency-gc", "futurediff-storage-check", "futurediff-openapi", "futurediff-backup-catalog",
}

type Options struct {
	SourceDir              string      `json:"source_dir"`
	Prefix                 string      `json:"prefix"`
	DataRoot               string      `json:"data_root"`
	Socket                 string      `json:"socket"`
	Service                ServiceKind `json:"service"`
	Runtime                string      `json:"runtime,omitempty"`
	RuntimeImage           string      `json:"runtime_image,omitempty"`
	CredentialConfig       string      `json:"credential_config,omitempty"`
	ApprovalKeyring        string      `json:"approval_keyring,omitempty"`
	ApprovalQuorumPolicy   string      `json:"approval_quorum_policy,omitempty"`
	EvidenceKey            string      `json:"evidence_key,omitempty"`
	EvidenceKeyring        string      `json:"evidence_keyring,omitempty"`
	SecretScanPolicy       string      `json:"secret_scan_policy,omitempty"`
	QuotaPolicy            string      `json:"quota_policy,omitempty"`
	RatePolicy             string      `json:"rate_policy,omitempty"`
	RepositoryPolicy       string      `json:"repository_policy,omitempty"`
	StoragePolicy          string      `json:"storage_policy,omitempty"`
	ConfigSigningKeyring   string      `json:"config_signing_keyring,omitempty"`
	RequireSignedConfigs   bool        `json:"require_signed_configs,omitempty"`
	AllowedPeerUIDs        string      `json:"allowed_peer_uids,omitempty"`
	DisablePeerAuth        bool        `json:"disable_peer_auth,omitempty"`
	RequireSignedApprovals bool        `json:"require_signed_approvals,omitempty"`
	DryRun                 bool        `json:"dry_run"`
}

type FileAction struct {
	Source string      `json:"source,omitempty"`
	Target string      `json:"target"`
	Mode   os.FileMode `json:"mode"`
	Kind   string      `json:"kind"`
}

type Plan struct {
	Options Options      `json:"options"`
	Actions []FileAction `json:"actions"`
	Notes   []string     `json:"notes"`
}

func DefaultService() ServiceKind {
	switch runtime.GOOS {
	case "linux":
		return ServiceSystemd
	case "darwin":
		return ServiceLaunchd
	default:
		return ServiceNone
	}
}

func (o Options) Normalize() (Options, error) {
	var err error
	if o.SourceDir == "" {
		o.SourceDir, err = os.Executable()
		if err != nil {
			return o, err
		}
		o.SourceDir = filepath.Dir(o.SourceDir)
	}
	if o.Prefix == "" {
		o.Prefix = filepath.Join(userHome(), ".local")
	}
	if o.DataRoot == "" {
		o.DataRoot = filepath.Join(userHome(), ".futurediff")
	}
	if o.Socket == "" {
		o.Socket = filepath.Join(o.DataRoot, "futurediff.sock")
	}
	if o.Service == "" {
		o.Service = DefaultService()
	}
	for _, p := range []string{o.SourceDir, o.Prefix, o.DataRoot, o.Socket} {
		if !filepath.IsAbs(p) {
			return o, fmt.Errorf("path must be absolute: %s", p)
		}
	}
	if o.CredentialConfig != "" && !filepath.IsAbs(o.CredentialConfig) {
		return o, errors.New("credential config must be an absolute path")
	}
	if o.EvidenceKeyring != "" && !filepath.IsAbs(o.EvidenceKeyring) {
		return o, errors.New("evidence keyring must be an absolute path")
	}
	if o.EvidenceKey != "" && !filepath.IsAbs(o.EvidenceKey) {
		return o, errors.New("evidence key must be an absolute path")
	}
	if o.ApprovalQuorumPolicy != "" && !filepath.IsAbs(o.ApprovalQuorumPolicy) {
		return o, errors.New("approval quorum policy must be an absolute path")
	}
	if o.ApprovalKeyring != "" && !filepath.IsAbs(o.ApprovalKeyring) {
		return o, errors.New("approval keyring must be an absolute path")
	}
	if o.SecretScanPolicy != "" && !filepath.IsAbs(o.SecretScanPolicy) {
		return o, errors.New("secret-scan policy must be an absolute path")
	}
	if o.QuotaPolicy != "" && !filepath.IsAbs(o.QuotaPolicy) {
		return o, errors.New("quota policy must be an absolute path")
	}
	if o.RatePolicy != "" && !filepath.IsAbs(o.RatePolicy) {
		return o, errors.New("rate policy must be an absolute path")
	}
	if o.RepositoryPolicy != "" && !filepath.IsAbs(o.RepositoryPolicy) {
		return o, errors.New("repository policy must be an absolute path")
	}
	if o.StoragePolicy != "" && !filepath.IsAbs(o.StoragePolicy) {
		return o, errors.New("storage policy must be an absolute path")
	}
	if o.ConfigSigningKeyring != "" && !filepath.IsAbs(o.ConfigSigningKeyring) {
		return o, errors.New("configuration signing keyring must be an absolute path")
	}
	if o.RequireSignedConfigs && o.ConfigSigningKeyring == "" {
		return o, errors.New("signed configurations require a configuration signing keyring")
	}
	if o.EvidenceKey != "" && o.EvidenceKeyring != "" {
		return o, errors.New("evidence key and evidence keyring are mutually exclusive")
	}
	if o.ApprovalQuorumPolicy != "" && o.ApprovalKeyring == "" {
		return o, errors.New("approval quorum policy requires an approval keyring")
	}
	if o.RequireSignedApprovals && o.ApprovalKeyring == "" {
		return o, errors.New("signed approvals require an approval keyring")
	}
	switch o.Service {
	case ServiceNone, ServiceSystemd, ServiceLaunchd:
	default:
		return o, fmt.Errorf("unsupported service kind %q", o.Service)
	}
	if o.Service == ServiceSystemd && runtime.GOOS != "linux" {
		return o, errors.New("systemd user service is supported only on Linux")
	}
	if o.Service == ServiceLaunchd && runtime.GOOS != "darwin" {
		return o, errors.New("launchd user service is supported only on macOS")
	}
	return o, nil
}

func BuildPlan(input Options) (Plan, error) {
	o, err := input.Normalize()
	if err != nil {
		return Plan{}, err
	}
	binDir := filepath.Join(o.Prefix, "bin")
	plan := Plan{Options: o, Notes: []string{"installation never enables provider credentials automatically", "service files use the private FutureDiff data root"}}
	for _, name := range DefaultBinaries {
		src := filepath.Join(o.SourceDir, name)
		if st, err := os.Stat(src); err == nil && st.Mode().IsRegular() {
			plan.Actions = append(plan.Actions, FileAction{Source: src, Target: filepath.Join(binDir, name), Mode: 0o755, Kind: "binary"})
		}
	}
	if len(plan.Actions) == 0 {
		return Plan{}, errors.New("no FutureDiff binaries found in source directory")
	}
	switch o.Service {
	case ServiceSystemd:
		plan.Actions = append(plan.Actions, FileAction{Target: filepath.Join(userHome(), ".config", "systemd", "user", "futurediff.service"), Mode: 0o600, Kind: "systemd-user-service"})
	case ServiceLaunchd:
		plan.Actions = append(plan.Actions, FileAction{Target: filepath.Join(userHome(), "Library", "LaunchAgents", "dev.futurediff.daemon.plist"), Mode: 0o600, Kind: "launchd-user-service"})
	}
	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].Target < plan.Actions[j].Target })
	return plan, nil
}

func Apply(plan Plan) error {
	if plan.Options.DryRun {
		return nil
	}
	if err := os.MkdirAll(plan.Options.DataRoot, 0o700); err != nil {
		return err
	}
	for _, action := range plan.Actions {
		if err := os.MkdirAll(filepath.Dir(action.Target), 0o700); err != nil {
			return err
		}
		var data []byte
		var err error
		switch action.Kind {
		case "binary":
			data, err = os.ReadFile(action.Source)
		case "systemd-user-service":
			data = []byte(renderSystemd(plan.Options))
		case "launchd-user-service":
			data = []byte(renderLaunchd(plan.Options))
		default:
			return fmt.Errorf("unknown action kind %q", action.Kind)
		}
		if err != nil {
			return err
		}
		if err := writeAtomic(action.Target, data, action.Mode); err != nil {
			return err
		}
	}
	return nil
}

func EncodePlan(w io.Writer, plan Plan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(plan)
}

func renderSystemd(o Options) string {
	args := daemonArgs(o)
	return fmt.Sprintf(`[Unit]
Description=FutureDiff local transaction daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=2
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%s
UMask=0077

[Install]
WantedBy=default.target
`, systemdEscapeArgs(args), systemdEscape(o.DataRoot))
}

func renderLaunchd(o Options) string {
	args := daemonArgs(o)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n")
	b.WriteString("<key>Label</key><string>dev.futurediff.daemon</string>\n<key>ProgramArguments</key><array>\n")
	for _, arg := range args {
		b.WriteString("<string>" + xmlEscape(arg) + "</string>\n")
	}
	b.WriteString("</array>\n<key>RunAtLoad</key><true/>\n<key>KeepAlive</key><true/>\n")
	b.WriteString("<key>StandardOutPath</key><string>" + xmlEscape(filepath.Join(o.DataRoot, "daemon.stdout.log")) + "</string>\n")
	b.WriteString("<key>StandardErrorPath</key><string>" + xmlEscape(filepath.Join(o.DataRoot, "daemon.stderr.log")) + "</string>\n")
	b.WriteString("<key>Umask</key><integer>63</integer>\n</dict></plist>\n")
	return b.String()
}

func daemonArgs(o Options) []string {
	args := []string{filepath.Join(o.Prefix, "bin", "futurediffd"), "--root", o.DataRoot, "--socket", o.Socket}
	if o.Runtime != "" {
		args = append(args, "--runtime", o.Runtime)
	}
	if o.RuntimeImage != "" {
		args = append(args, "--runtime-image", o.RuntimeImage)
	}
	if o.CredentialConfig != "" {
		args = append(args, "--credential-config", o.CredentialConfig)
	}
	if o.ApprovalKeyring != "" {
		args = append(args, "--approval-keyring", o.ApprovalKeyring)
	}
	if o.ApprovalQuorumPolicy != "" {
		args = append(args, "--approval-quorum-policy", o.ApprovalQuorumPolicy)
	}
	if o.EvidenceKeyring != "" {
		args = append(args, "--evidence-keyring", o.EvidenceKeyring)
	} else if o.EvidenceKey != "" {
		args = append(args, "--evidence-key", o.EvidenceKey)
	}
	if o.SecretScanPolicy != "" {
		args = append(args, "--secret-scan-policy", o.SecretScanPolicy)
	}
	if o.QuotaPolicy != "" {
		args = append(args, "--quota-policy", o.QuotaPolicy)
	}
	if o.RatePolicy != "" {
		args = append(args, "--rate-policy", o.RatePolicy)
	}
	if o.RepositoryPolicy != "" {
		args = append(args, "--repository-policy", o.RepositoryPolicy)
	}
	if o.StoragePolicy != "" {
		args = append(args, "--storage-policy", o.StoragePolicy)
	}
	if o.ConfigSigningKeyring != "" {
		args = append(args, "--config-signing-keyring", o.ConfigSigningKeyring)
	}
	if o.RequireSignedConfigs {
		args = append(args, "--require-signed-configs")
	}
	if o.AllowedPeerUIDs != "" {
		args = append(args, "--allowed-peer-uids", o.AllowedPeerUIDs)
	}
	if o.DisablePeerAuth {
		args = append(args, "--disable-peer-auth")
	}
	if o.RequireSignedApprovals {
		args = append(args, "--require-signed-approvals")
	}
	return args
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func systemdEscape(v string) string { return strings.ReplaceAll(v, " ", "\\x20") }
func systemdEscapeArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = systemdEscape(a)
	}
	return strings.Join(parts, " ")
}
func xmlEscape(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(v)
}
func userHome() string { h, _ := os.UserHomeDir(); return h }
