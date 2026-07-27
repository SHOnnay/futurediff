# ADR-023: Isolate SQLite behind a small Go repository boundary

**Status:** Accepted with follow-up

## Context

The execution environment could not download Go modules. The system SQLite C library and headers were available.

## Decision

Task 007 uses a small internal cgo binding to the SQLite C API. All low-level calls are isolated in `internal/ledger/sqlite.go`; domain and orchestration packages do not depend on cgo.

## Caution

A custom database binding carries maintenance and memory-safety risk. It is not intended to grow into a general SQL driver.

## Follow-up

After dependency and supply-chain review, replace the bootstrap adapter with a maintained SQLite driver while preserving the repository tests and migration contract. Until then, builds require cgo and a system SQLite development library.
