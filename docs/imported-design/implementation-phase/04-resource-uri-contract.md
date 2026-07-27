# Canonical Resource URI Contract 0.1 Draft

## Status

Draft for implementation-phase Step 04.

## Objective

This document freezes the canonical resource identity model for FutureDiff. Resource URIs are the shared language used by adapters, locks, approval snapshots, drift checks, audit logs, and reconciliation.

Without this contract, two adapters can touch the same real resource but describe it differently, which breaks concurrency control and approval integrity.

## Design principles

1. One real resource MUST map to one canonical URI form.
2. Resource identity MUST be stable across transactions.
3. Locking and drift detection MUST use canonicalized URIs, not adapter-local strings.
4. URIs MUST identify the resource, not the action.
5. Version pins MUST be separate from resource identity, but reference the same canonical URI.
6. Namespace design MUST favor boring explicitness over clever compression.

## Scope

This spec defines:
- URI syntax;
- namespace rules;
- normalization rules;
- parent/child resource modeling;
- collection versus instance modeling;
- examples for git, postgres, github, slack.

It does not yet define:
- every future provider namespace;
- cryptographic version token formats;
- policy rules for lock arbitration.

## Core rule

Every effectful adapter MUST emit canonical `resource_uris[]` for all resources it may mutate, depend on for freshness, or reserve for locking.

If the adapter cannot identify the resource set accurately enough, it is not implementation-ready for strong transactional guarantees.

## URI shape

Canonical resource URIs use this general form:

```text
<scheme>://<authority>/<path>?<query>
```

Where:
- `scheme` identifies the provider or resource domain;
- `authority` identifies the tenant, host, repository, workspace, or provider root when applicable;
- `path` identifies the resource hierarchy;
- `query` is optional and only allowed for semantically relevant selectors, never presentation details.

## Allowed schemes in the initial MVP

- `git://`
- `fs://`
- `postgres://`
- `github://`
- `slack://`
- `container://`

Other schemes MAY exist later, but Step 04 freezes the rules these schemes must follow.

## General normalization rules

Canonicalization MUST apply before hashing, locking, or comparison.

### Required normalization

- lowercase the scheme;
- lowercase the authority when the provider treats it case-insensitively;
- remove default ports when the scheme defines one;
- collapse duplicate `/` path separators;
- remove trailing `/` for non-root resources;
- percent-decode then re-encode reserved characters into canonical form;
- sort query parameters by key, then value;
- omit query parameters with default or empty semantic value when the schema says to omit them;
- preserve path segment case only when the underlying system is case-sensitive.

### Forbidden normalization

The system MUST NOT:
- rewrite one resource kind into another;
- strip path segments that change meaning;
- downcase case-sensitive identifiers;
- treat human display names as canonical IDs when stable IDs exist.

## Resource identity versus resource version

A resource URI identifies *what* the resource is.
A resource version identifies *which state* of that resource was previewed, approved, or checked.

These must remain separate.

Example:

```text
resource_uri: github://github.com/owner/repo/branch/main
resource_version: sha:abc123
```

Version tokens MUST be stored alongside resource URIs, not embedded into the canonical URI path.

## Collection versus instance rules

A collection resource and an instance resource are different URIs.

Examples:

```text
github://github.com/owner/repo/pulls
github://github.com/owner/repo/pull/431
```

Adapters MUST lock the narrowest safe instance resource possible.
If only a collection-level lock is safe, the adapter MUST say so explicitly.

## Parent and derived resource rules

Effects often touch more than one resource.
For example, opening a pull request may depend on:
- repository;
- base branch;
- head branch;
- pull-request collection;
- created pull request instance after commit.

Adapters MUST distinguish:
- **primary mutated resources**;
- **freshness-dependent resources**;
- **derived resources created only after commit**.

Derived resources MAY be absent during `prepare`, but the adapter MUST expose the parent collection URI so locking and approval still have a canonical anchor.

## Query-string rules

Query strings are allowed only when they select a semantically distinct subresource.

Good:

```text
postgres://db.example.com/appdb/schema/public/table/users?column=email
```

Bad:

```text
github://github.com/owner/repo/pull/431?tab=files
```

Presentation and UI navigation details MUST NOT appear in canonical URIs.

## Required resource binding metadata

Whenever a resource URI is emitted, the adapter SHOULD also classify it as one of:
- `mutates`
- `reads_for_freshness`
- `locks_only`
- `derived_on_commit`

This classification improves locking precision and audit clarity.

## Namespace rules by domain

## `git://`
Use for repository and branch identity in local or hosted git semantics.

### Canonical forms

```text
git://<repo-id>/repo
git://<repo-id>/branch/<branch-name>
git://<repo-id>/tag/<tag-name>
git://<repo-id>/commit/<commit-sha>
git://<repo-id>/path/<normalized-path>
```

### Rules

- `<repo-id>` MUST be a stable repository identity chosen by the integration layer.
- Branch names preserve case if the git host/runtime does.
- `path` is repository-relative, never absolute host filesystem path.
- Worktree-specific temporary locations MUST NOT appear in the canonical git URI.

## `fs://`
Use for direct filesystem resources not already modeled under git.

### Canonical forms

```text
fs://localhost/abs/<normalized-absolute-path>
fs://localhost/volume/<volume-id>/path/<normalized-path>
```

### Rules

- Prefer `git://` for tracked repository resources when possible.
- Use `fs://` for generated artifacts, temp files that matter to verification, or non-git assets.
- Canonicalization MUST resolve `.` and `..` before URI generation.
- Symbolic-link handling MUST follow one published rule per runtime; the adapter must not switch between logical and physical path identity arbitrarily.

## `postgres://`
Use for database, schema, table, row-range, and migration-relevant resources.

### Canonical forms

```text
postgres://<server>/<database>
postgres://<server>/<database>/schema/<schema-name>
postgres://<server>/<database>/schema/<schema-name>/table/<table-name>
postgres://<server>/<database>/schema/<schema-name>/table/<table-name>?column=<column-name>
postgres://<server>/<database>/migration/<migration-id>
```

### Rules

- `<server>` MUST be a stable logical DB authority, not a transient container hostname unless the transaction is intentionally local-only.
- Schema and table names preserve case rules consistent with the actual database semantics.
- Row-level URIs are intentionally out of the initial MVP unless a provider-specific adapter truly needs them.
- Migration effects SHOULD lock both the migration resource and the affected schema/table resources when known.

## `github://`
Use for GitHub repository resources and related artifacts.

### Canonical forms

```text
github://github.com/<owner>/<repo>
github://github.com/<owner>/<repo>/branch/<branch-name>
github://github.com/<owner>/<repo>/pulls
github://github.com/<owner>/<repo>/pull/<pull-number>
github://github.com/<owner>/<repo>/issues
github://github.com/<owner>/<repo>/issue/<issue-number>
github://github.com/<owner>/<repo>/labels/<label-name>
```

### Rules

- Owner and repo names SHOULD follow GitHub canonical casing if available, but comparison must respect GitHub's case-insensitive identity.
- Do not encode web UI tabs, filters, or anchors.
- Creating a new PR before number assignment SHOULD lock `.../pulls` plus relevant branches.
- After commit, the adapter MUST record the concrete `pull/<number>` URI in receipts.

## `slack://`
Use for Slack workspace, channel, thread, and message targets.

### Canonical forms

```text
slack://<workspace-id>/channel/<channel-id>
slack://<workspace-id>/channel/<channel-id>/thread/<thread-ts>
slack://<workspace-id>/channel/<channel-id>/message/<message-ts>
slack://<workspace-id>/user/<user-id>
```

### Rules

- Use immutable Slack IDs, never channel display names, as canonical identifiers.
- A prepared outbound message before send SHOULD lock the target channel or thread URI.
- After commit, the receipt SHOULD include the concrete message URI if one exists.

## `container://`
Use for staged runtime instances that matter to verification or artifact production.

### Canonical forms

```text
container://<runtime>/<container-id>
container://<runtime>/<container-id>/volume/<volume-name>
container://<runtime>/<container-id>/network/<network-name>
```

### Rules

- Container URIs are primarily local transactional resources, not external approval anchors.
- If the runtime tears down and recreates containers, the canonical identity MUST bind to the staged environment role when the physical container ID is too unstable.

## Locking guidance

Locks MUST operate on canonical URIs.

Recommended behavior:
- lock exact instance URIs whenever possible;
- lock parent collection URIs when creating new children;
- lock both dependent branches for PR-like resources;
- avoid global provider locks unless absolutely necessary.

Examples:

- creating GitHub PR: lock repository branch URIs + `.../pulls`
- altering Postgres table: lock schema/table URI
- sending Slack thread reply: lock thread URI, not whole workspace

## Drift-check guidance

Freshness checks MUST compare resource versions against canonical URIs.

Examples:
- GitHub branch URI -> head SHA
- Postgres schema/table URI -> schema revision or migration watermark
- Slack thread URI -> thread existence and access constraints
- git branch URI -> current commit SHA

## Audit and receipt rules

Every durable receipt SHOULD include:
- canonical resource URIs touched;
- resource versions observed before commit when applicable;
- derived committed resource URIs created by the action.

This is required for reconciliation and later export.

## Unsupported identity cases

An adapter MUST downgrade or fail closed when:
- it only knows a human-readable label but no stable resource ID;
- one provider action fans out to unknown resources with no bounded set;
- the provider mutates hidden dependent resources that cannot be named or checked.

In these cases, strong locking and approval guarantees do not exist.

## Examples

### Local git file change
```text
mutates:
- git://repo-main/repo
- git://repo-main/path/src/auth/login.ts
reads_for_freshness:
- git://repo-main/branch/main
```

### Postgres migration
```text
mutates:
- postgres://db.prod.internal/app/schema/public
- postgres://db.prod.internal/app/schema/public/table/customers
locks_only:
- postgres://db.prod.internal/app/migration/20260726_add_customer_status
```

### GitHub pull request creation
```text
mutates:
- github://github.com/acme/payments/pulls
reads_for_freshness:
- github://github.com/acme/payments/branch/main
- github://github.com/acme/payments/branch/agent/customer-status
```

### Slack notification
```text
mutates:
- slack://T123/channel/C456
```

## Relationship to prior steps

This contract sharpens fields already introduced earlier:
- EffectSpec `resource_uri_patterns`
- approval snapshot `resource_uris` and `resource_versions`
- state-machine drift and lock rules

Without canonical URIs, those earlier contracts cannot be enforced reliably.

## Exit criteria for Step 04

Step 04 is complete when this draft is turned into:
- a final URI spec;
- canonicalization helpers shared by adapters;
- tests proving equivalent inputs normalize to one canonical URI;
- lock and drift checks implemented against canonical resources only.

## Immediate next step after this document

Freeze the conformance and benchmark contract so adapters and releases must prove these semantics under failure, retry, and recovery scenarios.
