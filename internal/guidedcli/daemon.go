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
	Engine       Engine
	Binary       string
	Socket       string
	Root         string
	UnsafeNoAuth bool
}

func (d DaemonManager) Status(ctx context.Context) error {
	_, err := d.Engine.Run(ctx, "health")
	return err
}

func (d DaemonManager) Start(ctx context.Context) error {
	if d.Status(ctx) == nil {
		return nil
	}
	root := d.Root
	if root == "" {
		if value := os.Getenv("FUTUREDIFF_ROOT"); value != "" {
			root = value
		} else {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, ".futurediff")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(root, "futurediffd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	args := []string{"--root", root}
	if d.Socket != "" {
		args = append(args, "--socket", d.Socket)
	}
	if d.UnsafeNoAuth {
		args = append(args, "--disable-peer-auth")
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
	root := d.Root
	if root == "" {
		if value := os.Getenv("FUTUREDIFF_ROOT"); value != "" {
			root = value
		} else {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, ".futurediff")
		}
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
