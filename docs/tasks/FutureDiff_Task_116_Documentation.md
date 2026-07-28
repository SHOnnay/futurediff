# FutureDiff Task 116 — Canonical source manifest and tamper detection

## Status

Complete.

## Delivered

Recursive SHA-256 manifests record regular-file path, digest, size, aggregate count, byte count, and canonical manifest digest. Symlinks and special files are rejected. Verification reports missing, unexpected, and changed files separately.

## Acceptance evidence

Tests cover successful verification, content mutation, and symlink rejection.
