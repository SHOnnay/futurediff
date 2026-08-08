# Incident and Release Governance

Tabletop exercises cover credential exposure, provider unknown outcomes, ledger corruption, and runtime escape attempts. Each scenario requires detection, containment, recovery, communications, lessons, and a minimum score.

Release approval records require digest binding, distinct approvers, quorum, required roles, and separation of duties. Self-approval is rejected.

## Resilience drill in governance

The certified corruption/lock/disk-pressure drill (`scripts/certify-corruption-lock-disk-pressure.sh`) is a mandatory gate before any beta/stable promotion:
- Drill evidence is committed to the repository (tracked in MANIFEST.sha256)
- Zero failures required across all 77 checks and 9 scenario blocks
- Evidence classifications distinguish `real_local` (actual filesystem operations) from `deterministic_injection` (ADR-099 Go fault tests)
- Drill reproduces every beta-blocker scenario with operator guidance