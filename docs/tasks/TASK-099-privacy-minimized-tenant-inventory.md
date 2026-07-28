# FutureDiff Task 099 — Privacy-minimized tenant inventory

## Objective

Give operators tenant-capacity and sharing visibility without exposing repository paths, request bodies, credentials, or raw principal identities by default.

## Command

```bash
futurediff-tenant-inventory \
  --root "$HOME/.futurediff" \
  --all
```

By default each principal is represented by a SHA-256 digest. Raw identities require an explicit option:

```bash
--show-principals
```

## Report contents

- owned transaction count;
- open and terminal owned counts;
- owned transaction status distribution;
- active read and operate shares received;
- active and expired grants issued;
- report generation time;
- deterministic report digest over the inventory material.

## Excluded material

The report excludes repository paths, Git refs, patch bodies, provider payloads, credentials, idempotency keys and request bodies.

## Validation

The live report covered three known principals and contained no raw `uid:*` identities under the default redacted mode.
