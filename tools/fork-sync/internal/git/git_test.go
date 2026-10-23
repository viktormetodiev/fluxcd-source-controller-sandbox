package git

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRepo(t *testing.T) Repo {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
	r := Repo{Dir: t.TempDir()}
	_, err := r.Run("init", "-q", "-b", "main")
	require.NoError(t, err)
	return r
}

func write(t *testing.T, r Repo, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(r.Dir, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, path), []byte(content), 0o644))
}

func TestPathsKeepUnusualNames(t *testing.T) {
	r := newRepo(t)
	write(t, r, "schéma.yaml", "x")
	write(t, r, "with space.txt", "x")
	_, err := r.Run("add", "--all")
	require.NoError(t, err)

	paths, err := r.Paths("diff", "--cached", "--name-only", "-z")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"schéma.yaml", "with space.txt"}, paths)
}

func TestNameStatus(t *testing.T) {
	r := newRepo(t)
	write(t, r, "kept", "1")
	write(t, r, "removed", "1")
	_, err := r.Run("add", "--all")
	require.NoError(t, err)
	_, err = r.Run("commit", "-qm", "base")
	require.NoError(t, err)

	write(t, r, "kept", "2")
	write(t, r, "added", "1")
	require.NoError(t, os.Remove(filepath.Join(r.Dir, "removed")))
	_, err = r.Run("add", "--all")
	require.NoError(t, err)

	changes, err := r.NameStatus("--cached")
	require.NoError(t, err)
	assert.ElementsMatch(t, []Change{{"A", "added"}, {"M", "kept"}, {"D", "removed"}}, changes)
}

func TestRunError(t *testing.T) {
	r := newRepo(t)
	_, err := r.Run("rev-parse", "--verify", "no-such-ref")
	require.ErrorContains(t, err, "git rev-parse")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.False(t, r.Succeeds("rev-parse", "--verify", "no-such-ref"))
}

func TestPushWithToken(t *testing.T) {
	const token = "ghp_secretsecretsecret"
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	t.Run("pushes the branch", func(t *testing.T) {
		r := newRepo(t)
		remote := t.TempDir()
		_, err := Repo{Dir: remote}.Run("init", "-q", "--bare")
		require.NoError(t, err)
		_, err = r.Run("remote", "add", "origin", remote)
		require.NoError(t, err)
		_, err = r.Run("commit", "-q", "--allow-empty", "-m", "x")
		require.NoError(t, err)

		require.NoError(t, r.PushWithToken("main", token))
		assert.True(t, Repo{Dir: remote}.Succeeds("rev-parse", "--verify", "refs/heads/main"))
	})

	t.Run("the token never shows up in errors or logs", func(t *testing.T) {
		var log bytes.Buffer
		r := newRepo(t)
		r.Output = &log
		_, err := r.Run("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
		require.NoError(t, err)

		err = r.PushWithToken("main", token)
		require.Error(t, err)
		for _, secret := range []string{token, encoded} {
			assert.NotContains(t, err.Error(), secret)
			assert.NotContains(t, log.String(), secret)
		}
	})
}
