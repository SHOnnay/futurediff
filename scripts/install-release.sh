#!/usr/bin/env bash
set -euo pipefail

repo="SHOnnay/futurediff"
version=""
prefix="${HOME}/.local"
dry_run=0
print_asset=0

usage() {
  cat <<'EOF'
usage: install-release.sh --version VERSION [options]

Options:
  --version VERSION   required release, for example v0.1.0-alpha.3
  --prefix DIR        installation prefix (default: $HOME/.local)
  --repo OWNER/REPO   release repository (default: SHOnnay/futurediff)
  --dry-run           print actions without downloading or installing
  --print-asset       print the resolved archive name and exit
  -h, --help          show this help

The installer always downloads and verifies the release checksum sidecar.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version=${2:?--version requires a value}; shift 2 ;;
    --prefix) prefix=${2:?--prefix requires a value}; shift 2 ;;
    --repo) repo=${2:?--repo requires a value}; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    --print-asset) print_asset=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n $version ]] || { echo "--version is required" >&2; exit 2; }
[[ $version =~ ^v0\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || {
  echo "unsupported public version: $version" >&2; exit 2;
}
[[ $repo =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || {
  echo "invalid repository: $repo" >&2; exit 2;
}

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="futurediff-$version-$os-$arch.tar.gz"
if [[ $print_asset -eq 1 ]]; then
  printf '%s\n' "$asset"
  exit 0
fi

base="https://github.com/$repo/releases/download/$version"
destination="$prefix/bin"
if [[ $dry_run -eq 1 ]]; then
  printf '+ download %s/%s\n' "$base" "$asset"
  printf '+ download %s/%s.sha256\n' "$base" "$asset"
  printf '+ verify SHA-256\n'
  printf '+ install fdif futurediff futurediffd into %s\n' "$destination"
  exit 0
fi

for command in curl tar; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing required command: $command" >&2; exit 1; }
done

tmp=$(mktemp -d "${TMPDIR:-/tmp}/futurediff-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$tmp/$asset" "$base/$asset"
curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$tmp/$asset.sha256" "$base/$asset.sha256"

(
  cd "$tmp"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$asset.sha256"
  else
    shasum -a 256 -c "$asset.sha256"
  fi
)

tar -xzf "$tmp/$asset" -C "$tmp"
root="$tmp/${asset%.tar.gz}"
for binary in fdif futurediff futurediffd; do
  [[ -x "$root/bin/$binary" ]] || { echo "archive missing executable: $binary" >&2; exit 1; }
done

install -d "$destination"
for binary in fdif futurediff futurediffd; do
  install -m 0755 "$root/bin/$binary" "$destination/$binary"
done

printf 'Installed FutureDiff %s to %s\n' "$version" "$destination"
printf 'Run: %s/fdif doctor\n' "$destination"
