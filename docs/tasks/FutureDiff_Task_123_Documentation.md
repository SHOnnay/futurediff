# FutureDiff Task 123 — Guarded overlay installation

## Status

Complete.

## Delivered

The installer validates package hashes, copies only `MANIFEST.apply`, refuses conflicting files by default, supports dry-run mode, requires `--force` for replacement, and backs up replaced files before installation.

## Acceptance evidence

Installer behavior is exercised during package validation against temporary repositories.
