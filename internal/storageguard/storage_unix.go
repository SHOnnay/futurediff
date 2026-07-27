//go:build linux || darwin || freebsd || openbsd || netbsd

package storageguard

import "syscall"

func filesystem(path string) (Filesystem, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Filesystem{}, err
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bavail) * int64(st.Bsize)
	pct := float64(0)
	if total > 0 {
		pct = float64(free) * 100 / float64(total)
	}
	return Filesystem{TotalBytes: total, FreeBytes: free, FreePercent: pct}, nil
}
