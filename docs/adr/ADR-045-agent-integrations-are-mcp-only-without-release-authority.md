# ADR-045: OpenCode and Hermes integrations are MCP-only without release authority

Status: accepted

FutureDiff generates native local-stdio MCP configuration for OpenCode and Hermes Agent. The generated profiles expose only the nine non-release MCP tools. Approval, commit, credential access, and provider mutation are not present in the MCP surface.

The strict OpenCode profile denies direct edit and shell paths so changes must be performed through an enforced FutureDiff transaction. Hermes receives an explicit include-list and disables MCP resources, prompts, and parallel calls for this server.
