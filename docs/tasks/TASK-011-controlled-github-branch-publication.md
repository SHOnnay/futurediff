# Task 011 — Controlled GitHub Branch Publication and Effect Dependencies

**Status:** Completed  
**Primary language:** Go  
**Date:** 2026-07-27

## 1. Objective

Close the repository-to-pull-request integrity gap by publishing the exact approved FutureDiff commit to a new remote GitHub branch and making the draft pull request depend on that durable publication receipt.

## 2. Problem addressed

Before Task 011, FutureDiff could create a draft pull request for an already existing remote head branch. That branch was pinned and checked, but it was not necessarily the exact commit generated from the approved local FutureDiff patch.

Task 011 establishes this chain:

```text
approved staged tree
    → deterministic Git commit identity
    → create-only remote futurediff/* branch
    → durable branch receipt
    → dependent draft pull request
```

## 3. Acceptance criteria

1. Predict the exact commit identity before remote publication.
2. Do not mutate the user's checkout or publish a local ref during prediction.
3. Restrict remote publication to new `futurediff/*` branches.
4. Reject an already existing remote branch.
5. Bind branch owner, repository, URL, commit, and tree to approval material.
6. Transport credentials without placing them in arguments, URLs, or child-process environment.
7. Store a write-ahead provider attempt before push.
8. Treat push transport errors as ambiguous.
9. Reconcile ambiguous outcomes by reading the remote branch.
10. Add durable effect dependencies.
11. Prevent a dependent pull request from committing before the branch receipt.
12. Require the final PR head SHA to equal the exact approved commit.
13. Preserve all previous transaction and provider tests.

## 4. Implementation

### Deterministic commit identity

`internal/staging.Manager.PredictMaterializedRef` creates the same commit object later used by `Materialize` through `git commit-tree` with deterministic author, committer, timestamp, parent, tree, and message.

The object is unreachable until an approved publication path creates a ref. This allows provider effects to reference the exact commit SHA without changing the live repository.

### GitHub branch adapter

Added `internal/adapters/githubbranch`.

Capabilities:

```text
github.query_git_ref
github.publish_branch
```

Restrictions:

- exact HTTPS remote URL;
- no embedded user information;
- default HTTPS port only;
- path must equal `/<owner>/<repo>.git`;
- branch must begin `futurediff/`;
- full commit and tree object IDs required;
- create-only publication.

The secure Git runner uses `GIT_ASKPASS` and an inherited file descriptor. It clears credential helpers, disables terminal prompts and redirects, uses a temporary HOME, and supplies a minimal environment.

### Create-only lease

Publication uses:

```text
--force-with-lease=refs/heads/<branch>:
```

The empty expected-old value means the operation can create an absent branch but cannot overwrite an existing branch.

### Effect dependency graph

Added `depends_on` to external-effect records and migration `0007_effect_dependencies.sql`.

The ledger validates that each dependency exists in the same transaction. Verification and approval material include dependencies. The coordinator checks durable committed receipts before releasing dependents.

### Bound draft PR

A draft-PR input can declare `depends_on_effect_id`. When bound to a branch effect:

- exact predicted head SHA becomes part of PR preparation;
- pre-commit freshness permits the branch to be absent before publication;
- final freshness requires the published branch to equal the exact predicted SHA;
- the PR cannot commit before the branch effect.

### API and CLI

Added:

```text
POST /v1/transactions/{id}/effects/github/branch
```

CLI:

```text
prepare-github-branch
```

The draft-PR CLI accepts an optional branch-effect dependency ID.

## 5. Failure semantics

### Branch already exists

Preparation fails. FutureDiff does not overwrite or adopt the branch implicitly.

### Push result is ambiguous

```text
branch effect → UNKNOWN
transaction → NEEDS_RECONCILIATION
```

Recovery queries the exact ref. Exact approved SHA finalizes the receipt; another SHA is a conflict; absence may permit safe re-arming.

### Dependency not committed

The coordinator does not release the dependent PR or Slack message.

### Remote branch changes unexpectedly

The effect remains stale/conflicted. FutureDiff does not force-update it.

## 6. Validation

Passed:

```text
gofmt
go vet ./...
go test ./...
go test -race ./...
```

Key automated tests:

- deterministic commit prediction and materialization;
- create-only branch publication;
- existing-branch rejection;
- remote status lookup;
- exact branch receipt;
- branch publication precedes bound PR;
- PR commits only after dependency receipt;
- full transaction reaches committed state with one PR POST.

Provider behavior used deterministic fake transports/runners. No real GitHub resource was modified.

## 7. Limitations

- Real GitHub smart-HTTP publication has not yet been run against a dedicated test repository in this environment.
- The adapter creates new FutureDiff branches only; it cannot update, delete, force-push, tag, merge, or release.
- Git credentials currently originate from the bootstrap credential broker environment source.
- Git smart-HTTP behavior must be included in real-provider certification.
