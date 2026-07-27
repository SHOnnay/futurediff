# Configuration snapshot example

Create a digest-only snapshot without copying configuration contents:

```bash
futurediff-config-snapshot build \
  --file credentials=$HOME/.futurediff/credentials.json \
  --file approval-keyring=$HOME/.futurediff/operator-keyring.json \
  --file optional-policy=?$HOME/.futurediff/quorum-policy.json \
  --output $HOME/.futurediff/config.snapshot.json
```

Verify later with `futurediff-config-snapshot verify --manifest ...`.
