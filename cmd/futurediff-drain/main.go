package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	pidFile := flag.String("pid-file", filepath.Join(home, ".futurediff", "futurediff.pid"), "daemon pid file")
	timeout := flag.Duration("timeout", 30*time.Second, "maximum time to wait for exit")
	confirm := flag.String("confirm", "", "must equal DRAIN_FUTUREDIFF_DAEMON")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		printJSON(buildinfo.Current())
		return
	}
	if *confirm != "DRAIN_FUTUREDIFF_DAEMON" {
		fail(fmt.Errorf("explicit --confirm DRAIN_FUTUREDIFF_DAEMON is required"))
	}
	b, err := os.ReadFile(*pidFile)
	if err != nil {
		fail(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		fail(fmt.Errorf("invalid daemon pid file"))
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fail(err)
	}
	deadline := time.Now().Add(*timeout)
	for {
		exited, err := processExited(pid)
		if err != nil {
			fail(err)
		}
		if exited {
			printJSON(map[string]any{"pid": pid, "drained": true, "pid_file": *pidFile})
			return
		}
		if time.Now().After(deadline) {
			fail(fmt.Errorf("daemon did not exit before timeout"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func processExited(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == syscall.ESRCH {
		return true, nil
	}
	if err != nil && err != syscall.EPERM {
		return false, err
	}
	if b, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); readErr == nil {
		parts := strings.Fields(string(b))
		if len(parts) > 2 && parts[2] == "Z" {
			return true, nil
		}
	}
	return false, nil
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(err error)  { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
