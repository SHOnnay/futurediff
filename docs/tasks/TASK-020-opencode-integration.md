# Task 020 — OpenCode Integration

## Delivered

`futurediff-integrate --target opencode` generates an official local MCP entry with the FutureDiff stdio bridge and a strict permission profile.

The strict profile:

- enables the `futurediff` local MCP server;
- allows `futurediff_*` tools;
- denies direct `edit` and `bash` actions;
- leaves all other capabilities at `ask`;
- embeds no provider credential.

## Limitation

The configuration generator is tested, but a full session with the OpenCode executable could not be certified in this environment. OpenCode version-specific end-to-end certification remains necessary.

## Upstream references

- https://opencode.ai/docs/mcp-servers/
- https://opencode.ai/docs/permissions/
