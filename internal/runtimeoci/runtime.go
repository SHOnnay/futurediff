package runtimeoci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RuntimeKind string

const (
	Docker RuntimeKind = "docker"
	Podman RuntimeKind = "podman"
)

type Purpose string

const (
	Mutation     Purpose = "mutation"
	Verification Purpose = "verification"
)

type TerminationReason string

const (
	Exited       TerminationReason = "exited"
	TimedOut     TerminationReason = "timed_out"
	Cancelled    TerminationReason = "cancelled"
	RuntimeError TerminationReason = "runtime_error"
)

type Backend struct {
	Kind       RuntimeKind `json:"kind"`
	Binary     string      `json:"binary"`
	Version    string      `json:"version"`
	Rootless   bool        `json:"rootless"`
	ServerInfo string      `json:"server_info,omitempty"`
}

type Policy struct {
	Image               string        `json:"image"`
	CPUs                string        `json:"cpus"`
	Memory              string        `json:"memory"`
	PIDs                int           `json:"pids"`
	Network             string        `json:"network"`
	Timeout             time.Duration `json:"timeout"`
	MaxOutputBytes      int64         `json:"max_output_bytes"`
	MaxWorkspaceBytes   int64         `json:"max_workspace_bytes"`
	MaxWorkspaceFiles   int           `json:"max_workspace_files"`
	TmpfsSize           string        `json:"tmpfs_size"`
	RequireRootless     bool          `json:"require_rootless"`
	ReadOnlyRoot        bool          `json:"read_only_root"`
	DropAllCapabilities bool          `json:"drop_all_capabilities"`
	NoNewPrivileges     bool          `json:"no_new_privileges"`
}

func DefaultPolicy(image string) Policy {
	return Policy{Image: image, CPUs: "2.0", Memory: "2g", PIDs: 256, Network: "none", Timeout: 10 * time.Minute, MaxOutputBytes: 1 << 20, MaxWorkspaceBytes: 2 << 30, MaxWorkspaceFiles: 100000, TmpfsSize: "256m", RequireRootless: true, ReadOnlyRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}
}

func (p Policy) Validate() error {
	if !digestImage.MatchString(p.Image) {
		return errors.New("image must be pinned by sha256 digest")
	}
	if p.Network != "none" {
		return errors.New("enforced mode requires network=none")
	}
	if !p.RequireRootless || !p.ReadOnlyRoot || !p.DropAllCapabilities || !p.NoNewPrivileges {
		return errors.New("enforced mode requires rootless runtime, read-only root, dropped capabilities, and no-new-privileges")
	}
	if p.PIDs <= 0 || p.Timeout <= 0 || p.MaxOutputBytes <= 0 || p.MaxWorkspaceBytes <= 0 || p.MaxWorkspaceFiles <= 0 {
		return errors.New("resource limits must be positive")
	}
	if p.CPUs == "" || p.Memory == "" || p.TmpfsSize == "" {
		return errors.New("cpu, memory, and tmpfs limits are required")
	}
	return nil
}

type Plan struct {
	Binary string   `json:"binary"`
	Args   []string `json:"args"`
}

type Request struct {
	TransactionID string            `json:"transaction_id"`
	ExecutionID   string            `json:"execution_id"`
	Workspace     string            `json:"workspace"`
	Command       []string          `json:"command"`
	Environment   map[string]string `json:"environment,omitempty"`
	Purpose       Purpose           `json:"purpose"`
	SyncWorkspace bool              `json:"sync_workspace"`
}

type Evidence struct {
	TransactionID         string            `json:"transaction_id"`
	ExecutionID           string            `json:"execution_id"`
	Runtime               Backend           `json:"runtime"`
	Image                 string            `json:"image"`
	ImageDigest           string            `json:"image_digest"`
	CommandDigest         string            `json:"command_digest"`
	EnvironmentDigest     string            `json:"environment_digest"`
	PolicyDigest          string            `json:"policy_digest"`
	StartedAt             time.Time         `json:"started_at"`
	FinishedAt            time.Time         `json:"finished_at"`
	Duration              time.Duration     `json:"duration"`
	ExitCode              int               `json:"exit_code"`
	TerminationReason     TerminationReason `json:"termination_reason"`
	StdoutBytes           int64             `json:"stdout_bytes"`
	StderrBytes           int64             `json:"stderr_bytes"`
	StdoutTruncated       bool              `json:"stdout_truncated"`
	StderrTruncated       bool              `json:"stderr_truncated"`
	WorkspaceSynchronized bool              `json:"workspace_synchronized"`
}

type Result struct {
	Stdout   []byte   `json:"stdout,omitempty"`
	Stderr   []byte   `json:"stderr,omitempty"`
	ExitCode int      `json:"exit_code"`
	Evidence Evidence `json:"evidence"`
}

func (r Result) CombinedOutput() []byte {
	out := make([]byte, 0, len(r.Stdout)+len(r.Stderr))
	out = append(out, r.Stdout...)
	out = append(out, r.Stderr...)
	return out
}

type Runner struct {
	Kind          RuntimeKind
	Binary        string
	Policy        Policy
	ScratchRoot   string
	ProbeIdentity func(context.Context, RuntimeKind, string) (Backend, error)
}

var digestImage = regexp.MustCompile(`^[^\s@]+@sha256:[a-fA-F0-9]{64}$`)

func Probe(binary string) (Backend, error) { return ProbeContext(context.Background(), Docker, binary) }

func ProbeContext(ctx context.Context, kind RuntimeKind, binary string) (Backend, error) {
	if binary == "" {
		binary = string(kind)
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return Backend{}, err
	}
	backend := Backend{Kind: kind, Binary: path}
	switch kind {
	case Docker:
		version, err := commandOutput(ctx, path, "version", "--format", "{{.Server.Version}}")
		if err != nil {
			return Backend{}, err
		}
		info, err := commandOutput(ctx, path, "info", "--format", "{{json .SecurityOptions}}")
		if err != nil {
			return Backend{}, err
		}
		backend.Version = strings.TrimSpace(version)
		backend.ServerInfo = strings.TrimSpace(info)
		backend.Rootless = strings.Contains(strings.ToLower(info), "rootless")
	case Podman:
		version, err := commandOutput(ctx, path, "version", "--format", "{{.Server.Version}}")
		if err != nil {
			version, err = commandOutput(ctx, path, "version", "--format", "{{.Version}}")
			if err != nil {
				return Backend{}, err
			}
		}
		info, err := commandOutput(ctx, path, "info", "--format", "json")
		if err != nil {
			return Backend{}, err
		}
		var parsed struct {
			Host struct {
				Security struct {
					Rootless bool `json:"rootless"`
				} `json:"security"`
			} `json:"host"`
		}
		if err := json.Unmarshal([]byte(info), &parsed); err != nil {
			return Backend{}, err
		}
		backend.Version = strings.TrimSpace(version)
		backend.ServerInfo = strings.TrimSpace(info)
		backend.Rootless = parsed.Host.Security.Rootless
	default:
		return Backend{}, fmt.Errorf("unsupported runtime %q", kind)
	}
	if backend.Version == "" {
		return Backend{}, errors.New("runtime returned empty version")
	}
	return backend, nil
}

func ensureImageAvailable(ctx context.Context, backend Backend, image string) error {
	if _, err := commandOutput(ctx, backend.Binary, "image", "inspect", image); err == nil {
		return nil
	}
	if _, err := commandOutput(ctx, backend.Binary, "pull", image); err != nil {
		return fmt.Errorf("ensure OCI image available: %w", err)
	}
	if _, err := commandOutput(ctx, backend.Binary, "image", "inspect", image); err != nil {
		return fmt.Errorf("inspect OCI image after pull: %w", err)
	}
	return nil
}

func BuildPlan(backend Backend, workspace string, command []string, policy Policy) (Plan, error) {
	if err := policy.Validate(); err != nil {
		return Plan{}, err
	}
	if backend.Binary == "" {
		return Plan{}, errors.New("runtime backend required")
	}
	if policy.RequireRootless && !backend.Rootless {
		return Plan{}, errors.New("runtime is not rootless")
	}
	if len(command) == 0 {
		return Plan{}, errors.New("command required")
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return Plan{}, err
	}
	if strings.ContainsAny(workspace, ",\n\r") {
		return Plan{}, errors.New("workspace path is not safe for OCI mount syntax")
	}
	args := []string{"run", "--rm", "--init", "--pull=never", "--read-only", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", strconv.Itoa(policy.PIDs), "--memory", policy.Memory, "--cpus", policy.CPUs, "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=" + policy.TmpfsSize, "--mount", "type=bind,src=" + workspace + ",dst=/workspace,rw", "--workdir", "/workspace"}
	if backend.Kind == Podman {
		args = append(args, "--userns", "keep-id", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	} else {
		// Rootless Docker maps container root to the unprivileged daemon user.
		args = append(args, "--user", "0:0")
	}
	args = append(args, "--env", "HOME=/tmp/futurediff-home", "--env", "TMPDIR=/tmp", policy.Image)
	args = append(args, command...)
	return Plan{Binary: backend.Binary, Args: args}, nil
}

func (r Runner) Ready(ctx context.Context) (Backend, error) {
	probe := r.ProbeIdentity
	if probe == nil {
		probe = ProbeContext
	}
	backend, err := probe(ctx, r.Kind, r.Binary)
	if err != nil {
		return Backend{}, err
	}
	if err := r.Policy.Validate(); err != nil {
		return Backend{}, err
	}
	if r.Policy.RequireRootless && !backend.Rootless {
		return Backend{}, errors.New("runtime is not rootless")
	}
	return backend, nil
}

func (r Runner) Execute(ctx context.Context, request Request) (Result, error) {
	if request.TransactionID == "" || request.ExecutionID == "" || request.Workspace == "" || len(request.Command) == 0 {
		return Result{}, errors.New("transaction_id, execution_id, workspace, and command are required")
	}
	if err := validateEnvironment(request.Environment); err != nil {
		return Result{}, err
	}
	backend, err := r.Ready(ctx)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.Policy.Timeout)
	defer cancel()
	if err := ensureImageAvailable(ctx, backend, r.Policy.Image); err != nil {
		return Result{}, err
	}
	root := r.ScratchRoot
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Result{}, fmt.Errorf("create OCI scratch root: %w", err)
	}
	execRoot, err := os.MkdirTemp(root, "futurediff-oci-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(execRoot)
	workspace := filepath.Join(execRoot, "workspace")
	if _, err := copySanitized(request.Workspace, workspace, r.Policy); err != nil {
		return Result{}, err
	}
	plan, err := BuildPlan(backend, workspace, request.Command, r.Policy)
	if err != nil {
		return Result{}, err
	}
	// Rebuild environment placement deterministically when entries exist.
	if len(request.Environment) > 0 {
		base, _ := BuildPlan(backend, workspace, request.Command, r.Policy)
		imageIndex := len(base.Args) - len(request.Command) - 1
		args := append([]string(nil), base.Args[:imageIndex]...)
		keys := make([]string, 0, len(request.Environment))
		for key := range request.Environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "--env", key+"="+request.Environment[key])
		}
		args = append(args, base.Args[imageIndex:]...)
		plan.Args = args
	}
	policyDigest, err := digest(map[string]any{"policy": r.Policy, "runtime": backend})
	if err != nil {
		return Result{}, err
	}
	stdout, stderr := newBounded(r.Policy.MaxOutputBytes), newBounded(r.Policy.MaxOutputBytes)
	started := time.Now().UTC()
	cmd := exec.CommandContext(ctx, plan.Binary, plan.Args...)
	cmd.Env = minimalRuntimeEnv()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	finished := time.Now().UTC()
	exitCode, reason := 0, Exited
	if runErr != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			reason = TimedOut
		case errors.Is(ctx.Err(), context.Canceled):
			reason = Cancelled
		case exitCode == 125 || exitCode == 126 || exitCode == 127:
			reason = RuntimeError
		case errors.As(runErr, &exitErr):
			reason = Exited
		default:
			reason = RuntimeError
		}
	}
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode, Evidence: Evidence{TransactionID: request.TransactionID, ExecutionID: request.ExecutionID, Runtime: backend, Image: r.Policy.Image, ImageDigest: strings.SplitN(r.Policy.Image, "@", 2)[1], CommandDigest: digestStrings(request.Command), EnvironmentDigest: digestEnvironment(request.Environment), PolicyDigest: policyDigest, StartedAt: started, FinishedAt: finished, Duration: finished.Sub(started), ExitCode: exitCode, TerminationReason: reason, StdoutBytes: stdout.Total(), StderrBytes: stderr.Total(), StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated()}}
	if runErr != nil {
		return result, fmt.Errorf("OCI command failed: reason=%s exit=%d", reason, exitCode)
	}
	if request.SyncWorkspace {
		if request.Purpose != Mutation {
			return result, errors.New("only mutation execution may synchronize workspace")
		}
		if err := syncSanitized(workspace, request.Workspace, r.Policy); err != nil {
			return result, err
		}
		result.Evidence.WorkspaceSynchronized = true
	}
	return result, nil
}

func copySanitized(source, destination string, policy Policy) (int64, error) {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return 0, err
	}
	var total int64
	files := 0
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		first := strings.Split(rel, string(filepath.Separator))[0]
		if first == ".git" || first == ".futurediff" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink rejected: %s", rel)
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported workspace entry: %s", rel)
		}
		files++
		total += info.Size()
		if files > policy.MaxWorkspaceFiles || total > policy.MaxWorkspaceBytes {
			return errors.New("workspace limit exceeded")
		}
		return copyFile(path, target, info.Mode().Perm())
	})
	return total, err
}

func syncSanitized(source, destination string, policy Policy) error {
	sourceEntries, err := inventory(source)
	if err != nil {
		return err
	}
	destinationEntries, err := inventory(destination)
	if err != nil {
		return err
	}
	for rel := range destinationEntries {
		first := strings.Split(rel, string(filepath.Separator))[0]
		if first == ".git" || first == ".futurediff" {
			continue
		}
		if _, ok := sourceEntries[rel]; !ok {
			if err := os.RemoveAll(filepath.Join(destination, rel)); err != nil {
				return err
			}
		}
	}
	_, err = copySanitized(source, destination, policy)
	return err
}

func inventory(root string) (map[string]fs.FileMode, error) {
	out := map[string]fs.FileMode{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		first := strings.Split(rel, string(filepath.Separator))[0]
		if first == ".git" || first == ".futurediff" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink rejected: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		out[rel] = info.Mode()
		return nil
	})
	return out, err
}

func copyFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := destination + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode&0o777)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, destination)
}

func validateEnvironment(environment map[string]string) error {
	markers := []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL", "AUTHORIZATION", "COOKIE", "AWS_", "GITHUB_", "SLACK_"}
	for key, value := range environment {
		upper := strings.ToUpper(key)
		for _, marker := range markers {
			if strings.Contains(upper, marker) {
				return fmt.Errorf("sensitive environment %q rejected", key)
			}
		}
		if strings.ContainsAny(key+value, "\x00") || strings.Contains(key, "=") {
			return errors.New("invalid environment entry")
		}
	}
	return nil
}

func commandOutput(ctx context.Context, binary string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = minimalRuntimeEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("runtime probe failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
func digestStrings(values []string) string {
	h := sha256.New()
	for _, v := range values {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func digestEnvironment(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(values[k]))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func minimalRuntimeEnv() []string {
	keys := []string{"PATH", "HOME", "XDG_RUNTIME_DIR", "DOCKER_HOST", "CONTAINER_HOST"}
	var out []string
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

type bounded struct {
	buf          bytes.Buffer
	limit, total int64
	truncated    bool
}

func newBounded(limit int64) *bounded { return &bounded{limit: limit} }
func (b *bounded) Write(p []byte) (int, error) {
	b.total += int64(len(p))
	remain := b.limit - int64(b.buf.Len())
	if remain > 0 {
		n := int64(len(p))
		if n > remain {
			n = remain
		}
		_, _ = b.buf.Write(p[:n])
	}
	if b.total > b.limit {
		b.truncated = true
	}
	return len(p), nil
}
func (b *bounded) Bytes() []byte   { return append([]byte(nil), b.buf.Bytes()...) }
func (b *bounded) Total() int64    { return b.total }
func (b *bounded) Truncated() bool { return b.truncated }
