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

The [`sync-upstream`](.github/workflows/sync-upstream.yaml) workflow opens a PR
when upstream publishes a new stable release (`vX.Y.Z`). It runs on Mondays, or
on demand from the Actions tab (for a given tag, or with `dry_run` to stop before
pushing). The PR, `Sync upstream vX.Y.Z`, has up to two commits:

1. `Merge upstream tag vX.Y.Z`: upstream's changes, merged automatically.
2. `chore: apply Qdrant adaptations ...`, only when needed: what the fork owns,
   put back or regenerated. Its message lists the files.

The workflow runs [`tools/fork-sync`](tools/fork-sync), a small Go tool. What the
fork owns, the commands that adapt upstream's code (the API group rename,
`make tidy`) and the checks after the merge are set in
[`.github/fork-sync.yaml`](.github/fork-sync.yaml).

**Reviewing a sync PR**

- CI is the verdict. Failing builds or tests are fixed on the PR branch.
- With merge conflicts the PR is a draft, with the markers and a list of the
  conflicting files (`.sync-upstream-conflicts`) committed. The
  [`conflict-markers`](.github/workflows/conflict-markers.yaml) check fails until
  the conflicts are resolved and that list is deleted.
- If `make tidy` or the build fails (say, the release needs a newer Go) the PR
  still opens, with a note.
- Merge with a merge commit, never squash or rebase: the upstream tag has to stay
  an ancestor of `main`.
- The workflow never merges or tags. Closing a PR without merging tells it to
  skip that release. The agent's own CI is what checks compatibility with it.

**The fork owns `.github/`.** Upstream's CI needs secrets only Flux has and would
run with this repository's credentials, so it is never activated: when upstream
changes something there, the adaptations commit puts the fork's version back (on
conflicted syncs too). To take an upstream CI change, copy it by hand.

**Token.** The workflow needs a `BOT_GITHUB_TOKEN` secret (not `GITHUB_TOKEN`,
whose PRs do not trigger CI) that can push branches, open PRs and change files
under `.github/workflows/`. Without it, scheduled runs skip with a notice and
manual runs fail with a clear error. Only the publish step receives it.

**Tests.** `go -C tools/fork-sync test ./...` and
`scripts/rename-api-groups_test.sh`, run in CI as `fork-tools-test`.

#### Manual sync

From an up-to-date `main` (needs `go` and `make`):

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
