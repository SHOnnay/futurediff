# Hermes Agent integration

Generate a filtered MCP configuration snippet:

```bash
futurediff-integrate --target hermes \
  --mcp-binary /absolute/path/futurediff-mcp \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output ./hermes-futurediff.yaml
```

Merge the `mcp_servers.futurediff` entry into `~/.hermes/config.yaml`, then reload MCP. Only FutureDiff's non-release tools are included; resources and prompts are disabled.
