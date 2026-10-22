# Source controller

[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/4786/badge)](https://bestpractices.coreinfrastructure.org/projects/4786)
[![e2e](https://github.com/fluxcd/source-controller/workflows/e2e/badge.svg)](https://github.com/fluxcd/source-controller/actions)
[![report](https://goreportcard.com/badge/github.com/fluxcd/source-controller)](https://goreportcard.com/report/github.com/fluxcd/source-controller)
[![license](https://img.shields.io/github/license/fluxcd/source-controller.svg)](https://github.com/fluxcd/source-controller/blob/main/LICENSE)
[![release](https://img.shields.io/github/release/fluxcd/source-controller/all.svg)](https://github.com/fluxcd/source-controller/releases)

## About this fork

This fork is used by the
[qdrant-cloud-agent](https://github.com/qdrant/qdrant-cloud-agent/). The main
reason behind forking the upstream project is to be able to distribute the CRDs of this
project without risk of collision in case the customer is using Flux CD in their
K8s cluster. The main change between the fork and upstream consist of renaming the
API group names, replacing the ones from fluxcd (ex: `source.toolkit.fluxcd.io`)
by our custom ones.

### Maintaining the fork up to date

#### Automated: the `sync-upstream` workflow

The [`sync-upstream`](.github/workflows/sync-upstream.yaml) workflow runs on
Mondays, or on demand from the Actions tab (optionally for a given tag). When
upstream publishes a new stable release (`vX.Y.Z`), it opens a PR named
`Sync upstream vX.Y.Z` with two commits, so each can be reviewed on its own:

1. `Merge upstream tag vX.Y.Z`: upstream's changes since the last sync, merged
   automatically, with nothing of the fork's added.
2. `chore: apply Qdrant adaptations after merging vX.Y.Z`: what the fork owns,
   on top of it. Its message lists the fork-owned files put back or dropped, and
   how many files `./scripts/rename-api-groups.sh` and `make tidy` changed. It
   is skipped when there is nothing to change.

[`scripts/sync-upstream.sh`](scripts/sync-upstream.sh) does the merge, and you
can run it locally to reproduce what the workflow did.

**Merge these PRs with a merge commit, not squash or rebase.** The upstream tag
has to stay an ancestor of `main`, otherwise the next sync re-applies history
that is already in.

**The fork owns everything under `.github/`** (workflows, CODEOWNERS, Dependabot
config), as listed in [`scripts/fork-owned.txt`](scripts/fork-owned.txt).
Upstream's CI relies on secrets and services that only Flux has, and workflows
run with this repository's credentials, so nothing from upstream's `.github/` is
activated here. When upstream changes something there, the merge commit takes
upstream's version and the adaptations commit puts the fork's back, so nothing
conflicts and that commit shows what the fork did not take. When the merge has
conflicts, the fork's versions are still put back, in a commit of their own, so
upstream's CI never runs on a sync PR. Upstream changed
`.github/` in roughly 1 of 4 recent releases, mostly minors. To take an upstream
CI change, copy it by hand into the fork's own file.

**CI is the verdict.** The PR is always opened. With merge conflicts it is a
draft: the markers are committed together with `.sync-upstream-conflicts`, a
list of the conflicting files (some conflicts, like a file deleted upstream and
modified here, leave no markers). The
[`conflict-markers`](.github/workflows/conflict-markers.yaml) check fails until
the conflicts are resolved and that file is deleted. If it merged cleanly but
does not build or pass the tests, CI is red and someone fixes it on the PR
branch. If `make tidy` fails (for example because the release needs a newer Go
than the fork's tooling), the PR still opens, with a note saying so. The workflow never merges or tags
anything, and closing a PR without merging tells it to leave that release alone.
Compatibility with the cloud-agent is not checked here: the agent's own CI is
the gate once its `replace` lines are bumped to the new fork release.

**Token.** The workflow uses the `BOT_GITHUB_TOKEN` secret, not `GITHUB_TOKEN`:
pushes and PRs made with `GITHUB_TOKEN` do not trigger CI. It must be able to
push branches, open PRs and push changes under `.github/workflows/` (for a
classic PAT the `workflow` scope, for a fine-grained token or a GitHub App the
"Workflows" write permission). Scheduled runs skip quietly, with a notice, until
the secret exists; manual runs fail with a clear error. Only the last step, the
push, receives the token: the checkout does not persist credentials, because
`make tidy` runs upstream's Makefile.

**Tests.** The scripts have offline tests, `scripts/sync-upstream_test.sh` and
`scripts/rename-api-groups_test.sh`. They build tiny upstream and fork
repositories in a temporary directory, need only git, make and bash, and run in
CI (`scripts-test`) on changes under `scripts/`.

**Dry run.** Run the workflow from the Actions tab with `dry_run` enabled to do
everything except pushing the branch and opening the PR. Every run, dry or not,
shows the commits, the build outcome and the PR body in the job summary.

#### Manual

To sync by hand, for example when the workflow cannot run, use the same script on
an up-to-date `main` (it needs `go` and `make` for the tidy step):

```sh
scripts/sync-upstream.sh merge vX.Y.Z
git push origin sync/upstream-vX.Y.Z
```

Then open a PR against `main` and merge it with a merge commit. If the merge has
conflicts, resolve them on that branch and delete `.sync-upstream-conflicts`.
Do not use `-X theirs`: it would silently prefer upstream's side of every
conflict, including the fork's changes.

---

The source-controller is a Kubernetes operator, specialised in artifacts acquisition
from external sources such as Git, OCI, Helm repositories and S3-compatible buckets.
The source-controller implements the
[source.toolkit.fluxcd.io](docs/spec/README.md) API
and is a core component of the [GitOps toolkit](https://fluxcd.io/flux/components/).

![overview](docs/diagrams/source-controller-overview.png)

## APIs

| Kind                                                  | API Version                   |
|-------------------------------------------------------|-------------------------------|
| [GitRepository](docs/spec/v1/gitrepositories.md)      | `source.toolkit.fluxcd.io/v1` |
| [OCIRepository](docs/spec/v1/ocirepositories.md)      | `source.toolkit.fluxcd.io/v1` |
| [HelmRepository](docs/spec/v1/helmrepositories.md)    | `source.toolkit.fluxcd.io/v1` |
| [HelmChart](docs/spec/v1/helmcharts.md)               | `source.toolkit.fluxcd.io/v1` |
| [Bucket](docs/spec/v1/buckets.md)                     | `source.toolkit.fluxcd.io/v1` |
| [ExternalArtifact](docs/spec/v1/externalartifacts.md) | `source.toolkit.fluxcd.io/v1` |

## Features

* authenticates to sources (SSH, user/password, API token, Workload Identity)
* validates source authenticity (PGP, Cosign, Notation)
* detects source changes based on update policies (semver)
* fetches resources on-demand and on-a-schedule
* packages the fetched resources into a well-known format (tar.gz, yaml)
* makes the artifacts addressable by their source identifier (sha, version, ts)
* makes the artifacts available in-cluster to interested 3rd parties
* notifies interested 3rd parties of source changes and availability (status conditions, events, hooks)
* reacts to Git, Helm and OCI artifacts push events (via [notification-controller](https://github.com/fluxcd/notification-controller))

## Guides

* [Get started with Flux](https://fluxcd.io/flux/get-started/)
* [Setup Webhook Receivers](https://fluxcd.io/flux/guides/webhook-receivers/)
* [Setup Notifications](https://fluxcd.io/flux/guides/notifications/)
* [How to build, publish and consume OCI Artifacts with Flux](https://fluxcd.io/flux/cheatsheets/oci-artifacts/)

## Roadmap

The roadmap for the Flux family of projects can be found at <https://fluxcd.io/roadmap/>.

## Contributing

This project is Apache 2.0 licensed and accepts contributions via GitHub pull requests.
To start contributing please see the [development guide](DEVELOPMENT.md).
