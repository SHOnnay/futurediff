# FutureDiff Merge Report

## Scope

This repository combines the canonical Task 010 Go implementation with the user-provided `futurediff-design` branch.

The merge is deliberately selective:

- the canonical daemon, SQLite ledger, Git staging, OCI boundary, credential broker, approval digest, recovery, and GitHub external-effect coordinator remain authoritative;
- compatible schemas and research documentation from the design branch are imported into `specs/imported-v0.1/` and `docs/imported-design/`;
- the design branch's artifact/evidence export idea is hardened and integrated as `internal/futurepack/`;
- the complete cleaned original branch is preserved under `research/original-design-branch/` as a nested Go module;
- parallel spike coordinators and host-execution code are not wired into the trusted path because doing so would weaken the canonical safety model.

## Was the branch previously merged?

No. Before this package, the branch was inspected and used as research input, but it was not fully merged into Task 010.

## Does the uploaded branch contain implementation?

Yes. It contains approximately 6,300 lines of Go across 48 files, including adapters, PostgreSQL preview experiments, benchmark smokes, a cross-tool flow, artifact export, and coordinator spikes.

It is not "no implementation." It is better described as an architecture package with implementation spikes rather than an integrated product.

## Relative strengths

### Uploaded design branch is stronger in

- breadth of research documentation;
- machine-readable draft specifications;
- Slack and PostgreSQL experiments;
- benchmark scenario ideas;
- cross-tool demonstration breadth;
- futurepack/evidence-export concept.

### Canonical implementation is stronger in

- runnable local daemon and CLI;
- durable SQLite state;
- exact Git tree publication without touching the live checkout;
- digest-bound approval;
- transaction/effect state machines;
- unknown-outcome recovery;
- rootless OCI execution design;
- credential isolation and exact operation scopes;
- durable GitHub effect attempts and receipts;
- race-tested end-to-end lifecycle.

## Why parallel source was not overlaid

The design branch has a separate module identity and parallel coordinator/state model. Some spike paths execute commands on the host or apply patches toward the source repository. Overlaying that code would create duplicated authorities and regress safety properties.

The preserved nested module remains available for future controlled ports. Each port should pass the canonical conformance tests before entering the trusted path.
