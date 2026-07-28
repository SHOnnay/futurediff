# Observability and Alerting

The observability contract requires transaction, effect, reconciliation, and approval telemetry. Logs must include correlation fields and must not contain credential-bearing fields. Alert routes require distinct primary and secondary responders, acknowledgement targets, and escalation timing.

```bash
python3 tools/futurediff_operations.py observability-validate \
  --input config/observability-contract.json \
  --policy config/observability-policy.json

python3 tools/futurediff_operations.py alert-routing-validate \
  --input examples/alert-routing.example.json \
  --policy config/alert-routing-policy.json
```
