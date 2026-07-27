# ADR-055: Ledger audit is read-only

`futurediff-audit` verifies SQLite integrity, event chains, approval bindings, receipt/effect consistency, unresolved states, terminal-state invariants, and effect-dependency cycles. It never repairs data automatically. Repair requires a separate explicit operator workflow because silent correction could erase evidence.
