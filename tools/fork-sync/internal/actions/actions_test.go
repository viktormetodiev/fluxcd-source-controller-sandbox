package actions

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetOutput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", file)
	var out bytes.Buffer

	require.NoError(t, SetOutput(&out, "tag", "v1.9.6"))
	require.NoError(t, SetOutput(&out, "status", "merged"))
	require.Error(t, SetOutput(&out, "bad", "two\nlines"))

	assert.Equal(t, "tag=v1.9.6\nstatus=merged\n", out.String())
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "tag=v1.9.6\nstatus=merged\n", string(data))
}

func TestOutsideGitHubActions(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	var out bytes.Buffer
	require.NoError(t, SetOutput(&out, "tag", "v1.9.6"))
	require.NoError(t, AppendSummary("ignored"))
	assert.Equal(t, "tag=v1.9.6\n", out.String())
}
