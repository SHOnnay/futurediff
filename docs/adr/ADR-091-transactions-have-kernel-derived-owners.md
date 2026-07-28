# ADR-091 — Transactions have kernel-derived owners

New daemon-created transactions are durably owned by the principal derived from Unix peer credentials. Client payloads cannot choose the owner.
