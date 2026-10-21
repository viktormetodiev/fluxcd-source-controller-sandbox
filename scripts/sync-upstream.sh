#!/usr/bin/env bash
#
# Brings an upstream release of this fork into a new local branch. Pushing it and
# opening the pull request is done by .github/workflows/sync-upstream.yaml.
#
#   sync-upstream.sh target [TAG]   Print the upstream tag to sync (the latest
#                                   stable one by default), or "up-to-date".
#   sync-upstream.sh merge TAG      Create sync/upstream-TAG from HEAD with:
#     1. "Merge upstream tag TAG": upstream exactly as released.
#     2. "chore: apply Qdrant adaptations ...": the fork's version of what
#        upstream changed under the paths in scripts/fork-owned.txt, then the
#        output of scripts/rename-api-groups.sh and of `make tidy`. Skipped when
#        there is nothing to change.
#
# A merge with conflicts is committed with its markers, so that CI fails until
# someone resolves them. The conflicting files are listed in .sync-upstream-conflicts.
#
# Environment: UPSTREAM_URL (default: fluxcd/source-controller), SKIP_TIDY (skip
# `make tidy`), GITHUB_OUTPUT (when set, results are appended to it).

set -euo pipefail

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/fluxcd/source-controller.git}"
UPSTREAM_REFS="refs/upstream-tags"
CONFLICTS_FILE=".sync-upstream-conflicts"
FORK_OWNED_FILE="scripts/fork-owned.txt"

# Upstream stable releases only: vX.Y.Z. This skips pre-releases and the
# "api/vX.Y.Z" tags of the API module.
STABLE_TAG_REGEX='^v[0-9]+\.[0-9]+\.[0-9]+$'

usage() {
    echo "usage: $0 target [TAG] | merge TAG" >&2
    exit 2
}

output() {
    echo "$1=$2"
    if [ -n "${GITHUB_OUTPUT:-}" ]; then
        echo "$1=$2" >> "$GITHUB_OUTPUT"
    fi
}

count_lines() {
    printf '%s\n' "$1" | grep -c .
}

fetch_upstream_tags() {
    # Upstream tags go into their own namespace so they cannot clash with the
    # fork's own tags (vX.Y.Z-qdrant.N).
    git fetch --quiet --no-tags "$UPSTREAM_URL" "+refs/tags/*:${UPSTREAM_REFS}/*"
}

latest_stable_tag() {
    git for-each-ref --format='%(refname:strip=2)' "${UPSTREAM_REFS}/" \
        | grep -E "$STABLE_TAG_REGEX" \
        | sort -V \
        | tail -n 1
}

validate_tag() {
    if ! [[ $1 =~ $STABLE_TAG_REGEX ]]; then
        echo "error: '$1' is not a stable upstream release tag (expected vX.Y.Z)" >&2
        exit 2
    fi
    if ! git rev-parse --verify --quiet "${UPSTREAM_REFS}/$1" > /dev/null; then
        echo "error: upstream tag '$1' not found" >&2
        exit 2
    fi
}

# Files under the paths in scripts/fork-owned.txt (git pathspecs: files or
# directories) that upstream added, changed, deleted or renamed between the
# merge base and the given ref, one per line. --no-renames lists both the old and
# the new name of a rename.
fork_owned_changed_upstream() {
    local merge_base=$1 ref=$2 spec
    [ -f "$FORK_OWNED_FILE" ] || return 0
    {
        while IFS= read -r spec; do
            git diff --name-only --no-renames "$merge_base" "$ref" -- "$spec"
        done < <(grep -vE '^[[:space:]]*(#|$)' "$FORK_OWNED_FILE" || true)
    } | sort -u
}

# Make the working tree and index have each of the given paths (one per line) as
# they are in <ref>, or not have them at all if <ref> does not.
take_paths_from() {
    local ref=$1 paths=$2 path
    while IFS= read -r path; do
        if [ -z "$path" ]; then continue; fi
        if git cat-file -e "${ref}:${path}" 2> /dev/null; then
            git checkout "$ref" -- "$path"
        else
            git rm -q -f --ignore-unmatch -- "$path"
        fi
    done <<< "$paths"
}

# Names of the staged files that are not in the given newline-separated list.
staged_files_not_in() {
    git diff --cached --name-only --no-renames | sort | comm -23 - <(printf '%s\n' "$1" | sort)
}

# Merge <ref> without committing, with the fork-owned files set to upstream's
# version, so the merge commit is upstream exactly as released. Fails when real
# conflicts remain.
merge_as_released() {
    local ref=$1 owned=$2
    git merge --no-ff --no-commit "$ref" || true
    if ! git rev-parse --quiet --verify MERGE_HEAD > /dev/null; then
        echo "error: the merge of ${ref} did not start" >&2
        exit 1
    fi
    take_paths_from "$ref" "$owned"
    [ -z "$(git diff --name-only --diff-filter=U)" ]
}

# Commit the unresolved merge as it is, markers included: the pull request then
# shows exactly what needs resolving, and CI cannot go green until someone does.
commit_conflicts() {
    local tag=$1
    git diff --name-only --diff-filter=U > "$CONFLICTS_FILE"
    git add --all -- . ":(exclude)${CONFLICTS_FILE}"
    git commit --quiet --no-verify -m "Merge upstream tag ${tag} (UNRESOLVED CONFLICTS)"
}

# Everything the fork owns, on top of the merge, in a single commit whose message
# says what each part did, so the pull request can be judged from it.
commit_adaptations() {
    local tag=$1 base=$2 owned=$3
    local restored_status restored_names renamed_names tidied_names body

    take_paths_from "$base" "$owned"
    restored_status=$(git diff --cached --name-status --no-renames)
    restored_names=$(git diff --cached --name-only --no-renames)

    ./scripts/rename-api-groups.sh > /dev/null
    git add --all
    renamed_names=$(staged_files_not_in "$restored_names")

    if [ -z "${SKIP_TIDY:-}" ]; then
        # The project's own target: tidies the root and the api/ module.
        make tidy
        git add --all
    fi
    tidied_names=$(staged_files_not_in "${restored_names}"$'\n'"${renamed_names}")

    if git diff --cached --quiet; then
        return 0
    fi
    body="Reapplies what the fork owns on top of the upstream merge."
    if [ -n "$restored_status" ]; then
        body+=$'\n\n'"Fork-owned paths (${FORK_OWNED_FILE}), put back as the fork has them (status is relative to the merge commit):"$'\n'"${restored_status}"
    fi
    if [ -n "$renamed_names" ]; then
        body+=$'\n\n'"scripts/rename-api-groups.sh changed $(count_lines "$renamed_names") file(s)."
    fi
    if [ -n "$tidied_names" ]; then
        body+=$'\n\n'"make tidy changed $(count_lines "$tidied_names") file(s)."
    fi
    git commit --quiet -m "chore: apply Qdrant adaptations after merging ${tag}" -m "$body"
}

cmd_target() {
    fetch_upstream_tags
    local tag=${1:-$(latest_stable_tag)}
    if [ -z "$tag" ]; then
        echo "error: no stable upstream release tags found at $UPSTREAM_URL" >&2
        exit 1
    fi
    validate_tag "$tag"

    output tag "$tag"
    if git merge-base --is-ancestor "${UPSTREAM_REFS}/${tag}" HEAD; then
        output status up-to-date
    else
        output status needs-sync
        output branch "sync/upstream-${tag}"
    fi
}

cmd_merge() {
    local tag=${1:?usage: sync-upstream.sh merge TAG}
    fetch_upstream_tags
    validate_tag "$tag"

    local ref="${UPSTREAM_REFS}/${tag}"
    local branch="sync/upstream-${tag}"
    local base owned
    base=$(git rev-parse HEAD)
    owned=$(fork_owned_changed_upstream "$(git merge-base HEAD "$ref")" "$ref")

    git checkout --quiet -b "$branch"
    output tag "$tag"
    output branch "$branch"
    output base "$base"

    if ! merge_as_released "$ref" "$owned"; then
        commit_conflicts "$tag"
        output status conflict
        output conflicts_file "$CONFLICTS_FILE"
        return 0
    fi
    git commit --quiet -m "Merge upstream tag ${tag}"
    commit_adaptations "$tag" "$base" "$owned"
    output status merged
}

case "${1:-}" in
    target) shift; cmd_target "$@" ;;
    merge)  shift; cmd_merge "$@" ;;
    *)      usage ;;
esac
