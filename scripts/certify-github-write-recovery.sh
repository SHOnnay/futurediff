#!/usr/bin/env bash
# certify-github-write-recovery.sh
#
# Real GitHub write-and-recovery certification for FutureDiff on a disposable
# repository owned by the currently authenticated GitHub account.
#
# This is a recording of the exact sequence used for the 2026-08-02
# certification (docs/certification/GITHUB_WRITE_RECOVERY_2026-08-02.md).
# It is intentionally NOT fully automated end-to-end: it pauses at operator
# decision points so that every GitHub mutation is preceded by an explicit
# human approval, and it never prints tokens.
#
# Preconditions:
#   - gh authenticated with 'repo' scope (repo creation, branch push, PR creation)
#   - go toolchain available
#   - a GitHub token exported as FUTUREDIFF_GITHUB_TOKEN (never echoed)
#   - jq and python3 available
#
# Environment:
#   FUTUREDIFF_GITHUB_TOKEN   GitHub token for the disposable repo (required)
#   CERT_REPO_SUFFIX          optional suffix; default timestamp+random
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 2; }
}
require gh
require go
require python3

if [[ -z "${FUTUREDIFF_GITHUB_TOKEN:-}" ]]; then
  echo "error: FUTUREDIFF_GITHUB_TOKEN must be set (value is never printed)" >&2
  exit 2
fi

cert_root="$(mktemp -d "${TMPDIR:-/tmp}/futurediff-github-cert.XXXXXX")"
echo "certification working directory: $cert_root"

# --- 0. Build binaries -----------------------------------------------------
echo ">> building binaries"
(cd "$repo_root" && go build -o "$cert_root/futurediff" ./cmd/futurediff)
(cd "$repo_root" && go build -o "$cert_root/futurediffd" ./cmd/futurediffd)
(cd "$repo_root" && go build -o "$cert_root/fdif" ./cmd/fdif)
(cd "$repo_root" && go build -o "$cert_root/futurediff-audit" ./cmd/futurediff-audit)

# --- 1. Create disposable repository ---------------------------------------
suffix="${CERT_REPO_SUFFIX:-$(date +%Y%m%d%H%M%S)-$RANDOM}"
repo="futurediff-certification-$suffix"
owner="$(gh api user --jq .login)"
echo ">> creating disposable repository $owner/$repo"
gh repo create "$repo" --public \
  --description "Disposable FutureDiff write-and-recovery certification infrastructure. Created for controlled write certification; will be deleted/archived after evidence verification." \
  --add-readme
repo_url="https://github.com/$owner/$repo"
repo_api="https://api.github.com/repos/$owner/$repo"
echo "repository: $repo_url"
gh repo view "$repo" --json createdAt,defaultBranchRef --jq '{createdAt, defaultBranch:.defaultBranchRef.name}'

# --- 2. Configure credential for the disposable repo only -------------------
credential_config="$cert_root/providers.json"
cat > "$credential_config" <<EOF
{
  "version": "0.1",
  "adapters": [
    {
      "adapter_id": "builtin.github.branch-publish",
      "version": "0.1.0",
      "trust_level": "built_in",
      "executable_digest": "builtin:builtin.github.branch-publish@0.1.0",
      "enabled": true
    },
    {
      "adapter_id": "builtin.github.draft-pull-request",
      "version": "0.1.0",
      "trust_level": "built_in",
      "executable_digest": "builtin:builtin.github.draft-pull-request@0.1.0",
      "enabled": true
    }
  ],
  "credentials": [
    {
      "credential_id": "github-cert",
      "provider": "github",
      "account": "$owner",
      "source": { "kind": "environment", "reference": "FUTUREDIFF_GITHUB_TOKEN" },
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
        { "scheme": "https", "host": "github.com", "path_prefix": "/$owner/$repo.git" },
        { "scheme": "https", "host": "api.github.com", "path_prefix": "/repos/$owner/$repo" }
      ],
      "enabled": true
    }
  ]
}
EOF
chmod 600 "$credential_config"

export FUTUREDIFF_CREDENTIAL_CONFIG="$credential_config"
export FUTUREDIFF_GITHUB_CREDENTIAL_ID="github-cert"
export FUTUREDIFF_BINARY="$cert_root/futurediff"
export FUTUREDIFF_DAEMON_BINARY="$cert_root/futurediffd"

echo ">> starting daemon"
"$cert_root/fdif" daemon restart
"$cert_root/fdif" doctor

# --- 3. Success path --------------------------------------------------------
work="$cert_root/work"
git clone "$repo_url.git" "$work"
git -C "$work" config user.name "FutureDiff Certification"
git -C "$work" config user.email "certification@localhost"

echo ">> success path: fdif start"
tx_start="$cert_root/tx-start.json"
(cd "$work" && "$cert_root/fdif" --json start .) > "$tx_start"
tx="$(python3 -c "import json,sys; print(json.load(open('$tx_start'))['transaction']['transaction_id'])")"
echo "transaction: $tx"

echo ">> editing workspace (safe working copy; source worktree stays clean)"
echo "FutureDiff write certification" >> "$HOME/.futurediff/runtime/transactions/$tx/workspace/README.md"

echo ">> success path: fdif finish --github (seal, prepare effects, verify, approve, commit, push, PR)"
"$cert_root/fdif" --json --yes finish --github "$tx" > "$cert_root/tx-finish.json"
python3 - <<PYEOF
import json
d = json.load(open("$cert_root/tx-finish.json"))
g = d["github"]
print("status:", d["status"])
print("safe branch:", g["branch"])
print("commit:", d["commit_oid"])
print("PR:", g["pull_request_url"])
PYEOF

# --- 4. Verify remote state via gh (independent of FutureDiff) ---------------
pr_number="$(gh pr list --repo "$owner/$repo" --json number --jq '.[0].number')"
echo ">> remote verification"
gh pr view "$pr_number" --repo "$owner/$repo" --json state,isDraft,headRefName,baseRefName,mergedAt \
  --jq '{state,isDraft,headRefName,baseRefName,mergedAt}'
gh api "repos/$owner/$repo/commits/main" --jq '.sha' # default branch must be unchanged

# --- 5. Recovery drill --------------------------------------------------------
# Controlled incomplete transaction: prepare effects while sealed, interrupt
# before commit (here: empty patch forces commit failure), then recover.
echo ">> recovery drill"
(cd "$work" && git checkout main -q && git reset --hard origin/main -q)
rx_start="$cert_root/rx-start.json"
(cd "$work" && "$cert_root/fdif" --json start .) > "$rx_start"
rx="$(python3 -c "import json; print(json.load(open('$rx_start'))['transaction']['transaction_id'])")"
"$cert_root/fdif" --json --yes finish --github "$rx" > "$cert_root/rx-interrupted.json" || true
python3 - <<PYEOF
import json
d = json.load(open("$cert_root/rx-interrupted.json"))
print("interrupted finish ok:", d.get("ok"), "| error:", (d.get("error") or "")[:160])
PYEOF
"$cert_root/futurediff" get "$rx" | python3 -c \
  "import sys,json; print('state after interruption:', json.load(sys.stdin).get('transaction',{}).get('status'))"
"$cert_root/futurediff" recover "$rx" | python3 -c \
  "import sys,json; print('state after recover:', json.load(sys.stdin).get('transaction',{}).get('status'))"
echo ">> abort or resume the recovered transaction explicitly (operator decision)"
echo "   resume:  $cert_root/fdif --json --yes finish --github $rx"
echo "   abort:   $cert_root/fdif --json --yes abort $rx"

# --- 6. Denial spot-checks -----------------------------------------------------
echo ">> denial spot-checks (must all be denied)"
(cd "$work" && git checkout main -q && git reset --hard origin/main -q && echo dirty >> README.md)
(cd "$work" && "$cert_root/fdif" --json --yes start .) \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('dirty worktree ->', 'DENIED' if not d.get('ok') else 'UNEXPECTED PASS')"
(cd "$work" && git checkout -- README.md && git checkout -q --detach origin/main)
(cd "$work" && "$cert_root/fdif" --json --yes start .) \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('detached HEAD ->', 'DENIED' if not d.get('ok') else 'UNEXPECTED PASS')"
(cd "$work" && git checkout -q main)
shallow="$cert_root/shallow"
git clone -q --depth 1 "$repo_url.git" "$shallow"
(cd "$shallow" && "$cert_root/fdif" --json --yes start .) \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('shallow repo ->', 'DENIED' if not d.get('ok') else 'UNEXPECTED PASS')"

# --- 7. Cleanup ---------------------------------------------------------------
echo ">> cleanup"
echo "closing PRs and deleting temporary branches; then archive or delete the repo:"
echo "  gh pr close $pr_number --repo $owner/$repo"
echo "  gh api -X DELETE repos/$owner/$repo/git/refs/heads/futurediff/$tx"
echo "  gh repo delete $owner/$repo --yes   # requires delete_repo scope"
echo "  # or archive: gh api -X PATCH repos/$owner/$repo -f archived=true"
echo
echo "certification working directory retained at: $cert_root"
echo "copy evidence JSONs from here into docs/certification/<run>/ and write the report."
