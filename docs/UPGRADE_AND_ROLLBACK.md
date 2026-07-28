# Upgrade and Rollback

Every production upgrade must declare a version transition, backup step, migration or deployment step, verification step, and explicit rollback path. Destructive actions require a backup declaration.

```bash
python3 tools/futurediff_operations.py upgrade-validate \
  --input examples/upgrade-plan.example.json

python3 tools/futurediff_operations.py rollback-drill \
  --input examples/upgrade-plan.example.json
```

The local rollback drill verifies path completeness. A production-complete claim still requires execution against the canonical merged repository and production-like infrastructure.
