#!/usr/bin/env bash
# Rebrand the template for a new project.
#
#   scripts/rename.sh --module github.com/acme/orders --name Orders
#
# Rewrites the Go module path, the binary/image identifier and the product name
# shown in the UI. Run it on a clean checkout, before writing project code, and
# review the diff: it is a textual replacement, not a refactoring engine.
set -euo pipefail

module=""
product=""
binary=""

while [ $# -gt 0 ]; do
  case "$1" in
    --module) module="${2:-}"; shift 2 ;;
    --name) product="${2:-}"; shift 2 ;;
    --binary) binary="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$module" ] || [ -z "$product" ]; then
  echo "usage: scripts/rename.sh --module <go/module/path> --name <ProductName> [--binary <name>]" >&2
  exit 2
fi

# Default the binary/image identifier to the module's last path segment.
[ -n "$binary" ] || binary="${module##*/}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

old_module="github.com/junler/intent-admin"
old_binary="intent-admin"
old_product="Intent"

if ! grep -q "^module $old_module$" go.mod; then
  echo "go.mod does not declare $old_module — already renamed?" >&2
  exit 1
fi

if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  echo "working tree is dirty; commit or stash first so the rename is reviewable" >&2
  exit 1
fi

# Skip build output, dependencies and VCS metadata.
files() {
  git ls-files -z -- \
    ':!:web/package-lock.json' \
    ':!:internal/web/dist' \
    ':!:web/dist'
}

replace() {
  local from="$1" to="$2"
  if [ "$from" = "$to" ]; then
    return 0
  fi
  # Pass both sides through the environment: the strings contain slashes and
  # dots, which must not reach perl as regex syntax or substitution delimiters.
  files | xargs -0 grep -l -F -- "$from" 2>/dev/null | while read -r file; do
    FROM="$from" TO="$to" perl -pi -e 's/\Q$ENV{FROM}\E/$ENV{TO}/g' "$file"
  done
}

# Longest first: the module path contains the binary name.
replace "$old_module" "$module"
replace "$old_binary" "$binary"
replace "$old_product" "$product"

"${GO:-go}" mod edit -module "$module"
"${GO:-go}" mod tidy

echo "Renamed to module=$module binary=$binary product=$product"
echo "Review with: git diff --stat"
echo "Then verify:  make lint && make test"
