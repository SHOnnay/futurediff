# FutureDiff Task 121 — Reproducible source-release builder

## Status

Complete.

## Delivered

The release builder uses normalized ZIP timestamps, permissions, ordering, compression, and an embedded source manifest. Equivalent inputs produce byte-identical archives.

## Acceptance evidence

Tests build the same release twice and compare SHA-256 digests.
