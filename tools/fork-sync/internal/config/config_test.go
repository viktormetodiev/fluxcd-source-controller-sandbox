package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func load(t *testing.T, content string) (Config, error) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, DefaultPath), []byte(content), 0o644))
	return Load(dir, DefaultPath)
}

func TestLoad(t *testing.T) {
	cfg, err := load(t, `
upstream: https://github.com/fluxcd/source-controller.git
ownedPaths: [.github/]
adaptations:
  - name: make tidy
    run: [make, tidy]
    allowFailure: true
checks:
  - name: build
    run: [go, build, ./...]
prBodyTemplate: .github/sync-upstream-pr-body.md
`)
	require.NoError(t, err)
	assert.Equal(t, Config{
		Upstream:       "https://github.com/fluxcd/source-controller.git",
		BaseBranch:     "main",
		OwnedPaths:     []string{".github/"},
		Adaptations:    []Command{{Name: "make tidy", Run: []string{"make", "tidy"}, AllowFailure: true}},
		Checks:         []Command{{Name: "build", Run: []string{"go", "build", "./..."}}},
		PRBodyTemplate: ".github/sync-upstream-pr-body.md",
	}, cfg)
}

func TestLoadErrors(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field, e.g. a typo":    "upstream: x\nownedPath: [.github/]\n",
		"no upstream":                   "ownedPaths: [.github/]\n",
		"a command without a name":      "upstream: x\nadaptations:\n  - run: [true]\n",
		"a command with nothing to run": "upstream: x\nchecks:\n  - name: build\n",
		"not YAML":                      "upstream: [x\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, content)
			require.Error(t, err)
		})
	}

	_, err := Load(t.TempDir(), DefaultPath)
	require.ErrorContains(t, err, "reading "+DefaultPath)
}
