# What remains before authoritative production completion

The clean CLI terminal interface and all locally implementable product and assurance controls are complete. The following items require real external systems or independent humans and cannot be manufactured by the repository itself.

## Required next work

1. Apply the latest overlay to the canonical FutureDiff Go repository and commit the merged result.
2. Run the complete Go build, test, race, migration, installer, upgrade, and rollback suites on the merged commit.
3. Bind the Slack provider evidence to that exact commit SHA and release archive SHA-256.
4. Run a real GitHub write-and-recovery certification in a disposable repository.
5. Run rootless Docker and rootless Podman certification on real hosts.
6. Run native hosted CI on Linux, macOS, and the supported Windows target.
7. Generate and verify hosted artifact attestations, provenance, SBOMs, and signed release artifacts.
8. Run OpenCode and Hermes integration certification against the fixed release candidate.
9. Commission an independent security review and resolve disallowed findings.
10. Run measured load, stress, soak, failure-injection, backup, restore, RTO, and RPO exercises.
11. Deploy the same release candidate in a production-like environment and collect smoke, monitoring, alert, upgrade, and rollback evidence.
12. Obtain distinct security, operations, and release-owner sign-off.
13. Run `scripts/production-closure.sh` using those authoritative artifacts and require `production_complete=true`.

## Current evidence rule

A locally generated example, placeholder, skipped check, stale report, synthetic result, or self-asserted reviewer record must remain blocked. Provider evidence must identify the exact FutureDiff commit and release digest it certifies.
