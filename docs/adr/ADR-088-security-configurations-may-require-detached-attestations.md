# ADR-088 — Security configurations may require detached attestations

Sensitive configuration files may be bound to an operator-approved digest using an expiring Ed25519 sidecar. The configuration-signing keyring is a separate local trust root. FutureDiff verifies attestations before parsing or applying configured security policy.
