//go:build !linux && !darwin

package rootaudit

import "os"

func fileUID(st os.FileInfo) (int, bool) { return 0, false }
