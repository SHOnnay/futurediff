# Deployment Contract

FutureDiff declares staging and production topology in `config/deployment-contract.json`. The contract records service ownership, version, runtime, durable storage, queue semantics, secret-provider boundary, observability, backup objectives, and replica floors.

Validate it with:

```bash
python3 tools/futurediff_operations.py deployment-validate \
  --input config/deployment-contract.json
```

The validator rejects missing environments, inadequate production replicas, missing durability controls, and embedded secret values.
