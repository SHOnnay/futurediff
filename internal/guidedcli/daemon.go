package guidedcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type DaemonManager struct {
	Engine           Engine
	Binary           string
	Socket           string
	Root             string
	CredentialConfig string
	UnsafeNoAuth     bool
}

func (d DaemonManager) resolvedRoot() (string, error) {
	root := strings.TrimSpace(d.Root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("FDIF_HOME"))
	}
	if root == "" {
		root = strings.TrimSpace(os.Getenv("FUTUREDIFF_ROOT"))
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".futurediff")
	}
	return canonicalizeHomePath(root)
}

func (d DaemonManager) Status(ctx context.Context) error {
	_, err := d.Engine.Run(ctx, "health")
	return err
}

func (d DaemonManager) Start(ctx context.Context) error {
	if d.Status(ctx) == nil {
		return nil
	}
	if d.CredentialConfig != "" {
		info, statErr := os.Lstat(d.CredentialConfig)
		if statErr != nil {
			return fmt.Errorf("credential config: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("credential config must be a regular file, not a symlink")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return errors.New("credential config permissions are too broad; use chmod 600")
		}
	}
	root, err := d.resolvedRoot()
	if err != nil {
		return err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return err
	}
	args := []string{"--root", root}
	if d.Socket != "" {
		args = append(args, "--socket", d.Socket)
	}
	if d.CredentialConfig != "" {
		args = append(args, "--credential-config", d.CredentialConfig)
	}
	if d.UnsafeNoAuth {
		args = append(args, "--disable-peer-auth")
	}
	logPath := filepath.Join(root, "futurediffd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(d.Binary, args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if d.Status(ctx) == nil {
			return nil
		}
	}
	return fmt.Errorf("daemon did not become ready; inspect %s", logPath)
}

func (d DaemonManager) Stop() error {
	root, err := d.resolvedRoot()
	if err != nil {
		return err
	}
	pidPath := filepath.Join(root, "futurediff.pid")
	info, err := os.Lstat(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("daemon PID file not found")
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("refusing unsafe daemon PID file")
	}
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || pid <= 1 {
		return errors.New("invalid daemon PID file")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(os.Interrupt); err != nil {
		return process.Kill()
	}
	return nil
}
