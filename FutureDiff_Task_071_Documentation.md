# Task 071 — Exclusive daemon instance lock

## Goal
Prevent two FutureDiff daemon processes from opening the same local data root as concurrent writers.

## Implemented

- Added an advisory kernel file lock at `ROOT/daemon.lock` by default.
- The daemon acquires the lock before opening SQLite or writing its PID file.
- Lock acquisition uses non-blocking exclusive `flock` on Linux and macOS.
- The lock file is a regular, non-symlink `0600` file.
- Lock metadata records format version, PID, UID, start time, and data-root identity.
- The open file descriptor remains held for the daemon lifetime.
- `futurediff-daemon-lock` reports whether the lock is actively held.
- The PID file remains available for operator signalling, but it is no longer the primary exclusivity mechanism.

## Failure behavior

A second daemon using the same lock path exits before opening the ledger. Stale lock-file bytes do not prevent startup because the kernel lock, not file existence, determines ownership.

## Validation

A live daemon held the lock, a second daemon was rejected, and the inspection command reported the lock as released after graceful shutdown.
