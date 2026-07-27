# Evidence-key rotation

```bash
futurediff-evidence init-keyring \
  --keyring $HOME/.futurediff/evidence-keyring.json \
  --key $HOME/.futurediff/evidence-key-001.json

futurediff-evidence rotate-keyring \
  --keyring $HOME/.futurediff/evidence-keyring.json \
  --new-key $HOME/.futurediff/evidence-key-002.json
```

The new key becomes active for writes. Enabled historical keys remain available for decryption.
