#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: build-public-release.sh [VERSION] [OUTPUT_DIR]
       build-public-release.sh --print-target

Builds and packages only fdif, futurediff, and futurediffd for the current
native Linux or macOS platform. Cross-compilation is intentionally not used
because the secure daemon and SQLite path require native CGO validation.
EOF
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  --print-target) print_target=1; shift ;;
  *) print_target=0 ;;
esac

normalize_target() {
  local os arch
  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "unsupported operating system: $(uname -s)" >&2; return 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; return 1 ;;
  esac
  printf '%s-%s\n' "$os" "$arch"
}

target=$(normalize_target)
if [[ $print_target -eq 1 ]]; then
  printf '%s\n' "$target"
  exit 0
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${1:-$(cat "$root/VERSION")}
out=${2:-$root/dist/public}

if [[ ! $version =~ ^v0\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "public alpha version must be pre-1.0 semantic versioning: $version" >&2
  exit 2
fi

commit=$(git -C "$root" rev-parse HEAD 2>/dev/null || printf unknown)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
dirty=false
if ! git -C "$root" diff --quiet --ignore-submodules -- 2>/dev/null || \
   ! git -C "$root" diff --cached --quiet --ignore-submodules -- 2>/dev/null; then
  dirty=true
fi

platform=${target%-*}
arch=${target#*-}
name="futurediff-$version-$platform-$arch"
stage="$out/$name"
archive="$out/$name.tar.gz"

verify_binary_version() {
  local binary output
  binary=$1
  shift

  if [[ ! -x $binary ]]; then
    echo "expected public binary is missing or not executable: $binary" >&2
    exit 1
  fi

  if ! output=$("$binary" "$@" 2>&1); then
    echo "failed to read version from public binary: $binary $*" >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi

  if [[ $output != *"$version"* ]]; then
    echo "public binary version output does not include $version: $binary $*" >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi
}

rm -rf "$stage" "$archive" "$archive.sha256"
mkdir -p "$stage/bin" "$stage/completions"

ldflags="-s -w"
ldflags+=" -X github.com/SHOnnay/futurediff/internal/buildinfo.Version=$version"
ldflags+=" -X github.com/SHOnnay/futurediff/internal/buildinfo.Commit=$commit"
ldflags+=" -X github.com/SHOnnay/futurediff/internal/buildinfo.Date=$date"
ldflags+=" -X github.com/SHOnnay/futurediff/internal/buildinfo.Dirty=$dirty"

for command in fdif futurediff futurediffd; do
  CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" \
    -o "$stage/bin/$command" "$root/cmd/$command"
done

cp "$root/LICENSE" "$root/README.md" "$root/VERSION" "$stage/"
cp "$root"/completions/_fdif "$root"/completions/fdif.bash \
   "$root"/completions/fdif.fish "$root"/completions/fdif.ps1 \
   "$stage/completions/"

verify_binary_version "$stage/bin/fdif" version
verify_binary_version "$stage/bin/futurediff" version
verify_binary_version "$stage/bin/futurediffd" --version

metadata_path=$(
  find "$stage" \( -name '._*' -o -name '.DS_Store' \) -print -quit
)
if [[ -n $metadata_path ]]; then
  echo "unexpected macOS metadata in public staging tree: $metadata_path" >&2
  exit 1
fi

symlink_path=$(find "$stage" -type l -print -quit)
if [[ -n $symlink_path ]]; then
  echo "unexpected symlink in public staging tree: $symlink_path" >&2
  exit 1
fi

(
  cd "$out"
  COPYFILE_DISABLE=1 tar -czf \
    "$(basename "$archive")" "$(basename "$stage")"
)

actual_entries=$(mktemp "${TMPDIR:-/tmp}/futurediff-archive-actual.XXXXXX")
expected_entries=$(mktemp "${TMPDIR:-/tmp}/futurediff-archive-expected.XXXXXX")

tar -tzf "$archive" |
  sed '/\/$/d' |
  LC_ALL=C sort > "$actual_entries"

cat > "$expected_entries" <<EOF
$name/LICENSE
$name/README.md
$name/VERSION
$name/bin/fdif
$name/bin/futurediff
$name/bin/futurediffd
$name/completions/_fdif
$name/completions/fdif.bash
$name/completions/fdif.fish
$name/completions/fdif.ps1
EOF
LC_ALL=C sort -o "$expected_entries" "$expected_entries"

if ! diff -u "$expected_entries" "$actual_entries"; then
  rm -f "$actual_entries" "$expected_entries"
  echo "public archive contains missing or unexpected entries" >&2
  exit 1
fi

rm -f "$actual_entries" "$expected_entries"
rm -rf "$stage"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$out" && sha256sum "$(basename "$archive")" > "$(basename "$archive").sha256")
else
  digest=$(shasum -a 256 "$archive" | awk '{print $1}')
  printf '%s  %s\n' "$digest" "$(basename "$archive")" > "$archive.sha256"
fi

printf 'Built %s\n' "$archive"
printf 'Checksum %s\n' "$archive.sha256"
