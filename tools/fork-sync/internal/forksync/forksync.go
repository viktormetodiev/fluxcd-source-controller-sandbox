// Package forksync merges a release of a fork's upstream into the fork, the way
// the sync pull requests are built.
//
// A sync branch gets up to two commits, so that each can be reviewed on its own:
//
//  1. "Merge upstream tag vX.Y.Z": upstream's changes since the last sync,
//     merged automatically, with nothing of the fork's added. The paths the
//     fork owns take upstream's version here.
//  2. "chore: apply Qdrant adaptations ...": the fork's version of those paths
//     put back, plus what the configured rename and adaptations change. Skipped
//     when there is nothing to change.
//
// A merge with conflicts is committed as it is, with a file listing the
// conflicting files (some conflicts leave no markers), so that CI fails until
// someone resolves them. The fork-owned paths are still put back, in a commit of
// their own, so that upstream's versions of them never end up on the branch.
package forksync

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/mod/semver"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/config"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/rename"
)

const (
	// UpstreamRefs is where upstream's tags are fetched, apart from the fork's
	// own tags (vX.Y.Z-qdrant.N).
	UpstreamRefs = "refs/upstream-tags/"
	// ConflictsFile lists the conflicting files of a merge with conflicts. It is
	// committed, and CI fails while it exists.
	ConflictsFile = ".sync-upstream-conflicts"
	// BranchPrefix starts the name of every sync branch.
	BranchPrefix = "sync/upstream-"
	// renameStep names the rename in the adaptation results.
	renameStep = "rename"
)

// stableTag matches upstream's stable releases. Pre-releases and the tags of
// the api/ module (api/vX.Y.Z) do not match.
var stableTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// ErrInvalidTag is returned for a tag that is not a stable release (vX.Y.Z).
var ErrInvalidTag = errors.New("not a stable upstream release tag (expected vX.Y.Z)")

// Syncer syncs a fork with its upstream.
type Syncer struct {
	Repo   git.Repo
	Config config.Config
	// Logger reports what the sync does. The zero value logs nothing.
	Logger logr.Logger
	// Output receives the output of the commands the sync runs. Nil discards it.
	Output io.Writer
}

// Target is the upstream release to sync.
type Target struct {
	Tag string
	// UpToDate is true when the release is already merged into the fork.
	UpToDate bool
}

// Branch is the name of the sync branch for the release.
func (t Target) Branch() string {
	return BranchPrefix + t.Tag
}

// Status is the outcome of a merge.
type Status string

const (
	// StatusMerged means the merge was clean and the adaptations were applied.
	StatusMerged Status = "merged"
	// StatusConflict means the merge has conflicts, committed as they are.
	StatusConflict Status = "conflict"
)

// AdaptationResult is the outcome of an adaptation.
type AdaptationResult struct {
	Name   string
	Failed bool
	// Changed is the number of files the adaptation changed, not counting files
	// already changed by the steps before it.
	Changed int
}

// Result is the outcome of Merge.
type Result struct {
	Tag    string
	Branch string
	// Base is the commit the sync branch starts from.
	Base   string
	Status Status
	// Conflicts are the conflicting files, for StatusConflict.
	Conflicts []string
	// Restored are the fork-owned paths put back after the merge, with their
	// status relative to the merge commit.
	Restored    []git.Change
	Adaptations []AdaptationResult
}

// FailedAdaptations returns the names of the adaptations that failed.
func (r Result) FailedAdaptations() []string {
	var names []string
	for _, a := range r.Adaptations {
		if a.Failed {
			names = append(names, a.Name)
		}
	}
	return names
}

// FindTarget fetches upstream's tags and returns the release to sync: tag, or
// the latest stable release when tag is empty.
func (s *Syncer) FindTarget(tag string) (Target, error) {
	if err := s.fetchUpstreamTags(); err != nil {
		return Target{}, err
	}
	if tag == "" {
		var err error
		if tag, err = s.latestStableTag(); err != nil {
			return Target{}, err
		}
	}
	if err := s.checkTag(tag); err != nil {
		return Target{}, err
	}
	target := Target{
		Tag:      tag,
		UpToDate: s.Repo.Succeeds("merge-base", "--is-ancestor", UpstreamRefs+tag, "HEAD"),
	}
	s.Logger.Info("Found the upstream release", "tag", tag, "upToDate", target.UpToDate)
	return target, nil
}

// Merge creates the sync branch for the upstream release tag from HEAD and
// commits the merge and the adaptations to it. A merge with conflicts is not
// an error: it is committed as it is and reported with StatusConflict.
func (s *Syncer) Merge(tag string) (Result, error) {
	if err := s.fetchUpstreamTags(); err != nil {
		return Result{}, err
	}
	if err := s.checkTag(tag); err != nil {
		return Result{}, err
	}
	ref := UpstreamRefs + tag
	base, err := s.Repo.Run("rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	res := Result{Tag: tag, Branch: BranchPrefix + tag, Base: strings.TrimSpace(base)}

	owned, err := s.ownedChangedUpstream(res.Base, ref)
	if err != nil {
		return Result{}, err
	}
	s.Logger.Info("Merging the upstream release", "tag", tag, "branch", res.Branch, "forkOwnedChangedUpstream", owned)
	if _, err := s.Repo.Run("checkout", "--quiet", "-b", res.Branch); err != nil {
		return Result{}, err
	}

	conflicts, err := s.mergeUpstreamChanges(ref, owned)
	if err != nil {
		return Result{}, err
	}
	if len(conflicts) > 0 {
		s.Logger.Info("The merge has conflicts, committing it as it is", "files", conflicts)
		res.Status = StatusConflict
		res.Conflicts = conflicts
		if res.Restored, err = s.commitConflicts(tag, res.Base, owned, conflicts); err != nil {
			return Result{}, err
		}
		return res, nil
	}

	if _, err := s.Repo.Run("commit", "--quiet", "-m", "Merge upstream tag "+tag); err != nil {
		return Result{}, err
	}
	if err := s.commitAdaptations(&res, owned); err != nil {
		return Result{}, err
	}
	res.Status = StatusMerged
	s.Logger.Info("Merged the upstream release", "tag", tag, "failedAdaptations", res.FailedAdaptations())
	return res, nil
}

func (s *Syncer) fetchUpstreamTags() error {
	s.Logger.V(1).Info("Fetching upstream's tags", "upstream", s.Config.Upstream)
	_, err := s.Repo.Run("fetch", "--quiet", "--no-tags", s.Config.Upstream, "+refs/tags/*:"+UpstreamRefs+"*")
	return err
}

func (s *Syncer) latestStableTag() (string, error) {
	out, err := s.Repo.Run("for-each-ref", "--format=%(refname:strip=2)", UpstreamRefs)
	if err != nil {
		return "", err
	}
	latest := ""
	for tag := range strings.FieldsSeq(out) {
		if stableTag.MatchString(tag) && (latest == "" || semver.Compare(tag, latest) > 0) {
			latest = tag
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no stable release tags found at %s", s.Config.Upstream)
	}
	return latest, nil
}

func (s *Syncer) checkTag(tag string) error {
	if !stableTag.MatchString(tag) {
		return fmt.Errorf("%q: %w", tag, ErrInvalidTag)
	}
	if !s.Repo.Succeeds("rev-parse", "--verify", "--quiet", UpstreamRefs+tag) {
		return fmt.Errorf("upstream tag %q not found", tag)
	}
	return nil
}

// ownedChangedUpstream returns the files under the fork-owned paths that
// upstream added, changed, deleted or renamed (both names) since the last sync.
func (s *Syncer) ownedChangedUpstream(head, ref string) ([]string, error) {
	if len(s.Config.OwnedPaths) == 0 {
		return nil, nil
	}
	mergeBase, err := s.Repo.Run("merge-base", head, ref)
	if err != nil {
		return nil, err
	}
	args := append([]string{"diff", "--name-only", "--no-renames", "-z", strings.TrimSpace(mergeBase), ref, "--"}, s.Config.OwnedPaths...)
	return s.Repo.Paths(args...)
}

// mergeUpstreamChanges merges ref without committing, with the fork-owned paths
// set to upstream's version, and returns the files that still conflict.
func (s *Syncer) mergeUpstreamChanges(ref string, owned []string) ([]string, error) {
	_, mergeErr := s.Repo.Run("merge", "--no-ff", "--no-commit", ref) // fails on conflicts, which are handled below
	if !s.Repo.Succeeds("rev-parse", "--quiet", "--verify", "MERGE_HEAD") {
		if mergeErr == nil {
			return nil, fmt.Errorf("nothing to merge from %s", ref)
		}
		return nil, fmt.Errorf("failed to merge %s: %w", ref, mergeErr)
	}
	if err := s.takePathsFrom(ref, owned); err != nil {
		return nil, err
	}
	return s.Repo.Paths("diff", "--name-only", "--diff-filter=U", "-z")
}

// commitConflicts commits the unresolved merge, markers and list of conflicting
// files included, then puts the fork-owned paths back in a commit of their own.
func (s *Syncer) commitConflicts(tag, base string, owned, conflicts []string) ([]git.Change, error) {
	list := strings.Join(conflicts, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(s.Repo.Dir, ConflictsFile), []byte(list), 0o644); err != nil {
		return nil, err
	}
	if _, err := s.Repo.Run("add", "--all"); err != nil {
		return nil, err
	}
	if _, err := s.Repo.Run("commit", "--quiet", "--no-verify", "-m", "Merge upstream tag "+tag+" (UNRESOLVED CONFLICTS)"); err != nil {
		return nil, err
	}

	restored, err := s.restoreOwned(base, owned)
	if err != nil || len(restored) == 0 {
		return restored, err
	}
	_, err = s.Repo.Run("commit", "--quiet", "--no-verify",
		"-m", "chore: keep the fork-owned paths after merging "+tag,
		"-m", "The merge has conflicts, so only the fork-owned paths are put back for now "+
			"(status is relative to the merge commit):\n"+formatChanges(restored))
	return restored, err
}

// Rename applies the configured rename to the working tree, leaving out the
// fork-owned paths, and returns the paths it wrote. Without a rename configured
// it does nothing.
func (s *Syncer) Rename() ([]string, error) {
	if s.Config.Rename == nil {
		return nil, nil
	}
	return rename.Apply(s.Repo, *s.Config.Rename, s.Config.OwnedPaths)
}

// commitAdaptations puts the fork-owned paths back, renames and runs the
// adaptations, then commits all of it, if anything changed.
func (s *Syncer) commitAdaptations(res *Result, owned []string) error {
	var err error
	if res.Restored, err = s.restoreOwned(res.Base, owned); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range res.Restored {
		seen[c.Path] = true
	}

	if s.Config.Rename != nil {
		rename := func() error {
			_, err := s.Rename()
			return err
		}
		result, err := s.runAdaptation(renameStep, false, rename, seen)
		if err != nil {
			return err
		}
		res.Adaptations = append(res.Adaptations, result)
	}
	for _, cmd := range s.Config.Adaptations {
		runCmd := func() error { return s.run(cmd) }
		result, err := s.runAdaptation(cmd.Name, cmd.AllowFailure, runCmd, seen)
		if err != nil {
			return err
		}
		res.Adaptations = append(res.Adaptations, result)
	}

	if s.Repo.Succeeds("diff", "--cached", "--quiet") {
		return nil
	}
	_, err = s.Repo.Run("commit", "--quiet",
		"-m", "chore: apply Qdrant adaptations after merging "+res.Tag,
		"-m", adaptationsMessage(*res))
	return err
}

// runAdaptation runs one adaptation, apply, and stages what it changed. When it
// fails and is allowed to, its changes are dropped instead. Files already in
// seen are not counted as changed by it, and the ones it changes are added.
func (s *Syncer) runAdaptation(name string, allowFailure bool, apply func() error, seen map[string]bool) (AdaptationResult, error) {
	result := AdaptationResult{Name: name}
	untrackedBefore, err := s.Repo.Paths("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return result, err
	}

	if runErr := apply(); runErr != nil {
		if !allowFailure {
			return result, fmt.Errorf("failed to run the adaptation %q: %w", name, runErr)
		}
		s.Logger.Error(runErr, "Adaptation failed, dropping its changes", "adaptation", name)
		result.Failed = true
		return result, s.dropUnstaged(untrackedBefore)
	}

	if _, err := s.Repo.Run("add", "--all"); err != nil {
		return result, err
	}
	staged, err := s.Repo.Paths("diff", "--cached", "--name-only", "--no-renames", "-z")
	if err != nil {
		return result, err
	}
	for _, path := range staged {
		if !seen[path] {
			seen[path] = true
			result.Changed++
		}
	}
	s.Logger.Info("Adaptation done", "adaptation", name, "changedFiles", result.Changed)
	return result, nil
}

// restoreOwned puts the given fork-owned paths back as they are in base, and
// returns their status relative to the current commit.
func (s *Syncer) restoreOwned(base string, owned []string) ([]git.Change, error) {
	if err := s.takePathsFrom(base, owned); err != nil {
		return nil, err
	}
	return s.Repo.NameStatus("--cached")
}

// takePathsFrom makes the working tree and index have each path as it is in
// ref, or not have it at all if ref does not.
func (s *Syncer) takePathsFrom(ref string, paths []string) error {
	for _, path := range paths {
		var err error
		if s.Repo.Succeeds("cat-file", "-e", ref+":"+path) {
			_, err = s.Repo.Run("checkout", ref, "--", path)
		} else {
			_, err = s.Repo.Run("rm", "-q", "-f", "--ignore-unmatch", "--", path)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// dropUnstaged reverts what is not staged: changes to tracked files, and the
// untracked files that were not there before (given).
func (s *Syncer) dropUnstaged(untrackedBefore []string) error {
	if _, err := s.Repo.Run("checkout", "--", "."); err != nil {
		return err
	}
	untracked, err := s.Repo.Paths("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	before := map[string]bool{}
	for _, p := range untrackedBefore {
		before[p] = true
	}
	for _, p := range untracked {
		if !before[p] {
			if err := os.Remove(filepath.Join(s.Repo.Dir, p)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Syncer) run(cmd config.Adaptation) error {
	c := exec.Command(cmd.Run[0], cmd.Run[1:]...) //nolint:gosec // the commands come from the fork's own configuration
	c.Dir = s.Repo.Dir
	out := s.Output
	if out == nil {
		out = io.Discard
	}
	c.Stdout = out
	c.Stderr = out
	return c.Run()
}
