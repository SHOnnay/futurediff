# What to push to GitHub

Do not push a collection of historical overlay ZIP files into the source repository. Apply the newest cumulative overlay to the canonical repository, review the diff, validate it, and push the resulting normal source tree.

## Recommended sequence

```bash
# 1. Extract the final overlay outside the repository.
unzip FutureDiff_Tasks_111_180_Clean_CLI_UI_Overlay.zip

# 2. Preview the changes.
cd FutureDiff_Tasks_111_180_Clean_CLI_UI_Overlay
./scripts/apply-overlay.sh --dry-run /absolute/path/to/canonical/futurediff

# 3. Apply. Use --force only after reviewing conflicts and backups.
./scripts/apply-overlay.sh /absolute/path/to/canonical/futurediff

# 4. Validate the merged repository.
cd /absolute/path/to/canonical/futurediff
./scripts/validate-overlay.sh
go test ./...
go test -race ./...
go build ./...

# 5. Review and commit.
git status
git diff --check
git add .
git commit -m "feat: add production assurance and clean CLI terminal UI"
git push origin <your-branch>
```

## Files that should be visible at repository root

- `README.md`
- `LICENSE`
- `SECURITY.md`
- `CONTRIBUTING.md`
- `.github/workflows/`
- `cmd/` and `internal/` from the canonical Go repository
- `tools/`, `scripts/`, `config/`, `schemas/`, `tests/`, `docs/`, and `examples/`

## Do not commit

- credentials, tokens, `.env` files, private keys, raw OIDC tokens, or customer data;
- `.futurediff-overlay-backup/`;
- temporary validation output;
- local runtime databases;
- historical ZIP collections unless published separately as GitHub Release assets.

## Release assets

Attach the verified source archive, checksum, SBOM, provenance, signature, and evidence bundle to a GitHub Release after the hosted workflows pass. Keep the Git repository itself source-focused.
