# Security Policy

## Supported scope

Security reports are accepted for the current default branch and the latest
published alpha release. The public alpha supports same-machine local
operation on Linux and macOS. Windows, network-reachable, hosted, and
multi-tenant operation are outside the supported security boundary.

FutureDiff has not completed an independent external security review. Alpha
releases must not be described as externally audited or production-complete.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub private
vulnerability reporting when enabled, or contact the repository security
contact through a private channel listed in the repository profile.

Include the affected version, reproducible steps, impact, and logs with secrets
removed. Never include production credentials, private keys, access tokens,
private source code, customer data, or raw evidence containing them.

## Response targets

- acknowledgement: two business days;
- initial triage: five business days;
- critical remediation target: seven days where feasible;
- coordinated disclosure: after a fix and upgrade guidance are available.

These are operational targets, not contractual guarantees.

## Security boundaries

FutureDiff must fail closed when peer identity, approval material, transaction
state, repository identity, effect receipts, evidence integrity, or credential
scope cannot be proven.

The daemon and Unix-domain socket are local-only. Do not expose them directly
over a network.

External effects must use disposable resources during certification. Missing
external evidence is reported as blocked and is never converted to a pass.

## Local path handling

FutureDiff resolves one effective local home before launching the daemon or
storing the current-change selection. The guided CLI, daemon launcher, socket,
runtime directory, and workspace placement must use that same canonical home.

Path rules:

- known operating-system aliases needed for normal platform behavior are
  canonicalized before use;
- macOS paths below `/tmp` are used below `/private/tmp`;
- arbitrary user-controlled symlinked parent components are rejected;
- a configured FutureDiff home that is itself a symlink is rejected;
- the current-selection file may not be a symlink and remains mode `0600` on
  Unix;
- the daemon root must be a real private directory and remains mode `0700` on
  Unix;
- `fdif config --explain` displays canonical effective paths and their sources.

## Local subprocess handling

Guided-CLI Git helper commands must not inherit ambient `GIT_DIR`, `GIT_WORK_TREE`, pager, askpass, fsmonitor, or global/system Git configuration. They run with a minimized environment, disabled hooks, disabled fsmonitor, disabled external diff, and disabled interactive credential prompting.

This keeps repository detection, review, demo setup, and guided publication checks deterministic even when the caller shell or machine has hostile or surprising Git configuration.

Canonicalization does not make cooperative mode an operating-system sandbox
and cannot remove every same-user time-of-check/time-of-use race. Do not point
FutureDiff at directories controlled by another user.
