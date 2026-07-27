# Cross-Tool Evidence Export Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first exportable evidence bundle for one full cross-tool preparation flow.

## Implemented

- `futurediff/integrations/mvpflow/export.go`
- `futurediff/integrations/mvpflow/export_test.go`

## Proven behavior

- one prepared cross-tool result can be exported into a `.futurepack` bundle;
- the bundle includes staged repo artifacts, transaction metadata, Postgres preview artifacts, and prepared GitHub/Slack payloads;
- manifest metadata captures transaction state and support-level context;
- the exported bundle can be re-opened and inspected in test.

## Verification

- `go test ./...` passes, including `integrations/mvpflow`.

## Why this matters

This closes the gap between “we prepared multiple effects” and “we can hand someone one durable package that proves what was prepared.”

## Next useful move

Wire cross-tool export into benchmark runs so benchmark output and transaction evidence converge on one shared bundle format.
