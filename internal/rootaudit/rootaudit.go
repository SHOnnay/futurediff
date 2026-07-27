package rootaudit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const Version = "0.1"

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Report struct {
	Version   string    `json:"version"`
	Root      string    `json:"root"`
	CheckedAt time.Time `json:"checked_at"`
	Healthy   bool      `json:"healthy"`
	Checks    []Check   `json:"checks"`
}

func Audit(root string, expectedUID int, now time.Time) Report {
	report := Report{Version: Version, Root: root, CheckedAt: now.UTC(), Healthy: true}
	add := func(id, status, message, path string) {
		report.Checks = append(report.Checks, Check{ID: id, Status: status, Message: message, Path: path})
		if status == "fail" {
			report.Healthy = false
		}
	}
	if root == "" || !filepath.IsAbs(root) {
		add("root.absolute", "fail", "data root must be an absolute path", root)
		return report
	}
	st, err := os.Lstat(root)
	if err != nil {
		add("root.exists", "fail", err.Error(), root)
		return report
	}
	if st.Mode()&os.ModeSymlink != 0 {
		add("root.symlink", "fail", "data root must not be a symlink", root)
		return report
	}
	if !st.IsDir() {
		add("root.directory", "fail", "data root is not a directory", root)
		return report
	}
	add("root.directory", "pass", "data root is a real directory", root)
	if st.Mode().Perm()&0o077 != 0 {
		add("root.permissions", "fail", fmt.Sprintf("data root must be private (0700 or stricter), found %04o", st.Mode().Perm()), root)
	} else {
		add("root.permissions", "pass", fmt.Sprintf("private mode %04o", st.Mode().Perm()), root)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		uid, ok := fileUID(st)
		if !ok {
			add("root.owner", "fail", "could not resolve data-root owner", root)
		} else if expectedUID >= 0 && uid != expectedUID {
			add("root.owner", "fail", fmt.Sprintf("data root belongs to uid %d, expected %d", uid, expectedUID), root)
		} else {
			add("root.owner", "pass", fmt.Sprintf("owned by uid %d", uid), root)
		}
	} else {
		add("root.owner", "skip", "owner validation is unavailable on this platform", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		add("root.entries", "fail", err.Error(), root)
		return report
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			add("entry.stat", "fail", err.Error(), path)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			add("entry.symlink", "fail", "top-level data-root symlink is forbidden", path)
			continue
		}
		if info.Mode().Perm()&0o022 != 0 {
			add("entry.writable", "fail", fmt.Sprintf("group/world writable mode %04o", info.Mode().Perm()), path)
		}
		if isSensitive(entry.Name()) && info.Mode().IsRegular() && info.Mode().Perm()&0o077 != 0 {
			add("entry.sensitive_permissions", "fail", fmt.Sprintf("sensitive file must be 0600 or stricter, found %04o", info.Mode().Perm()), path)
		}
		if info.Mode()&os.ModeDevice != 0 || info.Mode()&os.ModeNamedPipe != 0 {
			add("entry.special", "fail", "device and FIFO entries are forbidden in the data-root top level", path)
		}
	}
	if report.Healthy {
		add("root.summary", "pass", "data-root ownership, permissions and entry types are secure", root)
	}
	return report
}

func isSensitive(name string) bool {
	lower := strings.ToLower(name)
	if lower == "ledger.db" || lower == "maintenance.json" || lower == "daemon.lock" || lower == "futurediff.pid" {
		return true
	}
	return strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".private.json") || strings.Contains(lower, "credential")
}
