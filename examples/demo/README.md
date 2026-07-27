# One-command local demonstration

Run:

```bash
./scripts/demo.sh
```

The demonstration creates a temporary Git repository, stages an alternate future, verifies it, binds approval to the exact digest, and publishes `refs/heads/futurediff/<transaction-id>`. The live checkout remains unchanged. No model, API key, container runtime, GitHub account, or Slack workspace is required.
