# Task 029 — Disposable provider mutation certification

## Objective

Test real provider mutation and cleanup semantics only after an operator explicitly opts in.

## Delivered

- `futurediff-provider-cert`
- Exact confirmation phrase requirement
- GitHub test workflow:
  - read default branch
  - create an unreachable test commit
  - create `futurediff-cert/*` branch
  - create draft pull request
  - close draft pull request
  - delete test branch
- Slack test workflow:
  - post uniquely marked message
  - delete the message
- Controlled egress for real execution
- Token values accepted only through named environment variables
- Machine-readable PASS/FAIL/BLOCKED/SKIP report
- Deterministic fake-provider tests proving cleanup

## Executed result

The fake HTTPS provider suite passed, including branch deletion, PR closure, and Slack deletion. The real command was run with a missing token and correctly returned BLOCKED with a nonzero exit status. No real external resource was created.

## Limitations

A Git commit object created through GitHub's API may remain unreachable until provider garbage collection after its temporary branch is deleted. Certification must therefore use a dedicated disposable repository.
