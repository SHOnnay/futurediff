# Task 053 — Approval-key rotation and revocation

## Objective

Allow Ed25519 operator approval keys to rotate safely without either immediate lockout or indefinite trust of old keys.

## Implemented

- Overlap rotation: add a new key while keeping older keys enabled.
- Cutover rotation: `--disable-old` disables older keys for the same approver.
- Explicit key enable/disable operation.
- Default refusal to disable an approver's final enabled key.
- Explicit `--allow-no-enabled` emergency override.
- Stable sorted keyring output and atomic `0600` publication.
- `list`, `rotate`, and `set-enabled` subcommands in `futurediff-approval`.

## Example

```bash
futurediff-approval rotate \
  --keyring operator-keyring.json \
  --approver operator@example.com \
  --private operator-private-v2.json

futurediff-approval set-enabled \
  --keyring operator-keyring.json \
  --key-id ed25519-old \
  --enabled=false
```

## Operational limitation

The daemon loads the approval keyring at startup. A keyring update requires a daemon restart or service reload before it changes the live trust set. External key transparency and hardware-backed signing remain production work.

## Validation

- Old and new keys both verify during overlap.
- Disabled keys are rejected.
- Final-key lockout is prevented by default.
- Approval tampering and expiry behavior remain intact.
