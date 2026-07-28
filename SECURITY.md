# Security Policy

## Supported scope

Security reports are accepted for the current default branch and the latest published release. Older snapshots are supported only when a maintainer explicitly marks them as supported.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub private vulnerability reporting when enabled, or contact the repository security contact through a private channel listed in the repository profile.

Include the affected version, reproducible steps, impact, logs with secrets removed, and any proposed remediation. Never include production credentials, private keys, access tokens, customer data, or raw evidence that contains them.

## Response targets

- acknowledgement: two business days;
- initial triage: five business days;
- critical remediation target: seven days where feasible;
- coordinated disclosure: after a fix and upgrade guidance are available.

These are operational targets, not contractual guarantees.

## Security boundaries

FutureDiff must fail closed when approval material, transaction state, repository identity, effect receipts, evidence integrity, or credential scope cannot be proven. External effects must use disposable resources during certification. Missing external evidence is reported as blocked and is never converted to a pass.
