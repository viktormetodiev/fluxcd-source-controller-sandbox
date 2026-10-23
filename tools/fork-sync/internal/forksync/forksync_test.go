package forksync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/config"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
)

// fixture is a tiny upstream repository and a fork of it. Upstream v1.0.0 has a
// workflow (scan.yaml), crd.yaml and other.txt. Upstream v1.0.1 is v1.0.0 plus
// the changes given to newFixture. The fork is v1.0.0 plus the rename of
// crd.yaml, its own scan.yaml and ours.yaml under the owned .github/, its sync
// configuration (a rename of the *.yaml files, make tidy), and the fork changes
// given to newFixture.
type fixture struct {
	t      *testing.T
	fork   string
	base   string
	syncer Syncer
}

func newFixture(t *testing.T, upstreamChanges, forkChanges string) *fixture {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	upstream := filepath.Join(dir, "up")
	bare := filepath.Join(dir, "upstream.git")
	fork := filepath.Join(dir, "fork")

	sh(t, dir, `git init -q -b main up && cd up && mkdir -p .github/workflows
printf 'upstream scan v1\nfossa\n' > .github/workflows/scan.yaml
{ echo 'group: source.toolkit.fluxcd.io'; for i in 1 2 3 4 5 6 7 8; do echo "filler $i"; done; echo 'tail: original'; } > crd.yaml
echo other > other.txt
git add -A && git commit -qm v1.0.0 && git tag v1.0.0`)
	sh(t, upstream, upstreamChanges)
	sh(t, upstream, `git add -A && git commit -qm v1.0.1 && git tag v1.0.1
git tag v1.0.2-rc.1   # a pre-release, which must be ignored
git tag api/v1.0.1    # the tag of the api/ module, which must be ignored`)
	sh(t, dir, `git clone -q --bare up upstream.git && git clone -q upstream.git fork 2>/dev/null`)

	writeFile(t, filepath.Join(fork, "Makefile"), "tidy:\n\t@true\n")
	writeFile(t, filepath.Join(fork, config.DefaultPath), "upstream: "+bare+`
ownedPaths:
  - .github/
rename:
  files: ["*.yaml"]
  replace:
    - {from: source.toolkit.fluxcd.io, to: cd.qdrant.io}
adaptations:
  - name: make tidy
    run: [make, tidy]
    allowFailure: true
`)
	sh(t, fork, `git checkout -q -B main v1.0.0
sed -i.bak 's/source.toolkit.fluxcd.io/cd.qdrant.io/' crd.yaml && rm crd.yaml.bak
printf 'fork codeql-only scan\n' > .github/workflows/scan.yaml
printf 'fork ci\n' > .github/workflows/ours.yaml`)
	sh(t, fork, forkChanges)
	sh(t, fork, `git add -A && git commit -qm fork`)

	cfg, err := config.Load(fork, config.DefaultPath)
	require.NoError(t, err)
	f := &fixture{t: t, fork: fork, syncer: Syncer{Repo: git.Repo{Dir: fork}, Config: cfg, Logger: testr.New(t)}}
	f.base = strings.TrimSpace(f.git("rev-parse", "HEAD"))
	return f
}

func (f *fixture) merge() Result {
	f.t.Helper()
	res, err := f.syncer.Merge("v1.0.1")
	require.NoError(f.t, err)
	return res
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	out, err := f.syncer.Repo.Run(args...)
	require.NoError(f.t, err)
	return out
}

func (f *fixture) gitSucceeds(args ...string) bool {
	return f.syncer.Repo.Succeeds(args...)
}

// subjects returns the subjects of the commits on the sync branch, oldest first.
func (f *fixture) subjects() []string {
	f.t.Helper()
	return strings.Split(strings.TrimSpace(f.git("log", "--reverse", "--first-parent", "--format=%s", f.base+"..HEAD")), "\n")
}

func (f *fixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.fork, path))
	require.NoError(f.t, err)
	return string(data)
}

func (f *fixture) exists(path string) bool {
	_, err := os.Stat(filepath.Join(f.fork, path))
	return err == nil
}

func TestFindTarget(t *testing.T) {
	f := newFixture(t, `echo "other v2" > other.txt`, ``)

	target, err := f.syncer.FindTarget("")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.1", target.Tag, "the latest stable tag, ignoring the pre-release and the api/ tag")
	assert.Equal(t, "sync/upstream-v1.0.1", target.Branch())
	assert.False(t, target.UpToDate)

	f.merge()
	target, err = f.syncer.FindTarget("")
	require.NoError(t, err)
	assert.True(t, target.UpToDate, "up to date once the tag is merged")

	_, err = f.syncer.FindTarget("v1.0.1;x")
	require.ErrorIs(t, err, ErrInvalidTag)
	_, err = f.syncer.FindTarget("v1.0.2-rc.1")
	require.ErrorIs(t, err, ErrInvalidTag)
	_, err = f.syncer.FindTarget("v9.9.9")
	require.ErrorContains(t, err, "not found")
}

func TestMerge(t *testing.T) {
	t.Run("upstream edits an owned file, adds a file with the old group name, edits another file", func(t *testing.T) {
		f := newFixture(t, `printf 'upstream scan v2\nfossa\n' > .github/workflows/scan.yaml
echo "new: source.toolkit.fluxcd.io" > new.yaml
echo "other v2" > other.txt`, ``)
		res := f.merge()

		assert.Equal(t, StatusMerged, res.Status)
		assert.Equal(t, []string{"Merge upstream tag v1.0.1", "chore: apply Qdrant adaptations after merging v1.0.1"}, f.subjects())
		assert.Contains(t, f.git("show", "HEAD~1:.github/workflows/scan.yaml"), "upstream scan v2", "the merge commit has upstream's version")
		assert.Contains(t, f.git("show", "HEAD~1:new.yaml"), "source.toolkit.fluxcd.io", "the merge commit has upstream's changes only")
		assert.Contains(t, f.read(".github/workflows/scan.yaml"), "fork codeql-only")
		assert.Contains(t, f.read("new.yaml"), "cd.qdrant.io")
		assert.Contains(t, f.read("other.txt"), "other v2")
		assert.Contains(t, f.read(".github/workflows/ours.yaml"), "fork ci")
		assert.NotContains(t, f.git("show", "--name-only", "--format=", "HEAD"), "ours.yaml")

		body := f.git("log", "-1", "--format=%b")
		assert.Contains(t, body, "M\t.github/workflows/scan.yaml")
		assert.Contains(t, body, "rename changed 1 file(s).")
		assert.NotContains(t, body, "make tidy", "tidy changed nothing, so it is not mentioned")
	})

	t.Run("upstream changes nothing the fork owns, nothing to adapt", func(t *testing.T) {
		f := newFixture(t, `echo "other v2" > other.txt`, ``)
		res := f.merge()
		assert.Equal(t, StatusMerged, res.Status)
		assert.Equal(t, []string{"Merge upstream tag v1.0.1"}, f.subjects(), "no empty adaptations commit")
		assert.Contains(t, f.read(".github/workflows/scan.yaml"), "fork codeql-only")
	})

	t.Run("upstream adds a workflow", func(t *testing.T) {
		f := newFixture(t, `printf 'on: push\n' > .github/workflows/upstream-new.yaml`, ``)
		f.merge()
		assert.True(t, f.gitSucceeds("cat-file", "-e", "HEAD~1:.github/workflows/upstream-new.yaml"), "the merge commit has it")
		assert.False(t, f.exists(".github/workflows/upstream-new.yaml"), "the branch does not")
		assert.Contains(t, f.git("log", "-1", "--format=%b"), "D\t.github/workflows/upstream-new.yaml")
	})

	t.Run("upstream renames an owned file", func(t *testing.T) {
		f := newFixture(t, `git mv .github/workflows/scan.yaml .github/workflows/scan-v2.yaml`, ``)
		f.merge()
		assert.Contains(t, f.read(".github/workflows/scan.yaml"), "fork codeql-only")
		assert.False(t, f.exists(".github/workflows/scan-v2.yaml"))
	})

	t.Run("upstream deletes an owned file", func(t *testing.T) {
		f := newFixture(t, `git rm -q .github/workflows/scan.yaml`, ``)
		f.merge()
		assert.False(t, f.gitSucceeds("cat-file", "-e", "HEAD~1:.github/workflows/scan.yaml"), "the merge commit lacks it")
		assert.Contains(t, f.read(".github/workflows/scan.yaml"), "fork codeql-only", "the fork's version is back")
	})

	t.Run("the fork deleted an owned file and upstream edits it", func(t *testing.T) {
		f := newFixture(t, `printf 'upstream scan v2\n' > .github/workflows/scan.yaml`, `git rm -q -f .github/workflows/scan.yaml`)
		res := f.merge()
		assert.Equal(t, StatusMerged, res.Status, "no modify/delete conflict")
		assert.False(t, f.exists(".github/workflows/scan.yaml"))
	})

	t.Run("upstream adds a workflow with a non-ASCII name", func(t *testing.T) {
		f := newFixture(t, `printf 'x\n' > "$(printf '.github/workflows/sch\303\251ma.yaml')"`, ``)
		res := f.merge()
		assert.Equal(t, StatusMerged, res.Status)
		entries, err := os.ReadDir(filepath.Join(f.fork, ".github", "workflows"))
		require.NoError(t, err)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		assert.ElementsMatch(t, []string{"ours.yaml", "scan.yaml"}, names)
	})
}

func TestMergeConflicts(t *testing.T) {
	const upstreamTail = `sed -i.bak 's/tail: original/tail: upstream/' crd.yaml && rm crd.yaml.bak`
	const forkTail = `sed -i.bak 's/tail: original/tail: fork/' crd.yaml && rm crd.yaml.bak`

	t.Run("in a file the fork does not own", func(t *testing.T) {
		f := newFixture(t, upstreamTail, forkTail)
		res := f.merge()
		assert.Equal(t, StatusConflict, res.Status)
		assert.Equal(t, []string{"crd.yaml"}, res.Conflicts)
		assert.Contains(t, f.git("show", "HEAD:crd.yaml"), "<<<<<<<", "markers are committed")
		assert.Equal(t, "crd.yaml\n", f.git("show", "HEAD:"+ConflictsFile), "the list is committed")
		assert.Equal(t, []string{"Merge upstream tag v1.0.1 (UNRESOLVED CONFLICTS)"}, f.subjects(), "nothing owned changed, no extra commit")
	})

	t.Run("together with a change to an owned file", func(t *testing.T) {
		f := newFixture(t, `printf 'upstream scan v2\n' > .github/workflows/scan.yaml
`+upstreamTail, forkTail)
		res := f.merge()
		assert.Equal(t, []string{"crd.yaml"}, res.Conflicts, "only the real conflict is reported")
		assert.Equal(t, "chore: keep the fork-owned paths after merging v1.0.1", f.subjects()[len(f.subjects())-1])
		assert.Contains(t, f.read(".github/workflows/scan.yaml"), "fork codeql-only")
	})

	t.Run("while upstream adds a workflow", func(t *testing.T) {
		f := newFixture(t, `printf 'on: push\n' > .github/workflows/upstream-new.yaml
`+upstreamTail, forkTail)
		f.merge()
		assert.False(t, f.exists(".github/workflows/upstream-new.yaml"), "upstream's CI is never on the branch that gets pushed")
	})

	t.Run("without markers: upstream deletes a file the fork modified", func(t *testing.T) {
		f := newFixture(t, `git rm -q other.txt`, `echo "fork edit" >> other.txt`)
		res := f.merge()
		assert.Equal(t, StatusConflict, res.Status)
		assert.False(t, f.gitSucceeds("grep", "-qE", "^(<<<<<<<|>>>>>>>) ", "HEAD"), "no markers in the tree")
		assert.Equal(t, "other.txt\n", f.git("show", "HEAD:"+ConflictsFile), "the committed list is what makes CI fail")
	})
}

func TestMergeAdaptationFailures(t *testing.T) {
	t.Run("an adaptation allowed to fail drops its changes, the rest is kept", func(t *testing.T) {
		// Like the Makefile's "rm -f go.sum" before go mod tidy fails.
		f := newFixture(t, `echo "new: source.toolkit.fluxcd.io" > new.yaml`,
			`printf 'tidy:\n\trm -f other.txt\n\ttouch leftover.txt\n\texit 1\n' > Makefile`)
		res := f.merge()
		assert.Equal(t, StatusMerged, res.Status, "the pull request still gets opened")
		assert.Equal(t, []string{"make tidy"}, res.FailedAdaptations())
		assert.True(t, f.exists("other.txt"), "the deleted file is back")
		assert.False(t, f.exists("leftover.txt"), "the file it created is gone")
		assert.Empty(t, strings.TrimSpace(f.git("status", "--porcelain")))
		assert.Contains(t, f.read("new.yaml"), "cd.qdrant.io", "the other adaptations are kept")
		assert.Contains(t, f.git("log", "-1", "--format=%b"), "make tidy failed, so its changes are not included.")
	})

	t.Run("an adaptation not allowed to fail stops the sync", func(t *testing.T) {
		f := newFixture(t, `echo "other v2" > other.txt`, ``)
		f.syncer.Config.Adaptations = append(f.syncer.Config.Adaptations, config.Adaptation{Name: "generate", Run: []string{"false"}})
		_, err := f.syncer.Merge("v1.0.1")
		require.ErrorContains(t, err, `adaptation "generate"`)
		var exitErr *exec.ExitError
		assert.ErrorAs(t, err, &exitErr)
	})
}

func isolateGit(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
}

func sh(t *testing.T, dir, script string) {
	t.Helper()
	if strings.TrimSpace(script) == "" {
		return
	}
	cmd := exec.Command("bash", "-ec", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s\n%s", script, out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
