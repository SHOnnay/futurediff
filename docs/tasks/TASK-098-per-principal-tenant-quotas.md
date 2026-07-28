# FutureDiff Task 098 — Per-principal tenant quotas

## Objective

Prevent one local principal or sharing pattern from exhausting the global transaction and access capacity.

## Policy

```json
{
  "version": "0.1",
  "max_open_transactions_per_owner": 8,
  "max_active_grants_per_transaction": 16,
  "max_shared_transactions_per_principal": 32
}
```

Configure the daemon with:

```bash
futurediffd --tenant-quota-policy /absolute/tenant-quota-policy.json
```

## Enforced limits

- open, non-terminal transactions owned by one principal;
- active grants attached to one transaction;
- active shared transactions visible to one recipient principal.

Expired grants do not consume quota. Updating an already-active grant does not consume a new slot. Global Task 069 resource quotas continue to apply independently.

## Inspection

```bash
futurediff-tenant-quota \
  --policy tenant-quota-policy.json \
  --root "$HOME/.futurediff" \
  --principal uid:1000 \
  --transaction <transaction-id>
```

## Validation

With `max_active_grants_per_transaction` set to one, the daemon accepted the first temporary grant, returned HTTP `409` for a second grant while the slot was occupied, and accepted a new grant after the first expired.
