# Task 052 — Encrypted runtime evidence

## Objective

Protect runtime stdout, stderr, and structured OCI evidence at rest without first writing plaintext files.

## Implemented

- AES-256-GCM evidence cipher.
- Versioned `0600` evidence-key file.
- Random nonce per artifact.
- Transaction/execution/artifact identity bound as associated data.
- Atomic encrypted-file publication.
- Daemon flag: `--evidence-key`.
- `futurediff-evidence` command for key generation and offline encrypt/decrypt operations.
- Runtime evidence paths use the `.fde` suffix when encryption is enabled.

## Example

```bash
futurediff-evidence generate-key --output ~/.futurediff/evidence-key.json

futurediffd --root ~/.futurediff \
  --evidence-key ~/.futurediff/evidence-key.json
```

## Security boundary

This encrypts newly produced runtime-execution files. It does not encrypt the SQLite ledger, Git patches, pre-existing evidence, provider metadata, or filenames. Loss of the encryption key makes encrypted evidence unrecoverable. Key escrow and HSM/keychain support remain production work.

## Validation

- Plaintext round-trip succeeds with correct associated data.
- Wrong associated data fails.
- Ciphertext tampering fails authentication.
- Weak key-file permissions are rejected.
- Runtime evidence is written only in encrypted form when configured.
