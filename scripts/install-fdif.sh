#!/usr/bin/env bash
set -euo pipefail

prefix="/usr/local"
dry_run=0
build=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) prefix=${2:?--prefix requires a value}; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    --no-build) build=0; shift ;;
    -h|--help)
      echo "usage: $0 [--prefix DIR] [--dry-run] [--no-build]"
      exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
if [[ $build -eq 1 ]]; then
  if [[ $dry_run -eq 1 ]]; then
    printf '+ mkdir -p bin\n'
    printf '%s\n' \
      '+ go build -trimpath -o bin/futurediff ./cmd/futurediff' \
      '+ go build -trimpath -o bin/futurediffd ./cmd/futurediffd' \
      '+ go build -trimpath -o bin/fdif ./cmd/fdif'
  else
    mkdir -p bin
    go build -trimpath -o bin/futurediff ./cmd/futurediff
    go build -trimpath -o bin/futurediffd ./cmd/futurediffd
    go build -trimpath -o bin/fdif ./cmd/fdif
  fi
fi
for binary in futurediff futurediffd fdif; do
  [[ -f "bin/$binary" ]] || { echo "missing bin/$binary" >&2; exit 1; }
done

destination="$prefix/bin"
if [[ $dry_run -eq 1 ]]; then
  echo "+ install -d $destination"
  for binary in futurediff futurediffd fdif; do
    echo "+ install -m 0755 bin/$binary $destination/$binary"
  done
  exit 0
fi
install -d "$destination"
for binary in futurediff futurediffd fdif; do
  install -m 0755 "bin/$binary" "$destination/$binary"
done
printf 'Installed futurediff, futurediffd, and fdif to %s\n' "$destination"
