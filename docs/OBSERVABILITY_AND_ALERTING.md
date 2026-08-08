# Observability and Alerting

The observability contract requires transaction, effect, reconciliation, and approval telemetry. Logs must include correlation fields and must not contain credential-bearing fields. Alert routes require distinct primary and secondary responders, acknowledgement targets, and escalation timing.

```bash
python3 tools/futurediff_operations.py observability-validate \
  --input config/observability-contract.json \
  --policy config/observability-policy.json
```

```bash
python3 tools/futurediff_operations.py alert-routing-validate \
  --input examples/alert-routing.example.json \
  --policy config/alert-routing-policy.json
```

## Resilience telemetry

`fdif doctor --json` and `futurediff-restore --json` emit stable JSON with `reason_code`, `component`, `path_class`, `transaction_id`, `integrity_status`, `lock_status`, `owner_status`, `safe_to_retry`, `automatic_cleanup_allowed`, `backup_available`, `backup_verified`, `recovery_required`, `recommended_action` per the ADR-099 reason codes. The certification drill validates these fields under real_local and deterministic_injection classifications.