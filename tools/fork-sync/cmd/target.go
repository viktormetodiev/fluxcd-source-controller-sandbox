package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/actions"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/forksync"
)

const (
	statusUpToDate  = "up-to-date"
	statusNeedsSync = "needs-sync"
	statusExists    = "exists"
)

var targetOpts struct {
	tag           string
	checkExisting bool
}

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Find the upstream release to sync",
	Long: `Finds the upstream release to sync (the latest stable one, unless --tag is
given) and prints tag, branch and status:

  needs-sync   the release is not in the fork yet
  up-to-date   the release is already merged
  exists       with --check-existing: a sync branch or pull request for it is
               already there, including a pull request closed without merging,
               which is a decision not to take that release.

--check-existing reads pull requests with the GH_TOKEN environment variable.`,
	Args: cobra.NoArgs,
	RunE: runTarget,
}

func init() {
	targetCmd.Flags().StringVar(&targetOpts.tag, "tag", "", "upstream release to sync, vX.Y.Z (default: the latest stable release)")
	targetCmd.Flags().BoolVar(&targetOpts.checkExisting, "check-existing", false, "report a release that already has a sync branch or pull request")
	rootCmd.AddCommand(targetCmd)
}

func runTarget(cmd *cobra.Command, _ []string) error {
	s, err := newSyncer(cmd)
	if err != nil {
		return err
	}
	target, err := s.FindTarget(targetOpts.tag)
	if err != nil {
		return err
	}

	status := statusNeedsSync
	switch {
	case target.UpToDate:
		status = statusUpToDate
	case targetOpts.checkExisting:
		exists, err := syncStarted(cmd.Context(), s, target.Branch())
		if err != nil {
			return err
		}
		if exists {
			status = statusExists
		}
	}

	out := cmd.OutOrStdout()
	for _, o := range [][2]string{{"tag", target.Tag}, {"branch", target.Branch()}, {"status", status}} {
		if err := actions.SetOutput(out, o[0], o[1]); err != nil {
			return err
		}
	}
	return nil
}

// syncStarted reports whether the sync branch is on origin or has a pull request.
func syncStarted(ctx context.Context, s *forksync.Syncer, branch string) (bool, error) {
	if s.Repo.Succeeds("ls-remote", "--exit-code", "--heads", "origin", branch) {
		return true, nil
	}
	client, err := newGitHubClient()
	if err != nil {
		return false, err
	}
	return client.PullRequestExists(ctx, branch)
}
