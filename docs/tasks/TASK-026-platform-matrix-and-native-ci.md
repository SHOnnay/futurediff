# Task 026 — Platform matrix and native CI

## Objective

Replace vague cross-platform claims with an executable support decision.

## Delivered

- `futurediff-platform`
- Machine-readable platform matrix
- Linux amd64 marked supported
- Linux arm64 and macOS amd64/arm64 marked experimental
- Windows amd64 explicitly marked unsupported
- Native Ubuntu, Intel macOS, and Apple Silicon macOS CI workflow
- Windows workflow check enforcing the explicit unsupported decision

## Reasoning

FutureDiff currently depends on Unix-domain sockets, Unix file permissions, user-level daemon semantics, and a cgo SQLite bridge. Claiming Windows support before named pipes and Windows credential isolation exist would be unsafe.

## Executed result

The current Linux amd64 environment was reported as supported. macOS targets remain CI-dependent and OCI enforcement remains host-specific.
