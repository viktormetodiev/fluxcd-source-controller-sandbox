#!/usr/bin/env bash
#
# Offline tests for sync-upstream.sh. Each case builds a tiny upstream repository
# and a fork of it in a temporary directory, runs the script against the local
# upstream (UPSTREAM_URL), and checks the commits and files it produces.
#
# Needs only git, make and bash. Run: scripts/sync-upstream_test.sh

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Keep the tests independent of the machine's git configuration.
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

# new_case NAME UPSTREAM_CHANGES FORK_CHANGES
#
# Upstream v1.0.0 has a workflow (scan.yaml), crd.yaml and other.txt. Upstream
# v1.0.1 is v1.0.0 plus UPSTREAM_CHANGES (a shell snippet run in the upstream
# repository). The fork is v1.0.0 plus the rename of crd.yaml, its own scan.yaml
# and ours.yaml under the owned .github/, the sync scripts, and FORK_CHANGES.
# The test continues in the fork directory, with the script's outputs going to
# $GITHUB_OUTPUT.
new_case() {
    local name=$1 upstream_changes=$2 fork_changes=$3
    local dir="$TMP/$name"
    mkdir -p "$dir" && cd "$dir" || exit 1

    git init -q -b main up && cd up || exit 1
    mkdir -p .github/workflows
    printf 'upstream scan v1\nfossa\n' > .github/workflows/scan.yaml
    { echo 'group: source.toolkit.fluxcd.io'; for i in 1 2 3 4 5 6 7 8; do echo "filler $i"; done; echo 'tail: original'; } > crd.yaml
    echo other > other.txt
    git add -A && git commit -qm v1.0.0 && git tag v1.0.0
    eval "$upstream_changes"
    git add -A && git commit -qm v1.0.1 && git tag v1.0.1
    git tag v1.0.2-rc.1   # a pre-release, which must be ignored
    git tag api/v1.0.1    # the tag of the api/ module, which must be ignored
    cd ..

    git clone -q --bare up upstream.git
    git clone -q upstream.git fork 2> /dev/null && cd fork || exit 1
    git checkout -q -B main v1.0.0
    mkdir -p scripts
    cp "$HERE/sync-upstream.sh" scripts/
    # A stand-in for the fork's rename script.
    printf '#!/usr/bin/env bash\nset -e\nfor f in *.yaml; do sed -i.bak "s/source.toolkit.fluxcd.io/cd.qdrant.io/g" "$f"; rm -f "$f.bak"; done\n' > scripts/rename-api-groups.sh
    chmod +x scripts/*.sh
    printf '# owned\n.github/\n' > scripts/fork-owned.txt
    printf 'tidy:\n\t@true\n' > Makefile
    sed -i.bak 's/source.toolkit.fluxcd.io/cd.qdrant.io/' crd.yaml && rm crd.yaml.bak
    printf 'fork codeql-only scan\n' > .github/workflows/scan.yaml
    printf 'fork ci\n' > .github/workflows/ours.yaml
    eval "$fork_changes"
    git add -A && git commit -qm fork

    export UPSTREAM_URL="$dir/upstream.git" GITHUB_OUTPUT="$dir/out.txt"
    : > "$GITHUB_OUTPUT"
    BASE=$(git rev-parse HEAD)
}

run_merge() { scripts/sync-upstream.sh merge v1.0.1 > ../merge.log 2>&1; }
first_parent_subjects() { git log --first-parent --format=%s "$BASE..HEAD"; }
commit_count() { first_parent_subjects | wc -l | tr -d ' '; }

echo "target"
new_case target 'echo "other v2" > other.txt' ':'
scripts/sync-upstream.sh target > /dev/null 2>&1
check "picks the latest stable tag, ignoring the pre-release and the api/ tag" 'grep -q "^tag=v1.0.1" $GITHUB_OUTPUT && grep -q "^status=needs-sync" $GITHUB_OUTPUT'
check "reports the branch name" 'grep -q "^branch=sync/upstream-v1.0.1" $GITHUB_OUTPUT'
scripts/sync-upstream.sh merge v1.0.1 > /dev/null 2>&1
: > "$GITHUB_OUTPUT"
scripts/sync-upstream.sh target > /dev/null 2>&1
check "says up-to-date once the tag is merged" 'grep -q "^status=up-to-date" $GITHUB_OUTPUT'
scripts/sync-upstream.sh merge 'v1.0.1;x' > /dev/null 2>&1; rc=$?
check "rejects a tag that is not vX.Y.Z (exit 2)" '[ $rc = 2 ]'
scripts/sync-upstream.sh merge v9.9.9 > /dev/null 2>&1; rc=$?
check "rejects an unknown tag (exit 2)" '[ $rc = 2 ]'

echo "upstream edits an owned file, adds a file with the old group name, edits another file"
new_case owned-edited 'printf "upstream scan v2\nfossa\n" > .github/workflows/scan.yaml; echo "new: source.toolkit.fluxcd.io" > new.yaml; echo "other v2" > other.txt' ':'
run_merge
MERGE=$(git log --first-parent --format=%h "$BASE..HEAD" | tail -1)
check "status merged" 'grep -q "^status=merged" $GITHUB_OUTPUT'
check "exactly two commits: the merge, then the adaptations" '[ "$(commit_count)" = 2 ] && [ "$(first_parent_subjects | tail -1)" = "Merge upstream tag v1.0.1" ]'
check "the adaptations commit has the expected subject" '[ "$(git log -1 --format=%s)" = "chore: apply Qdrant adaptations after merging v1.0.1" ]'
check "the merge commit is upstream as released: its scan.yaml" 'git show $MERGE:.github/workflows/scan.yaml | grep -q "upstream scan v2"'
check "the merge commit is upstream as released: the new file is not renamed yet" 'git show $MERGE:new.yaml | grep -q "source.toolkit.fluxcd.io"'
check "the final tree has the fork's scan.yaml" 'grep -q "fork codeql-only" .github/workflows/scan.yaml'
check "the final tree has the new file renamed" 'grep -q "cd.qdrant.io" new.yaml'
check "the unrelated upstream change arrived" 'grep -q "other v2" other.txt'
check "the message lists the restored file with its status" 'git log -1 --format=%b | grep -q "^M[[:space:]]*.github/workflows/scan.yaml"'
check "the message counts the files the rename script changed" 'git log -1 --format=%b | grep -q "rename-api-groups.sh changed 1 file(s)"'
check "the message does not mention tidy, which changed nothing" '! git log -1 --format=%b | grep -q "make tidy changed"'
check "the fork's own workflow is untouched" 'grep -q "fork ci" .github/workflows/ours.yaml && ! git show --name-only --format= HEAD | grep -q ours.yaml'

echo "upstream changes nothing the fork owns"
new_case untouched 'echo "other v2" > other.txt' ':'
run_merge
check "status merged" 'grep -q "^status=merged" $GITHUB_OUTPUT'
check "only the merge commit, no empty adaptations commit" '[ "$(commit_count)" = 1 ]'
check "the fork's scan.yaml is untouched" 'grep -q "fork codeql-only" .github/workflows/scan.yaml'

echo "upstream adds a new workflow"
new_case new-workflow 'printf "upstream new workflow\n" > .github/workflows/new-upstream.yaml' ':'
run_merge
MERGE=$(git log --first-parent --format=%h "$BASE..HEAD" | tail -1)
check "the merge commit has it" 'git cat-file -e $MERGE:.github/workflows/new-upstream.yaml'
check "the final tree does not" '[ ! -e .github/workflows/new-upstream.yaml ]'
check "the adaptations commit shows it as deleted, in the diff and in the message" 'git show --name-status --format= HEAD | grep -q "^D.*new-upstream.yaml" && git log -1 --format=%b | grep -q "^D[[:space:]]*.github/workflows/new-upstream.yaml"'

echo "upstream renames an owned file"
new_case renamed 'git mv .github/workflows/scan.yaml .github/workflows/scan-v2.yaml' ':'
run_merge
check "the fork's scan.yaml is back and the renamed file is not kept" 'grep -q "fork codeql-only" .github/workflows/scan.yaml && [ ! -e .github/workflows/scan-v2.yaml ]'

echo "upstream deletes an owned file"
new_case owned-deleted 'git rm -q .github/workflows/scan.yaml' ':'
run_merge
MERGE=$(git log --first-parent --format=%h "$BASE..HEAD" | tail -1)
check "the merge commit lacks it, as upstream released" '! git cat-file -e $MERGE:.github/workflows/scan.yaml 2> /dev/null'
check "the fork's version is back in the end" 'grep -q "fork codeql-only" .github/workflows/scan.yaml'

echo "the fork deleted an owned file and upstream edits it"
new_case fork-deleted 'printf "upstream scan v2\n" > .github/workflows/scan.yaml' 'git rm -q -f .github/workflows/scan.yaml'
run_merge
check "no conflict, and the file stays deleted" 'grep -q "^status=merged" $GITHUB_OUTPUT && [ ! -e .github/workflows/scan.yaml ]'

echo "a conflict in a file the fork does not own"
new_case conflict 'sed -i.bak "s/tail: original/tail: upstream/" crd.yaml; rm crd.yaml.bak' 'sed -i.bak "s/tail: original/tail: fork/" crd.yaml; rm crd.yaml.bak'
run_merge
check "status conflict, markers committed" 'grep -q "^status=conflict" $GITHUB_OUTPUT && git show HEAD:crd.yaml | grep -q "^<<<<<<<"'
check "the conflicting files are listed in a committed file" 'grep -qx crd.yaml .sync-upstream-conflicts && [ -n "$(git ls-files .sync-upstream-conflicts)" ]'

echo "a conflict and a change to an owned file at the same time"
new_case conflict-and-owned 'printf "upstream scan v2\n" > .github/workflows/scan.yaml; sed -i.bak "s/tail: original/tail: upstream/" crd.yaml; rm crd.yaml.bak' 'sed -i.bak "s/tail: original/tail: fork/" crd.yaml; rm crd.yaml.bak'
run_merge
check "only the real conflict is reported, the owned file is resolved" 'grep -q "^status=conflict" $GITHUB_OUTPUT && [ "$(cat .sync-upstream-conflicts)" = crd.yaml ]'
check "the fork-owned file is put back in a commit of its own" '[ "$(git log -1 --format=%s)" = "chore: keep the fork-owned paths after merging v1.0.1" ] && grep -q "fork codeql-only" .github/workflows/scan.yaml'

echo "a conflict while upstream adds a workflow"
new_case conflict-new-workflow 'printf "on: push\n" > .github/workflows/upstream-new.yaml; sed -i.bak "s/tail: original/tail: upstream/" crd.yaml; rm crd.yaml.bak' 'sed -i.bak "s/tail: original/tail: fork/" crd.yaml; rm crd.yaml.bak'
run_merge
check "upstream's workflow is not on the branch that gets pushed" 'grep -q "^status=conflict" $GITHUB_OUTPUT && [ ! -e .github/workflows/upstream-new.yaml ]'

echo "a conflict that leaves no markers: upstream deletes a file the fork modified"
new_case no-markers 'git rm -q other.txt' 'echo "fork edit" >> other.txt'
run_merge
check "status conflict" 'grep -q "^status=conflict" $GITHUB_OUTPUT'
check "there are no markers in the tree" '! git grep -qE "^(<<<<<<<|>>>>>>>) " HEAD'
check "the committed list is what makes CI fail" 'grep -qx other.txt .sync-upstream-conflicts && [ -n "$(git ls-files .sync-upstream-conflicts)" ]'
check "nothing fork-owned changed, so no extra commit" '[ "$(git log -1 --format=%s)" = "Merge upstream tag v1.0.1 (UNRESOLVED CONFLICTS)" ]'

echo "make tidy fails, after already deleting a file (like the Makefile's rm -f go.sum)"
new_case tidy-fails 'echo "new: source.toolkit.fluxcd.io" > new.yaml' 'printf "tidy:\n\trm -f other.txt\n\texit 1\n" > Makefile'
run_merge
check "status merged: the pull request still gets opened" 'grep -q "^status=merged" $GITHUB_OUTPUT'
check "the failure is reported" 'grep -q "^tidy=failed" $GITHUB_OUTPUT'
check "the partial changes of tidy are dropped" '[ -f other.txt ] && [ -z "$(git status --porcelain)" ]'
check "the rest of the adaptations are still committed, and the message says tidy failed" 'grep -q "cd.qdrant.io" new.yaml && git log -1 --format=%b | grep -q "make tidy failed"'

echo "upstream adds a workflow with a non-ASCII name"
new_case non-ascii 'f=$(printf ".github/workflows/sch\303\251ma.yaml"); printf "x\n" > "$f"' ':'
run_merge
check "it is handled like any other path and dropped" 'grep -q "^status=merged" $GITHUB_OUTPUT && [ -z "$(ls .github/workflows | grep -v -e "^scan.yaml$" -e "^ours.yaml$")" ]'

echo
echo "passed: $pass  failed: $fail"
[ "$fail" = 0 ]
