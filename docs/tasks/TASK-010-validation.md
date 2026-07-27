# FutureDiff Task 010 Validation Report

**Task:** Durable External-Effect Coordinator and GitHub Draft-PR Adapter  
**Date:** 2026-07-27  
**Result:** Source, race, integration, and Unix-socket API validation passed

## Passed checks

```text
gofmt -l .                         PASS
go vet ./...                       PASS
go test ./...                      PASS
go test -race ./...                PASS
go build ./cmd/futurediff          PASS
go build ./cmd/futurediffd         PASS
GitHub adapter focused tests       PASS
Application coordinator tests      PASS
Unix-socket external-effect test   PASS
```

## Security and transaction behavior validated

- exact GitHub input and branch validation;
- draft-only payload generation;
- separate broker grants for ref reads, status queries, and mutation;
- prepared effect included in transaction material;
- ref staleness clears approval before provider mutation;
- coordinator lease and fencing token required;
- write-ahead effect attempt stored before mutation;
- provider status checked before create;
- normal commit performs one POST;
- ambiguous create is recovered with one POST total;
- definite provider rejection can be re-armed only after status proof;
- prepared-effect abort performs zero provider mutations;
- repository and external-effect receipts are both required for final commit;
- live checkout remains unchanged;
- provider token is absent from durable transaction events.

## Coverage snapshot

| Package | Coverage |
|---|---:|
| EffectSpec | 55.6% |
| GitHub draft-PR adapter | 68.9% |
| Local API | 59.2% |
| Application orchestration | 63.6% |
| Credential broker | 72.3% |
| Domain | 47.4% |
| Ledger | 27.0% |
| OCI runtime | 69.3% |
| Git staging | 63.2% |
| Verification | 51.3% |

## Validation boundary

The provider tests use a deterministic fake HTTPS transport. No real GitHub account or production credential was used. Docker and Podman are unavailable in this environment, so real rootless OCI certification remains pending.

Task 010 also does not push the approved local FutureDiff commit to the remote GitHub head. It creates a draft PR only for an already existing remote branch whose head and base SHAs are recorded and rechecked.
