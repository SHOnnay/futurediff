//go:build !darwin && !linux

package operatoraudit

func withFileLock(_ string, fn func() error) error { return fn() }
