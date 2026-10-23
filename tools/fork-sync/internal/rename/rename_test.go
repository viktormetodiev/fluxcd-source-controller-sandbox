package rename

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/config"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
)

// cfg is the rename of the fork's .github/fork-sync.yaml, shortened.
var cfg = config.Rename{
	Files: []string{"*.go", "*.yaml", "*.yml", "docs/*.md", "PROJECT"},
	Replace: []config.Replacement{
		{From: "source.toolkit.fluxcd.io", To: "cd.qdrant.io"},
		{From: "domain: toolkit.fluxcd.io", To: "domain: qdrant.io"},
		{From: "finalizers.fluxcd.io", To: "finalizers.qdrant.io"},
		{From: "group: source", To: "group: cd"},
		{From: "shortName=gitrepo", To: "shortName=qdrantgitrepo"},
	},
	ShortNames: []config.Replacement{
		{From: "gitrepo", To: "qdrantgitrepo"},
		{From: "hc", To: "qdranthc"},
	},
	CRDDir: "config/crd/bases",
}

const upstreamCRD = `spec:
  group: source.toolkit.fluxcd.io
  names:
    kind: GitRepository
    shortNames:
    - gitrepo
    - gitrepository
    singular: gitrepository
`

func TestText(t *testing.T) {
	assert.Equal(t, `spec:
  group: cd.qdrant.io
  names:
    kind: GitRepository
    shortNames:
    - qdrantgitrepo
    - gitrepository
    singular: gitrepository
`, Text(cfg, upstreamCRD), "only whole short name entries are renamed")

	assert.Equal(t, "shortNames:\n- qdrantgitrepo\ncategories:\n- hc\n",
		Text(cfg, "shortNames:\n- gitrepo\ncategories:\n- hc\n"), "the lists after shortNames are left alone")
	assert.Equal(t, "domain: qdrant.io\ngroup: cd\n", Text(cfg, "domain: toolkit.fluxcd.io\ngroup: source\n"))
}

func TestApply(t *testing.T) {
	repo := newRepo(t)
	files := map[string]string{
		"config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml": upstreamCRD,
		"main.go": "// +kubebuilder:resource:shortName=gitrepo\n" +
			"const Finalizer = \"finalizers.fluxcd.io\"\nconst Group = \"source.toolkit.fluxcd.io\"\n",
		"PROJECT":                   "domain: toolkit.fluxcd.io\ngroup: source\n",
		"docs/spec/v1/spec.md":      "The group is source.toolkit.fluxcd.io.\n",
		"README.md":                 "Mentions source.toolkit.fluxcd.io, but is not a renamed file.\n",
		".github/workflows/ci.yaml": "group: source.toolkit.fluxcd.io\n",
		"tools/sync/sync_test.go":   "const upstreamGroup = \"source.toolkit.fluxcd.io\"\n",
		"unchanged.go":              "package unchanged\n",
	}
	for path, content := range files {
		write(t, repo, path, content)
	}
	_, err := repo.Run("add", "--all")
	require.NoError(t, err)
	write(t, repo, "untracked.yaml", "group: source.toolkit.fluxcd.io\n")
	write(t, repo, ".gitignore", "ignored.yaml\n")
	write(t, repo, "ignored.yaml", "group: source.toolkit.fluxcd.io\n")

	written, err := Apply(repo, cfg, []string{".github/", "tools/"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"config/crd/bases/cd.qdrant.io_gitrepositories.yaml",
		"main.go", "PROJECT", "docs/spec/v1/spec.md", "untracked.yaml",
	}, written)

	assert.Equal(t, "// +kubebuilder:resource:shortName=qdrantgitrepo\n"+
		"const Finalizer = \"finalizers.qdrant.io\"\nconst Group = \"cd.qdrant.io\"\n", read(t, repo, "main.go"))
	assert.Equal(t, "domain: qdrant.io\ngroup: cd\n", read(t, repo, "PROJECT"))
	assert.Equal(t, "The group is cd.qdrant.io.\n", read(t, repo, "docs/spec/v1/spec.md"))
	assert.Equal(t, Text(cfg, upstreamCRD), read(t, repo, "config/crd/bases/cd.qdrant.io_gitrepositories.yaml"),
		"the renamed CRD is written under the renamed name")
	for _, path := range []string{
		"config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml", // upstream's CRD stays
		"README.md", ".github/workflows/ci.yaml", "tools/sync/sync_test.go",
	} {
		assert.Equal(t, files[path], read(t, repo, path), path+" is not renamed")
	}
	assert.Contains(t, read(t, repo, "ignored.yaml"), "source.toolkit.fluxcd.io", "ignored files are not renamed")

	written, err = Apply(repo, cfg, []string{".github/", "tools/"})
	require.NoError(t, err)
	assert.Empty(t, written, "a second run changes nothing")
}

func TestApplySkipsDeletedFiles(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "gone.yaml", "group: source.toolkit.fluxcd.io\n")
	_, err := repo.Run("add", "--all")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(repo.Dir, "gone.yaml")))

	written, err := Apply(repo, cfg, nil)
	require.NoError(t, err)
	assert.Empty(t, written)
}

func newRepo(t *testing.T) git.Repo {
	t.Helper()
	for k, v := range map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1"} {
		t.Setenv(k, v)
	}
	repo := git.Repo{Dir: t.TempDir()}
	_, err := repo.Run("init", "-q")
	require.NoError(t, err)
	return repo
}

func write(t *testing.T, repo git.Repo, path, content string) {
	t.Helper()
	file := filepath.Join(repo.Dir, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
}

func read(t *testing.T, repo git.Repo, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Dir, path))
	require.NoError(t, err)
	return string(data)
}
