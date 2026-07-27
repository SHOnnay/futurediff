//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd

package storageguard

import "errors"

func filesystem(path string) (Filesystem, error) {
	return Filesystem{}, errors.New("filesystem capacity inspection is unsupported on this platform")
}
