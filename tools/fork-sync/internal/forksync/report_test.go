package forksync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
)

func TestAdaptationsMessage(t *testing.T) {
	msg := adaptationsMessage(Result{
		Restored: []git.Change{{Status: "M", Path: ".github/workflows/scan.yaml"}, {Status: "D", Path: ".github/workflows/new.yaml"}},
		Adaptations: []CommandResult{
			{Name: "rename API groups", Changed: 2},
			{Name: "make tidy", Failed: true},
			{Name: "nothing to do"},
		},
	})
	assert.Equal(t, `Reapplies what the fork owns on top of the upstream merge.

Fork-owned paths, put back as the fork has them (status is relative to the merge commit):
M	.github/workflows/scan.yaml
D	.github/workflows/new.yaml

rename API groups changed 2 file(s).

make tidy failed, so its changes are not included.`, msg)
}

func TestPRBody(t *testing.T) {
	t.Run("a clean merge is just the template", func(t *testing.T) {
		body, err := PRBody(DefaultPRBodyTemplate, Result{Tag: "v1.9.6"}, "https://github.com/fluxcd/source-controller.git")
		require.NoError(t, err)
		assert.Equal(t, "Merges upstream [`v1.9.6`](https://github.com/fluxcd/source-controller/releases/tag/v1.9.6) into the fork.\n", body)
	})

	t.Run("what needs attention comes first", func(t *testing.T) {
		body, err := PRBody("TEMPLATE {{ .Tag }}\n", Result{
			Tag:         "v1.9.6",
			Conflicts:   []string{"a.go", "b.yaml"},
			Adaptations: []CommandResult{{Name: "make tidy", Failed: true}},
			Checks:      []CommandResult{{Name: "build", Failed: true}, {Name: "vet"}},
		}, "x")
		require.NoError(t, err)
		assert.Contains(t, body, "**`make tidy` failed**")
		assert.Contains(t, body, "**Check `build` failed**")
		assert.NotContains(t, body, "vet")
		assert.Contains(t, body, "> - `a.go`\n> - `b.yaml`\n")
		assert.Contains(t, body, ConflictsFile)
		assert.True(t, strings.HasSuffix(body, "TEMPLATE v1.9.6\n"), "the template comes last")
	})

	t.Run("a template using an unknown field is an error", func(t *testing.T) {
		_, err := PRBody("{{ .Nope }}", Result{}, "x")
		require.Error(t, err)
	})
}

func TestSummary(t *testing.T) {
	r := Result{Tag: "v1.9.6", Status: StatusMerged, Checks: []CommandResult{{Name: "build"}}}
	s := Summary(r, "abc Merge upstream tag v1.9.6\n", "BODY", true)
	assert.Contains(t, s, "## Sync upstream v1.9.6")
	assert.Contains(t, s, "Merge status: **merged**. Check build: **passed**.")
	assert.Contains(t, s, "Dry run: nothing was pushed")
	assert.Contains(t, s, "abc Merge upstream tag v1.9.6")
	assert.True(t, strings.HasSuffix(s, "BODY"))
	assert.NotContains(t, Summary(r, "", "", false), "Dry run")
}
