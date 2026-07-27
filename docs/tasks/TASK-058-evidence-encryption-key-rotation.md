# Task 058 — Evidence encryption key rotation

## Delivered
- Evidence keyring with one active write key
- Enabled historical decrypt keys
- `futurediff-evidence init-keyring`
- `futurediff-evidence rotate-keyring`
- `futurediff-evidence decrypt-keyring`
- Daemon `--evidence-keyring`
- Backward-compatible single-key mode
- Installer support and keyring schema

## Security boundary
Key files remain `0600`. Keyring metadata contains paths and key IDs but no key values. `--disable-old` intentionally removes historical decryption capability.

## Validation
Evidence encrypted before rotation remained decryptable after a new active key was installed.
