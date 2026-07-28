#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
usage() { echo "usage: $0 [--dry-run] [--force] TARGET_REPOSITORY" >&2; exit 2; }
DRY_RUN=0
FORCE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    --help|-h) usage ;;
    --*) usage ;;
    *) break ;;
  esac
done
[[ $# -eq 1 ]] || usage
TARGET="$(cd "$1" && pwd)"
[[ -f "$ROOT/MANIFEST.apply" ]] || { echo "MANIFEST.apply missing" >&2; exit 2; }
[[ -f "$ROOT/MANIFEST.sha256" ]] || { echo "MANIFEST.sha256 missing" >&2; exit 2; }
(
  cd "$ROOT"
  sha256sum -c MANIFEST.sha256 >/dev/null
)
BACKUP_ROOT="$TARGET/.futurediff-overlay-backup/$(date -u +%Y%m%dT%H%M%SZ)"
while IFS= read -r rel; do
  [[ -n "$rel" ]] || continue
  [[ "$rel" != /* && "$rel" != *".."* ]] || { echo "unsafe manifest path: $rel" >&2; exit 2; }
  src="$ROOT/$rel"
  dst="$TARGET/$rel"
  [[ -f "$src" ]] || { echo "source missing: $rel" >&2; exit 2; }
  if [[ -e "$dst" || -L "$dst" ]]; then
    if cmp -s "$src" "$dst"; then
      echo "unchanged $rel"
      continue
    fi
    if [[ $FORCE -ne 1 ]]; then
      echo "conflict $rel (use --force)" >&2
      exit 3
    fi
    echo "replace $rel"
    if [[ $DRY_RUN -ne 1 ]]; then
      mkdir -p "$BACKUP_ROOT/$(dirname "$rel")"
      cp -a "$dst" "$BACKUP_ROOT/$rel"
    fi
  else
    echo "add $rel"
  fi
  if [[ $DRY_RUN -ne 1 ]]; then
    mkdir -p "$(dirname "$dst")"
    tmp="$dst.futurediff-overlay-tmp"
    cp "$src" "$tmp"
    chmod --reference="$src" "$tmp" 2>/dev/null || true
    mv "$tmp" "$dst"
  fi
done < "$ROOT/MANIFEST.apply"
echo "overlay application complete"
