# Task 057 — Multi-operator approval quorum

## Delivered
- Approval quorum policy and bundle types
- Threshold, allowed-approver, and required-approver enforcement
- Distinct approver, key, and nonce requirements
- `futurediff-approval-quorum assemble|verify`
- Daemon `--approval-quorum-policy`
- API and CLI support for quorum bundles
- Installer support
- Quorum signature reference persisted without private material

## Security boundary
Multiple keys controlled by the same approver count once. A quorum policy automatically disables unsigned approval.

## Validation
Two independent Ed25519 approvers met a two-person quorum. A single envelope was rejected by the service.
