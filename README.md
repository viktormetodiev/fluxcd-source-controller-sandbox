# Source controller

[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/4786/badge)](https://bestpractices.coreinfrastructure.org/projects/4786)
[![e2e](https://github.com/fluxcd/source-controller/workflows/e2e/badge.svg)](https://github.com/fluxcd/source-controller/actions)
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

The [`sync-upstream`](.github/workflows/sync-upstream.yaml) workflow opens a PR
when upstream publishes a new stable release (`vX.Y.Z`). It runs on Mondays, or
on demand from the Actions tab (for a given tag, or with `dry_run` to stop before
pushing). The PR, `Sync upstream vX.Y.Z`, has up to two commits:

1. `Merge upstream tag vX.Y.Z`: upstream's changes, merged automatically.
2. `chore: apply Qdrant adaptations ...`, only when needed: what the fork owns,
   put back or regenerated. Its message lists the files.

The workflow runs [`tools/fork-sync`](tools/fork-sync), a small Go tool. What the
fork owns, the API group rename and the commands that adapt upstream's code
(`make tidy`) are set in [`.github/fork-sync.yaml`](.github/fork-sync.yaml).

**Reviewing a sync PR**

- CI is the verdict. Failing builds or tests are fixed on the PR branch.
- With merge conflicts the PR is a draft, with the markers and a list of the
  conflicting files (`.sync-upstream-conflicts`) committed. The
  [`conflict-markers`](.github/workflows/conflict-markers.yaml) check fails until
  the conflicts are resolved and that list is deleted. The rename and the
  adaptations do not run on a conflicted sync: once the conflicts are resolved,
  run `go -C tools/fork-sync run . --repo ../.. rename`, then the commands under
  `adaptations` in `.github/fork-sync.yaml`.
- If `make tidy` fails (say, the release needs a newer Go) the PR still opens,
  with a note.
- Merge with a merge commit, never squash or rebase: the upstream tag has to stay
  an ancestor of `main`.
- The workflow never merges or tags. Closing a PR without merging tells it to
  skip that release. The agent's own CI is what checks compatibility with it.

**Porting the adapter.** [`adapter/adapter.go`](adapter/adapter.go) is the fork's
own code: it adds the source reconcilers to `qdrant-cloud-agent`'s controller
manager, which upstream's `main.go` cannot do (`package main` cannot be
imported). It sets them up the way `main.go` does, so when a release changes
that setup, the `adapter` check fails on the sync PR. Make the same change
upstream made in `main.go`, on the sync branch:

```sh
git fetch --no-tags https://github.com/fluxcd/source-controller.git "+refs/tags/*:refs/upstream-tags/*"
git diff refs/upstream-tags/v1.9.3 refs/upstream-tags/v1.10.1 -- main.go   # the last synced and the new release
```

For example, v1.10 passes the manager to `events.NewRecorder` as an option
(`events.WithManager(mgr)`) and renames the reconcilers' `EventRecorder` field
to `Recorder`.

**The fork owns `.github/`,** because the sync PR's CI runs the workflows with
this repository's secrets before anyone reviews it. Upstream's changes there are
put back to the fork's version in the adaptations commit (on conflicted syncs
too), and a sync stops if an adaptation changes a workflow.

**Taking an upstream CI change.** When upstream changed workflows the fork
has, the sync PR lists them with the command that applies upstream's changes on
top of the fork's version. Run it on the sync branch when CI needs them (a newer
Go, say), leaving out the files you do not want. By hand, between the last
synced and the new release:

```sh
git fetch --no-tags https://github.com/fluxcd/source-controller.git "+refs/tags/*:refs/upstream-tags/*"
git diff refs/upstream-tags/vOLD refs/upstream-tags/vNEW -- .github/workflows/test.yaml | git apply -3
```

**Credentials.** The workflow pushes and opens PRs as the
[`qdrant-flux-fork-sync`](https://github.com/apps/qdrant-flux-fork-sync) app,
installed only on the Flux forks (not `GITHUB_TOKEN`, whose PRs do not trigger CI
and which cannot push the workflow changes that come with upstream's commits).
Its client ID and private key are the `FORK_SYNC_APP_CLIENT_ID` and
`FORK_SYNC_APP_PRIVATE_KEY` secrets. The workflow has two jobs, so that
upstream's code never runs where they are: `sync` merges and runs upstream's
code, with no credentials, and `publish` pushes and opens the PR from a fresh
runner, with a token that lasts for the job.

**Tests.** `go -C tools/fork-sync test ./...`, run in CI as `fork-tools-test`.
[fluxcd-helm-controller](https://github.com/qdrant/fluxcd-helm-controller) has a
copy of the tool: fixes go to both.

#### Manual sync

From an up-to-date `main` with no local changes (needs `go` and `make`):

```sh
go -C tools/fork-sync run . --repo ../.. merge vX.Y.Z
git push origin sync/upstream-vX.Y.Z
```

Open a PR against `main` and merge it with a merge commit. Resolve conflicts on
that branch and delete `.sync-upstream-conflicts`. Do not use `-X theirs`, it
silently prefers upstream's side of every conflict.

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
