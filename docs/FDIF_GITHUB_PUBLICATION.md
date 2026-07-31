# Publishing a reviewed change to GitHub with `fdif`

FutureDiff's default success state is a safe **local** branch:

```bash
fdif finish
```

GitHub is optional. To publish the same exact approved commit to GitHub and
create a draft pull request:

```bash
fdif finish --github
```

The GitHub effects are prepared while the transaction is sealed, included in
verification and approval, and committed only after the final `SEND`
confirmation. The current source branch remains unchanged.

## Supported repository remotes

The alpha supports repositories hosted on `github.com` with remotes such as:

```text
https://github.com/OWNER/REPOSITORY.git
git@github.com:OWNER/REPOSITORY.git
ssh://git@github.com/OWNER/REPOSITORY.git
```

FutureDiff normalizes the provider destination to a credential-free HTTPS URL.
Embedded credentials, non-default ports, escaped paths, non-GitHub hosts and
unsafe repository paths are rejected.

GitHub Enterprise Server is not supported in this alpha.

## Configure a least-privilege credential

Start from the repository example:

```bash
mkdir -p "$HOME/.config/futurediff"
cp examples/credentials/providers.example.json \
  "$HOME/.config/futurediff/providers.json"
chmod 600 "$HOME/.config/futurediff/providers.json"
```

Edit the `github-main` entry so its account and both allowed destination paths
match the one repository the credential may access. Keep the token out of the
JSON file by using an environment source:

```json
{
  "credential_id": "github-main",
  "provider": "github",
  "account": "OWNER",
  "source": {
    "kind": "environment",
    "reference": "FUTUREDIFF_GITHUB_TOKEN"
  },
  "allowed_adapters": [
    "builtin.github.branch-publish",
    "builtin.github.draft-pull-request"
  ],
  "allowed_operations": [
    "github.query_git_ref",
    "github.publish_branch",
    "github.read_refs",
    "github.query_pull_requests",
    "github.create_draft_pull_request"
  ],
  "allowed_destinations": [
    {
      "scheme": "https",
      "host": "github.com",
      "path_prefix": "/OWNER/REPOSITORY.git"
    },
    {
      "scheme": "https",
      "host": "api.github.com",
      "path_prefix": "/repos/OWNER/REPOSITORY"
    }
  ],
  "enabled": true
}
```

Export the token and FutureDiff configuration in the environment used to start
the daemon:

```bash
export FUTUREDIFF_GITHUB_TOKEN='...'
export FUTUREDIFF_CREDENTIAL_CONFIG="$HOME/.config/futurediff/providers.json"
export FUTUREDIFF_GITHUB_CREDENTIAL_ID='github-main'
```

Do not put the token in shell history, command arguments, pull-request text or
FutureDiff state files. Use a token whose repository permissions are limited to
creating branches and pull requests in the intended repository.

Restart the daemon after changing credential configuration:

```bash
fdif daemon restart
fdif doctor
```

`fdif doctor` checks that the credential configuration is a regular,
non-symlink file with permissions no broader than `0600`. It does not print or
validate the secret value.

The same settings can be supplied explicitly:

```bash
fdif \
  --credential-config "$HOME/.config/futurediff/providers.json" \
  --github-credential github-main \
  finish --github
```

## Run the workflow

Start and edit the safe working copy as usual:

```bash
fdif start
fdif workspace
fdif review --full
```

Then publish locally and create a draft pull request:

```bash
fdif finish --github
```

Defaults:

- remote: `origin`;
- base: the source branch captured when the transaction started;
- head: `futurediff/<transaction-id>`;
- title: `FutureDiff change <short-transaction-id>`;
- state: draft pull request.

Customize the provider request:

```bash
fdif finish --github \
  --remote upstream \
  --base main \
  --title "Add safer validation" \
  --body-file ./pull-request-body.md
```

Available GitHub options:

```text
--github
--remote NAME
--base BRANCH
--title TEXT
--body TEXT
--body-file PATH
--github-credential ID
```

Use only one of `--body` and `--body-file`. The body file must be a regular,
non-symlink file and the adapter limits the body to 64 KiB.

## Approval and confirmation

The interactive workflow has two distinct safety decisions:

1. type `YES` to approve the exact verified transaction material;
2. type `SEND` to publish that exact approved commit and execute the prepared
   GitHub effects.

For non-interactive disposable automation, use `--yes`. This suppresses both
prompts but does not disable daemon peer authentication.

## Important transaction-state rule

Select `--github` on the first `finish` run. GitHub branch and draft-PR effects
must be prepared while the transaction is sealed so they are part of the exact
material that is verified and approved.

If a transaction already reached `ready` without GitHub effects, either:

- finish it locally with `fdif finish`; or
- start a new transaction and select `--github` before verification.

FutureDiff intentionally does not attach a new provider action after the exact
version has already been verified.

## Failure and recovery

Provider effects are ordered:

```text
materialize local safe branch
→ publish the create-only GitHub branch
→ create the dependent draft pull request
```

If a provider outcome is uncertain, FutureDiff records the effect state for
reconciliation. The local safe branch may already exist even when the command
returns an external-publication error.

Inspect first:

```bash
fdif status TRANSACTION_ID
fdif events TRANSACTION_ID
futurediff effects TRANSACTION_ID
```

Then use the canonical low-level recovery command reported by the error. Do not
manually force-push the FutureDiff branch while a transaction needs
reconciliation.

## JSON mode

```bash
fdif --json --yes finish --github
```

Success produces one JSON document. Its `github` object includes the repository,
base, safe branch, draft status, provider resource identifier and pull-request
URL when the durable receipt contains it.
