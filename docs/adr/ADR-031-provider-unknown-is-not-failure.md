# ADR-031: An unknown provider outcome is not failure

**Status:** Accepted

## Decision

Any transport, response-read, response-decode, or incomplete-receipt condition occurring after possible provider dispatch is classified as `UNKNOWN`.

FutureDiff must query adapter `status` before retrying. A blind duplicate mutation is prohibited.

## Consequences

- recovery may require manual intervention when provider state cannot be proven;
- availability is traded for correctness;
- provider adapters must implement status evidence for strong commit semantics.
