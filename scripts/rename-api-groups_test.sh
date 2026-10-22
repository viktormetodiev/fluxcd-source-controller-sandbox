#!/usr/bin/env bash
#
# Offline test for rename-api-groups.sh, on a small repository that has the kinds
# of files the script rewrites (and some it must not).
#
# Needs only git and bash. Run: scripts/rename-api-groups_test.sh

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

pass=0
fail=0

check() {
    if eval "$2"; then
        echo "  ok    $1"
        pass=$((pass + 1))
    else
        echo "  FAIL  $1"
        fail=$((fail + 1))
    fi
}

new_repo() {
    cd "$TMP" && rm -rf repo && git init -q -b main repo && cd repo || exit 1
    mkdir -p config/crd/bases docs scripts
    cat > config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml <<'EOF'
spec:
  group: source.toolkit.fluxcd.io
  names:
    kind: GitRepository
    shortNames:
    - gitrepo
    singular: gitrepository
EOF
    cat > main.go <<'EOF'
// +kubebuilder:resource:shortName=gitrepo
const Finalizer = "finalizers.fluxcd.io"
const Group = "source.toolkit.fluxcd.io"
EOF
    printf 'domain: toolkit.fluxcd.io\ngroup: source\n' > PROJECT
    printf 'The group is source.toolkit.fluxcd.io.\n' > docs/spec.md
    printf 'Mentions source.toolkit.fluxcd.io but is not a rewritten file type.\n' > README.md
    git add -A && git commit -qm base
    cp "$HERE/rename-api-groups.sh" scripts/
}

echo "rename"
new_repo
ORIGINAL_CRD=$(cat config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml)
scripts/rename-api-groups.sh > /dev/null 2>&1; rc=$?
check "exits 0" '[ $rc = 0 ]'
check "Go files: group, finalizer and short name" 'grep -q "cd.qdrant.io" main.go && grep -q "finalizers.qdrant.io" main.go && grep -q "shortName=qdrantgitrepo" main.go'
check "PROJECT: domain and group" 'grep -q "domain: qdrant.io" PROJECT && grep -q "group: cd$" PROJECT'
check "docs/*.md is rewritten" 'grep -q "cd.qdrant.io" docs/spec.md'
check "other markdown files are not" 'grep -q "source.toolkit.fluxcd.io" README.md'
check "the CRD gets a renamed copy, with the group and the short name list rewritten" 'grep -q "group: cd.qdrant.io" config/crd/bases/cd.qdrant.io_gitrepositories.yaml && grep -q -- "- qdrantgitrepo" config/crd/bases/cd.qdrant.io_gitrepositories.yaml'
check "the original CRD file is restored as it was" '[ "$(cat config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml)" = "$ORIGINAL_CRD" ]'
check "no backup files are left behind" '[ -z "$(find . -name "*.rename-api-groups.bak" -not -path "./.git/*")" ]'

echo "running it twice"
scripts/rename-api-groups.sh > /dev/null 2>&1; rc=$?
before=$(git status --porcelain | sort)
scripts/rename-api-groups.sh > /dev/null 2>&1
check "is idempotent" '[ $rc = 0 ] && [ "$before" = "$(git status --porcelain | sort)" ]'

echo "a fork that dropped the original CRD files"
new_repo
git rm -q config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml
mkdir -p config/crd/bases
printf 'group: cd.qdrant.io\n' > config/crd/bases/cd.qdrant.io_gitrepositories.yaml
git add -A && git commit -qm "fork"
scripts/rename-api-groups.sh > /dev/null 2>&1; rc=$?
check "still exits 0, there is nothing to restore" '[ $rc = 0 ]'

echo
echo "passed: $pass  failed: $fail"
[ "$fail" = 0 ]
