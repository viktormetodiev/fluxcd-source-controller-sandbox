package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/actions"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/forksync"
)

var mergeOpts struct {
	bodyFile string
	dryRun   bool
}

var mergeCmd = &cobra.Command{
	Use:   "merge TAG",
	Short: "Create the sync branch for an upstream release",
	Long: `Creates sync/upstream-TAG from HEAD, with the merge of the upstream release
and, when needed, a commit with what the fork owns. Prints tag, branch, base
and status (merged or conflict).

It needs no credentials, and must not get any: the adaptations run the
merged code (make tidy runs upstream's Makefile).`,
	Args: cobra.ExactArgs(1),
	RunE: runMerge,
}

func init() {
	mergeCmd.Flags().StringVar(&mergeOpts.bodyFile, "body-file", "", "write the pull request body to this file")
	mergeCmd.Flags().BoolVar(&mergeOpts.dryRun, "dry-run", false, "say in the job summary that nothing will be pushed")
	rootCmd.AddCommand(mergeCmd)
}

func runMerge(cmd *cobra.Command, args []string) error {
	s, err := newSyncer(cmd)
	if err != nil {
		return err
	}
	// Read the template before merging: it is the fork's, not upstream's.
	tmpl := forksync.DefaultPRBodyTemplate
	if s.Config.PRBodyTemplate != "" {
		data, err := os.ReadFile(filepath.Join(repoDir, s.Config.PRBodyTemplate))
		if err != nil {
			return err
		}
		tmpl = string(data)
	}

	res, err := s.Merge(args[0])
	if err != nil {
		return err
	}

	body, err := forksync.PRBody(tmpl, res, s.Config.Upstream)
	if err != nil {
		return err
	}
	if mergeOpts.bodyFile != "" {
		if err := os.WriteFile(mergeOpts.bodyFile, []byte(body), 0o644); err != nil {
			return err
		}
	}
	commits, err := s.Repo.Run("log", "--first-parent", "--oneline", res.Base+"..HEAD")
	if err != nil {
		return err
	}
	if err := actions.AppendSummary(forksync.Summary(res, commits, body, mergeOpts.dryRun)); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, o := range [][2]string{{"tag", res.Tag}, {"branch", res.Branch}, {"base", res.Base}, {"status", string(res.Status)}} {
		if err := actions.SetOutput(out, o[0], o[1]); err != nil {
			return err
		}
	}
	return nil
}
