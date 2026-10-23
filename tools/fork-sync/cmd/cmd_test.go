package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/config"
)

// fakeGitHub answers the pull request API calls of target and publish.
type fakeGitHub struct {
	mu      sync.Mutex
	created []map[string]any
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		_, _ = w.Write([]byte(`[]`))
	case http.MethodPost:
		var pr map[string]any
		_ = json.NewDecoder(r.Body).Decode(&pr)
		f.created = append(f.created, pr)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url": "https://github.com/qdrant/fork/pull/1"}`))
	}
}

// TestTargetMergePublish runs the commands the way the workflow does, against
// local repositories and a fake GitHub API.
func TestTargetMergePublish(t *testing.T) {
	dir := t.TempDir()
	fork := filepath.Join(dir, "fork")
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
	sh(t, dir, `git init -q -b main up && cd up
echo v1 > a.txt && git add -A && git commit -qm v1.0.0 && git tag v1.0.0
echo v2 > a.txt && git commit -qam v1.0.1 && git tag v1.0.1
cd .. && git clone -q --bare up upstream.git && git init -q --bare origin.git
git clone -q upstream.git fork 2>/dev/null && cd fork && git checkout -q -B main v1.0.0
mkdir -p .github && printf 'upstream: %s\n' "$PWD/../upstream.git" > `+config.DefaultPath+`
git add -A && git commit -qm fork
git remote set-url origin "$PWD/../origin.git" && git push -q origin main`)

	api := &fakeGitHub{}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	t.Setenv("GH_TOKEN", "token")
	t.Setenv("GITHUB_REPOSITORY", "qdrant/fork")
	t.Setenv("GITHUB_API_URL", server.URL+"/")
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(dir, "summary.md"))
	body := filepath.Join(dir, "pr-body.md")

	out := run(t, "target", "--repo", fork, "--check-existing")
	assert.Contains(t, out, "tag=v1.0.1\n")
	assert.Contains(t, out, "branch=sync/upstream-v1.0.1\n")
	assert.Contains(t, out, "status=needs-sync\n")

	out = run(t, "merge", "v1.0.1", "--repo", fork, "--body-file", body, "--dry-run")
	assert.Contains(t, out, "status=merged\n")
	summary, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	require.NoError(t, err)
	assert.Contains(t, string(summary), "Dry run: nothing was pushed")

	out = run(t, "publish", "--repo", fork, "--branch", "sync/upstream-v1.0.1", "--body-file", body)
	assert.Contains(t, out, "url=https://github.com/qdrant/fork/pull/1\n")
	assert.Contains(t, out, "Opened the pull request", "logs go to stderr")
	sh(t, dir, `git --git-dir=origin.git rev-parse --verify -q refs/heads/sync/upstream-v1.0.1 > /dev/null`)
	wantBody, err := os.ReadFile(body)
	require.NoError(t, err)
	require.Len(t, api.created, 1)
	assert.Equal(t, map[string]any{
		"title": "Sync upstream v1.0.1",
		"head":  "sync/upstream-v1.0.1",
		"base":  "main",
		"body":  string(wantBody),
		"draft": false,
	}, api.created[0])

	sh(t, fork, `git checkout -q main`)
	out = run(t, "target", "--repo", fork, "--check-existing")
	assert.Contains(t, out, "status=exists\n", "the sync branch is on origin now")
}

func TestRejectsUnknownLogLevels(t *testing.T) {
	_, err := execute("target", "--repo", t.TempDir(), "--log-level", "loud")
	require.ErrorContains(t, err, "--log-level")
}

func TestRename(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	sh(t, repo, `git init -q && mkdir .github
printf 'upstream: x\n' > .github/without-rename.yaml
printf 'upstream: x\nrename:\n  files: ["*.go"]\n  replace: [{from: fluxcd.io, to: qdrant.io}]\n' > `+config.DefaultPath+`
echo 'const group = "fluxcd.io"' > group.go
echo 'const other = 1' > other.go`)

	out := run(t, "rename", "--repo", repo, "--log-level", "warn")
	assert.Equal(t, "group.go\n", out, "prints the files it wrote")
	data, err := os.ReadFile(filepath.Join(repo, "group.go"))
	require.NoError(t, err)
	assert.Equal(t, "const group = \"qdrant.io\"\n", string(data))

	_, err = execute("rename", "--repo", repo, "--config", ".github/without-rename.yaml")
	require.ErrorContains(t, err, "there is no rename")
}

func TestPublishRejectsOtherBranches(t *testing.T) {
	_, err := execute("publish", "--branch", "main", "--body-file", "x")
	require.ErrorContains(t, err, "must be a sync branch")
}

// run executes the command line and returns what it printed.
func run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execute(args...)
	require.NoError(t, err, out)
	return out
}

func execute(args ...string) (string, error) {
	// Flags keep their values between executions: start from the defaults.
	repoDir, configPath, logLevel = ".", config.DefaultPath, "info"
	targetOpts.tag, targetOpts.checkExisting = "", false
	mergeOpts.bodyFile, mergeOpts.dryRun = "", false
	publishOpts.branch, publishOpts.bodyFile = "", ""

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

func sh(t *testing.T, dir, script string) {
	t.Helper()
	cmd := exec.Command("bash", "-ec", script)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s\n%s", script, strings.TrimSpace(string(output)))
}
