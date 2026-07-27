# Task 074 — Signed configuration attestations

## Goal
Allow operators to require cryptographic evidence that security-sensitive configuration files are exactly the files approved for deployment.

## Implemented

`futurediff-config-sign` creates and verifies Ed25519 sidecar attestations. Each attestation binds:

- stable configuration kind;
- exact file SHA-256 and byte length;
- operator identity and trusted key ID;
- signed and expiry times;
- random nonce;
- detached Ed25519 signature.

The sidecar convention is `FILE.fdattest.json`. The daemon accepts a separate configuration-signing trust root through `--config-signing-keyring`. With `--require-signed-configs`, every configured credential, approval, evidence, secret-scan, quota, or rate file must have a valid unexpired sidecar before the file is loaded.

The signing keyring itself is the local trust anchor and is not self-signed. Its filesystem permissions remain independently enforced.

`futurediff-config-lint` now recognizes rate policies and configuration-attestation envelopes.

## Validation

A signed rate policy allowed startup. Adding one newline to the policy changed its digest and caused startup to fail before API service began.
