//go:build linux || darwin

package rootaudit

import (
	"os"
	"syscall"
)

func fileUID(st os.FileInfo) (int, bool) {
	raw, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(raw.Uid), true
}
