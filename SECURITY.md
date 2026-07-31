# Security Policy

## Supported scope

Security reports are accepted for the current default branch and the latest published alpha release. The public alpha supports same-machine local operation on Linux and macOS. Windows, network-reachable, hosted, and multi-tenant operation are outside the supported security boundary.

FutureDiff has not yet completed an independent external security review. Alpha releases must not be described as externally audited or production-complete.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub private vulnerability reporting when enabled, or contact the repository security contact through a private channel listed in the repository profile.

Include the affected version, reproducible steps, impact, and logs with secrets removed. Never include production credentials, private keys, access tokens, private source code, customer data, or raw evidence containing them.

## Response targets

- acknowledgement: two business days;
- initial triage: five business days;
- critical remediation target: seven days where feasible;
- coordinated disclosure: after a fix and upgrade guidance are available.

These are operational targets, not contractual guarantees.

## Security boundaries

FutureDiff must fail closed when peer identity, approval material, transaction state, repository identity, effect receipts, evidence integrity, or credential scope cannot be proven.

The daemon and Unix-domain socket are local-only. Do not expose them directly over a network.

External effects must use disposable resources during certification. Missing external evidence is reported as blocked and is never converted to a pass.
