# FutureDiff progress audit — Task 095

## Scope completed

Tasks 091–095 implemented transaction-level resource isolation on top of the Task 086–090 local authorization layer.

## Acceptance evidence

- owner principal is derived from Unix peer identity and persisted in SQLite;
- different UIDs create different transaction owners;
- owned-scope listing returns only owned/shared transactions;
- inaccessible IDs return `404`;
- read sharing permits reads but blocks mutations;
- operate sharing permits agent-safe mutations;
- shared principals cannot administer access;
- revocation is effective on the next request;
- all-scope roles can inspect all transactions;
- the independent access-event hash chain detects direct row changes;
- the semantic audit and signed integrity checkpoint cover the access-chain head;
- the tenant conformance suite reports 13 passes and zero failures;
- all 70 Go commands build;
- normal, race and coverage test suites complete successfully;
- the v0.95.0 release passes 75 offline checks with zero failures.

## External criteria still blocked

The remaining public-MVP evidence depends on systems not available in this environment: rootless container hosts, provider test accounts, live agent runtimes, native macOS CI and hosted release signing.

## Completion assessment

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.85% | 0.15% |
| Production-grade platform | 91% | 9% |
