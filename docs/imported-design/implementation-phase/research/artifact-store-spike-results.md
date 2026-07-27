# Artifact Store Spike Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This spike proves the first durable local artifact store for evidence, previews, receipts, and exported benchmark assets.

## Implemented

- `futurediff/verifier/evidence/artifactstore/store.go`
- `futurediff/verifier/evidence/artifactstore/store_test.go`

## Proven behavior

- large evidence blobs are stored on the filesystem as content-addressed artifacts;
- artifact refs survive store reopen/recovery;
- artifacts can be read back by ref without relying on a database row payload;
- artifact refs can be exported into a `.futurepack` bundle;
- exported bundle contents match the stored artifact bytes.

## Verification

- `go test ./...` passes, including `verifier/evidence/artifactstore`.

## Why this matters

This gives FutureDiff a real boundary for keeping large evidence out of the control-plane database and makes benchmark/export work much less fake.

## Next useful move

Wire the artifact store into more staging and adapter paths so transaction evidence, previews, and receipts all converge on one durable store.
