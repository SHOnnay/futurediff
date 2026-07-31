# ADR-097: Optional GitHub publication from `fdif finish`

- Status: accepted for public alpha
- Scope: guided CLI only

## Context

FutureDiff's canonical commit path already materializes an approved local branch
and can execute prepared external effects. The low-level client can prepare a
create-only GitHub branch publication and a dependent draft pull request, but
normal users previously had to assemble that flow manually.

The guided workflow must preserve these boundaries:

- local publication works without provider credentials;
- provider actions are included in verification and exact approval;
- no external action is silently committed under local-only wording;
- the current source branch remains unchanged;
- recovery remains owned by the canonical engine.

## Decision

`fdif finish` remains the local-only default.

`fdif finish --github`:

1. resolves and validates a `github.com` remote;
2. seals the active transaction;
3. prepares the create-only branch effect;
4. prepares a draft-PR effect dependent on that exact branch effect;
5. verifies and approves the complete transaction material;
6. requires `SEND` before commit;
7. commits through the canonical FutureDiff client;
8. displays the durable draft-PR receipt URL.

Canonical branch naming remains:

```text
futurediff/<transaction-id>
```

The base branch defaults to the source branch captured at transaction creation.
The pull request is always a draft in the alpha.

## Credential boundary

`fdif` accepts only a credential configuration path and credential ID. Tokens
remain in sources controlled by the daemon credential broker. The daemon is
started with `--credential-config`, and the guided launcher rejects symlinks,
non-regular files and permissions broader than `0600`.

## State boundary

GitHub effects must be prepared in the sealed state. A transaction that reached
`ready` without those effects cannot have GitHub publication attached later.

If any external effect is already prepared, local-only `fdif finish` and guided
`fdif publish` refuse to commit it silently. Unsupported or mixed provider
effect sets require the low-level CLI.

## Failure boundary

Local branch materialization precedes external provider effects. If a provider
outcome is uncertain, the transaction may require reconciliation even though
the local safe branch exists. The guided CLI reports this condition and points
to canonical status and recovery commands; it does not bypass or duplicate
reconciliation logic.

## Consequences

Positive:

- one guided command covers the real branch-to-draft-PR flow;
- local-only use remains independent of GitHub;
- provider intent is digest-bound and visible before mutation;
- branch and PR ordering is explicit;
- durable receipts provide the PR URL.

Limitations:

- only `github.com` is supported;
- GitHub Enterprise Server is deferred;
- live provider use requires a least-privilege credential configuration;
- Windows GitHub publication remains outside the public-alpha runtime claim;
- provider effects prepared through the low-level CLI may require low-level
  completion and recovery.
