# ADR-068: Upgrades are rehearsed on an offline clone

Status: accepted

Upgrade rehearsal requires the daemon socket to be absent. The source SQLite ledger is copied using the SQLite online-backup interface, current migrations are applied only to the clone, and semantic audit runs against the upgraded clone. Transaction and unresolved-state counts must remain stable, migration count may only increase, and the source ledger SHA-256 must remain unchanged.
