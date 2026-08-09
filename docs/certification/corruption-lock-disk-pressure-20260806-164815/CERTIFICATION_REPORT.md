# Corruption / Lock / Disk-Pressure Certification Report

- Generated (UTC): `20260806-164815`
- Host: `Darwin-arm64`
- Drill: `scripts/certify-corruption-lock-disk-pressure.sh` (real binaries, isolated homes)
- Result: **PASSED**
- Classifications: `real_local` (real binaries + local filesystem), `deterministic_injection` (project Go fault-injection tests, ADR-099 seams; nothing outside tests constructs an injector), `prior_real_github` (prior real-GitHub write-recovery certification, referenced below; this drill never contacts a provider).

## Scenario matrix

| Scenario | Evidence | Classification | Injected failure | Expected | Observed source |
|---|---|---|---|---|---|
| A | `a1-live-refusal.json` (present) | real_local | none (live daemon holds the lock) | cleanup-lock refuses; lock and socket preserved | .action=refused .reason_code=lock_owner_alive; lock file still present |
| A | `a2-stale-doctor.json` (present) | real_local | stale lock (dead pid) + stale socket written over the home | doctor surfaces the stale lock candidate; cleanup removes lock and socket; audit event recorded | .Detail contains stale_lock_candidate; .action=cleaned; operator-events.jsonl contains lock_cleanup |
| A | `a3-repeat.json` (present) | real_local | none (second cleanup-lock run) | repeated cleanup is a no-op | .action=none |
| B | `b1-ambiguous-refused.json` (present) | real_local | valid-format lock held by a live process whose daemon is unreachable | cleanup refuses; nothing removed | .action=refused .reason_code=lock_owner_ambiguous; lock present; holder process untouched |
| C | `c0-corrupt-header.json` (present) | real_local | bytes 100-139 of the SQLite header flipped | doctor flags ledger_integrity as fail | .checks[].ID=ledger_integrity .Status=fail |
| C | `c2-require-integrity.err` (present) | real_local | corrupt header ledger; futurediffd --require-integrity | startup refuses with reason_code=ledger_corrupt | stderr contains 'integrity gate refused startup' and 'reason_code=ledger_corrupt' |
| C | `c4-unchanged-after-diagnosis.json` (present) | real_local | doctor + require-integrity run over the corrupt ledger | diagnosis never rewrites the corrupt files | ledger.db sha identical before/after diagnosis |
| C | `c3-doctor-truncated-ledger.json` (present) | real_local | ledger truncated to half size | doctor flags ledger_integrity as fail | .checks[].ID=ledger_integrity .Status=fail |
| C | `c5-restore-over-corrupt-refused.json` (present) | real_local | restore attempted over the corrupt live ledger | restore refuses (fail-closed; corrupt original never overwritten) | exit non-zero |
| D | `d0-backup.json` (present) | real_local | none (authoritative same-home backup) | backup recorded in the home catalog with sha256 | .backup.sha256 present |
| D/J | `d1-restore.json` (present) | real_local | restore over the live ledger (backup older than live; -allow-stale-backup) | apply succeeds; pre-restore live ledger preserved byte-for-byte in a quarantine; comparison classifies all states | .applied=true .preserved_original.quarantine_dir present; .effect_reconciliation.* |
| D | `d1-quarantine-check.json` (present) | real_local | byte-for-byte comparison of quarantine vs pre-restore live files | every pre-restore file matches; no file missing | .matches.* == true; .missing empty; .manifest_present true |
| D/J | `d1-no-dispatch.json` (present) | real_local | count of attempt/receipt rows in the restored ledger | restore comparison never dispatches to providers | .attempt_count=4 .receipt_count=1 (exactly the seeded rows) |
| D | `d2-repeat.json` (present) | real_local | immediate second restore of the same backup | already-restored path is stable and still evaluates reconciliation | .already_restored=true; .effect_reconciliation.reconciliation_required=true; newer_than_backup_count=0 (N/A: no pre-restore ledger exists on the repeat path); 6 classified effects |
| D | `d3-foreign-refused.json` (present) | real_local | byte-different copy at a path not recorded in the home catalog; the operator digest matches the on-disk bytes | restore refuses with the authoritative-catalog error (only the catalog can prove provenance) | stderr contains 'not recorded in the authoritative backup catalog' |
| D | `d4-digest-refused.json` (present) | real_local | expected-sha256 does not match the on-disk backup | restore refuses with the digest error | stderr contains 'backup SHA-256 does not match expected digest' |
| E | `e1-tamper.json` (present) | real_local | event_type of a middle audit record mutated (JSON kept valid) | trail fingerprint recorded before and after tampering | before/after sha256 and line counts |
| E | `e2-doctor.json` (present) | real_local | fdif doctor over the tampered trail | audit_chain check fails with a hash-mismatch finding | .checks[].ID=audit_chain .Status=fail; Detail contains 'mismatch' |
| E | `e3-trail-unchanged.json` (present) | real_local | trail fingerprint after doctor | doctor never truncates, resets, or rewrites the trail | sha256/line count identical to the tampered state |
| E | `e4-append-refused.json` (present) | real_local | cleanup-lock tries to append an audit event to the tampered trail | the append fails closed; cleanup refuses | exit non-zero; message contains 'not appendable' / reason audit_write_failed |
| E | `e5-trail-after-append-refusal.json` (present) | real_local | trail fingerprint after the refused append | trail still byte-identical; nothing was rewritten | sha256 identical to the tampered state |
| F | `f1-blocked.err` (present) | real_local | storage policy with minimum_free_bytes = 1 PiB | every mutation is blocked before it starts with 507 storage_pressure | stderr contains '{"error":"storage_pressure"' |
| F | `f3-storage-check.json` (present) | real_local | futurediff-storage-check with the same policy | reports healthy=false and the threshold finding; exit 2 | .healthy=false; .findings[] contains 'below minimum' |
| F | `f5-durablewrite-faults.txt` (present) | deterministic_injection | durablewrite injector faults: create/write/short-write/fsync/rename/dir-sync | every boundary fails closed; partial temp never authoritative; classify maps ENOSPC/EDQUOT/EROFS/EIO | go test -v ./internal/durablewrite/ (ADR-099 test seams) |
| F | `f6-storageguard-faults.txt` (present) | deterministic_injection | storageguard probe faults incl. short write and dir-sync | partial temp never authoritative; previous state preserved; no false success; retry succeeds after fault removed | go test -v ./internal/storageguard/ |
| F | `f7-restore-disk-faults.txt` (present) | deterministic_injection | disk-pressure at restore boundaries (ENOSPC/EDQUOT/EIO) | restore fails closed; quarantine retained on failure | go test -v ./internal/ledgerrestore/ |
| G | `g0-backup.json` (present) | real_local | none (home-g authoritative backup + catalog) | backup recorded in the home-g catalog so the restore reaches the write boundary | .backup.sha256 present |
| G | `g1-write-failure.err` (present) | real_local | destination directory made read-only (chmod 555) | restore refuses; previous authoritative ledger untouched | exit non-zero; ledger.db sha unchanged |
| H | `h1-receipt-faults.txt` (present) | deterministic_injection | receipt/materialization fault tests | failure before dispatch prevents provider call; failure after completion enters reconciliation; repeated recovery never duplicates | go test -v ./internal/app/ |
| I | `i1-publish-failed.err` (present) | real_local | source .git/objects made read-only (chmod 555) | publish fails cleanly; source branch unchanged; no futurediff ref created | exit non-zero; main rev-parse unchanged; zero refs/heads/futurediff |
| I | `i2-recover.json` (present) | real_local | canonical recovery after the failed commit | fdif recover re-runs the commit and reports the transaction ready | exit 0; 'Status  ready' / 'Reason  recovered' |
| I | `i3-publish-retry.json` (present) | real_local | permissions restored; publish retried after recovery | exactly one futurediff branch with exactly one commit; source branch still unchanged | refs/heads/futurediff count=1; rev-list --count main..branch =1; main unchanged |
| J | `d1-restore.json` (present) | real_local | seeded durable rows: committed+receipt, verified no attempt, verified not_found, committing unknown, needs_reconciliation unknown, manual; plus a post-backup effect | every stable state classified from durable ledger evidence only; recovery commands are exactly canonical | known_present=1 known_absent=1 ambiguous=3 (incl. newer_than_backup) no_external_effect=1 newer_than_backup=1 evidence_unavailable=0; commands = fdif recover tx-jm --yes, fdif recover tx-jr --yes, fdif status tx-jh |

## Safety invariants verified

- No provider is contacted anywhere in this drill; restore comparison and recovery guidance operate from durable ledger evidence only (attempt/receipt counts unchanged across restore: 4 attempts, 1 receipt).
- Pre-restore live ledger and WAL/SHM sidecars are preserved byte-for-byte in a private quarantine with an evidence manifest; the quarantine is never auto-deleted.
- Older-than-live backups are refused unless the operator explicitly passes `-allow-stale-backup`; every apply re-binds the operator-supplied digest to the staged bytes.
- Foreign (uncatalogued) backups and digest mismatches are refused before any mutation.
- Corrupt-ledger diagnosis and `--require-integrity` startup fail closed and never rewrite the corrupt files (fingerprints unchanged).
- Ambiguous lock ownership refuses cleanup and removes nothing; stale artifacts are cleaned only when ownership is provably dead, and normal startup succeeds afterward.
- A tampered operator audit trail is reported with the exact hash-mismatch guidance, is never truncated/reset/rewritten, and refuses appends.
- Disk pressure is evaluated before any mutation (507 `storage_pressure`); no transaction is created; the retry succeeds after capacity is restored.
- Deterministic durable-write faults: partial temporary files never become authoritative, previous state is preserved, and classification maps ENOSPC/EDQUOT/EROFS/EIO to `disk_full`/`quota_exceeded`/`filesystem_read_only`/`storage_io_failure`.
- Provider-receipt faults: failure before dispatch prevents the provider call; failure after completion enters reconciliation; repeated recovery never duplicates effects (app fault tests; prior real-GitHub evidence: `docs/certification/github-write-recovery-20260802/`).
- Local git publish failure leaves the source branch and ref namespace untouched; one retry creates exactly one branch and one commit.
- Restore comparison is read-only and idempotent (repeat restore stable) and emits only canonical recovery commands (`fdif recover <id> --yes`, `fdif status <id>`); nothing is executed automatically.

## No-secrets statement

- This drill requires no credentials, tokens, or network access. A final scan of the evidence directory for token/credential/private-key patterns found no candidate material (`secrets-scan.txt`).
- No environment dumps, headers, credential configs, or filesystem paths outside the disposable `/tmp` sandbox are recorded in the evidence.

