Merges upstream [`{{ .Tag }}`]({{ .UpstreamURL }}/releases/tag/{{ .Tag }}) into the fork and re-applies what the fork owns.

Opened automatically by the `sync-upstream` workflow. **CI is the verdict**: if it is red, this release needs work before it can be taken.

### Reviewing

- The first commit, "Merge upstream tag", only has upstream's changes, merged automatically. There is little to review there, but an automatic merge can still be wrong in ways git does not notice: CI is what catches those.
- The "Apply Qdrant adaptations" commit, when there is one, says what the fork put back, dropped or regenerated. Review that and CI.
- **Merge with a merge commit, not squash or rebase**, so the upstream tag stays an ancestor of `main`.

### After merging

- [ ] Tag the fork release: `{{ .Tag }}-qdrant.1` and `api/{{ .Tag }}-qdrant.1` (bump the number for a re-release of the same upstream version).
- [ ] fluxcd-helm-controller: update its `source-controller/api` replace and `SOURCE_BRANCH` in the Makefile to the new tag.
- [ ] qdrant-cloud-agent: bump the `replace` lines in `go.mod`, run `make download-crd-deps` and review the CRD diff (see docs/upgrading-flux-controller-crds.md).
