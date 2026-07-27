# OpenCode integration

Generate a project or user configuration:

```bash
futurediff-integrate --target opencode \
  --mcp-binary /absolute/path/futurediff-mcp \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output ./opencode.futurediff.json
```

The strict profile denies OpenCode's direct `edit` and `bash` paths and allows only the FutureDiff MCP tool surface. It does not expose approval or commit tools.
