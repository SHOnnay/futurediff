package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Config struct {
	Image       string
	Workdir     string
	MountSource string
	Command     []string
	Env         map[string]string
}

type Plan struct {
	Binary string
	Args   []string
}

type ProbeResult struct {
	Available bool
	Binary    string
}

type Runtime struct {
	LookPath func(string) (string, error)
	Runner   func(ctx context.Context, name string, args []string) ([]byte, error)
}

func (r Runtime) Probe() (*ProbeResult, error) {
	binary, err := r.lookPath()("docker")
	if err != nil {
		return &ProbeResult{Available: false}, fmt.Errorf("docker runtime unavailable: %w", err)
	}
	return &ProbeResult{Available: true, Binary: binary}, nil
}

func (r Runtime) BuildPlan(cfg Config) (*Plan, error) {
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, errors.New("image is required")
	}
	if strings.TrimSpace(cfg.Workdir) == "" {
		return nil, errors.New("workdir is required")
	}
	if !filepath.IsAbs(cfg.Workdir) {
		return nil, errors.New("workdir must be absolute")
	}
	if strings.TrimSpace(cfg.MountSource) == "" {
		cfg.MountSource = cfg.Workdir
	}
	if !filepath.IsAbs(cfg.MountSource) {
		return nil, errors.New("mount source must be absolute")
	}
	if len(cfg.Command) == 0 {
		return nil, errors.New("command is required")
	}
	probe, err := r.Probe()
	if err != nil {
		return nil, err
	}
	args := []string{
		"run",
		"--rm",
		"--network", "none",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "256",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--mount", fmt.Sprintf("type=bind,src=%s,dst=/workspace", cfg.MountSource),
		"--workdir", "/workspace",
	}
	for _, item := range sortedEnv(cfg.Env) {
		args = append(args, "--env", item)
	}
	args = append(args, cfg.Image)
	args = append(args, cfg.Command...)
	return &Plan{Binary: probe.Binary, Args: args}, nil
}

func (r Runtime) Run(ctx context.Context, cfg Config) ([]byte, error) {
	plan, err := r.BuildPlan(cfg)
	if err != nil {
		return nil, err
	}
	return r.runner()(ctx, plan.Binary, plan.Args)
}

func (r Runtime) lookPath() func(string) (string, error) {
	if r.LookPath != nil {
		return r.LookPath
	}
	return exec.LookPath
}

func (r Runtime) runner() func(context.Context, string, []string) ([]byte, error) {
	if r.Runner != nil {
		return r.Runner
	}
	return func(ctx context.Context, name string, args []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("run docker command: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return output, nil
	}
}

func sortedEnv(input map[string]string) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, fmt.Sprintf("%s=%s", key, input[key]))
	}
	return result
}
