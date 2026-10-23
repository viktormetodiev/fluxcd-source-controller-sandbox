// Package git runs the git command line in a repository.
//
// It uses git itself rather than a Go implementation: syncing a fork needs a
// three-way merge that can stop on conflicts, which go-git does not do.
package git

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Repo is a git working tree.
type Repo struct {
	// Dir is the root of the working tree.
	Dir string
	// Output receives what git prints on its standard error. Nil discards it.
	Output io.Writer
}

// Change is a path with its status in a diff, as git prints it: "A", "M" or "D".
type Change struct {
	Status string
	Path   string
}

// Run runs git with the given arguments and returns its standard output.
func (r Repo) Run(args ...string) (string, error) {
	return r.run(nil, args...)
}

// Succeeds runs git and reports whether it exited with status 0, for commands
// whose exit status is the answer (merge-base --is-ancestor, cat-file -e).
func (r Repo) Succeeds(args ...string) bool {
	_, err := r.Run(args...)
	return err == nil
}

// Paths runs git with the given arguments, which must make it print
// NUL-separated paths (-z), and returns them. NUL separation keeps unusual file
// names intact, where newline-separated output would quote them.
func (r Repo) Paths(args ...string) ([]string, error) {
	out, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

// NameStatus returns the changes of "git diff --name-status --no-renames -z"
// with the given extra arguments.
func (r Repo) NameStatus(args ...string) ([]Change, error) {
	fields, err := r.Paths(append([]string{"diff", "--name-status", "--no-renames", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("unexpected git diff --name-status output: %q", fields)
	}
	changes := make([]Change, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		changes = append(changes, Change{Status: fields[i], Path: fields[i+1]})
	}
	return changes, nil
}

// PushWithToken pushes a branch to origin, authenticating to github.com with
// the given token. The token is passed through git's environment-based
// configuration, so it does not show up in the command line, in process
// listings or in error messages.
func (r Repo) PushWithToken(branch, token string) error {
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	env := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=" + header,
	}
	_, err := r.run(env, "push", "origin", branch)
	return err
}

func (r Repo) run(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	if env != nil {
		cmd.Env = append(cmd.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if r.Output != nil && stderr.Len() > 0 {
		_, _ = r.Output.Write(stderr.Bytes())
	}
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return stdout.String(), fmt.Errorf("failed to run git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), fmt.Errorf("failed to run git %s: %w", args[0], err)
	}
	return stdout.String(), nil
}

func splitNUL(s string) []string {
	parts := strings.Split(s, "\x00")
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}
