# Real-environment certification

Run only the targets for which the host has dedicated disposable resources.
The command accepts environment-variable **names**, not token values.

Local integration contract:

```bash
futurediff-cert-suite \
  --target local \
  --mcp-binary "$PWD/bin/futurediff-mcp" \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output local-certification.json
```

All readiness targets:

```bash
export FD_GITHUB_TEST_TOKEN='...'
export FD_SLACK_TEST_TOKEN='...'

futurediff-cert-suite \
  --target all \
  --oci-runtime docker \
  --oci-image 'registry.example/futurediff-cert@sha256:<64-hex-digest>' \
  --github-owner example \
  --github-repo futurediff-certification \
  --github-token-env FD_GITHUB_TEST_TOKEN \
  --slack-channel C0123456789 \
  --slack-token-env FD_SLACK_TEST_TOKEN \
  --opencode-binary /absolute/path/to/opencode \
  --hermes-binary /absolute/path/to/hermes \
  --attestation-artifact ./dist/futurediff.tar.gz \
  --attestation-repo example/futurediff \
  --output certification.json
```

A `blocked` check is not a pass. It means the source implementation exists, but
required host resources or explicit live mutation certification are missing.
