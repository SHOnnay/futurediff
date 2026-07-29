package guidedcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Engine interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type CommandError struct {
	Command  string
	ExitCode int
	Stderr   string
}

func (e *CommandError) Error() string {
	message := strings.TrimSpace(e.Stderr)
	if message == "" {
		message = "command failed"
	}
	return fmt.Sprintf("%s: %s", e.Command, message)
}

type ExecEngine struct {
	Binary string
	Socket string
}

func (e ExecEngine) Run(ctx context.Context, args ...string) ([]byte, error) {
	binary := e.Binary
	if binary == "" {
		binary = "futurediff"
	}
	commandArgs := make([]string, 0, len(args)+2)
	if e.Socket != "" {
		commandArgs = append(commandArgs, "--socket", e.Socket)
	}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, binary, commandArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		displayArgs := redactCommandArgs(commandArgs)
		return nil, &CommandError{Command: strings.Join(append([]string{binary}, displayArgs...), " "), ExitCode: code, Stderr: stderr.String()}
	}
	return stdout.Bytes(), nil
}

func redactCommandArgs(args []string) []string {
	redacted := append([]string(nil), args...)
	commandIndex := 0
	if len(redacted) >= 2 && redacted[0] == "--socket" {
		commandIndex = 2
	}
	if commandIndex < len(redacted) {
		command := redacted[commandIndex]
		if (command == "approve" || command == "commit") && commandIndex+2 < len(redacted) {
			redacted[commandIndex+2] = "<transaction-digest>"
		}
	}
	for i := 0; i < len(redacted); i++ {
		lower := strings.ToLower(redacted[i])
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api-key") {
			if strings.Contains(redacted[i], "=") {
				key, _, _ := strings.Cut(redacted[i], "=")
				redacted[i] = key + "=<redacted>"
			} else if i+1 < len(redacted) {
				redacted[i+1] = "<redacted>"
				i++
			}
		}
	}
	return redacted
}

func decodeResponse(raw []byte) (Response, error) {
	var response Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return Response{}, fmt.Errorf("decode FutureDiff response: %w", err)
	}
	return response, nil
}

func decodeApprovalMaterial(raw []byte) (ApprovalMaterial, error) {
	var material ApprovalMaterial
	if err := json.Unmarshal(raw, &material); err != nil {
		return ApprovalMaterial{}, fmt.Errorf("decode approval material: %w", err)
	}
	if material.TransactionDigest == "" {
		return ApprovalMaterial{}, errors.New("FutureDiff returned no transaction_digest")
	}
	return material, nil
}

func findBinary(explicit, envName, name string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), executableName(name))
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return name
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}
