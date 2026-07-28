# Data Governance

Data classes declare retention, deletion, storage, credential handling, and legal-basis requirements. Provider credentials are restricted to the credential broker and have zero durable retention. Deletion verification is mandatory, and backup retention has an explicit maximum.

```bash
python3 tools/futurediff_operations.py data-governance-validate \
  --input config/data-governance-policy.json
```
