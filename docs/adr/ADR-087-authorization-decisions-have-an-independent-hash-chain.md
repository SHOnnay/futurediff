# ADR-087 — Authorization decisions have an independent hash chain

RBAC and capability decisions are recorded separately from mutation execution evidence so denied attempts remain visible even when no handler executes.
