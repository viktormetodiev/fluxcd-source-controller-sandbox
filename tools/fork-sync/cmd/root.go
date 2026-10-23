// Package cmd is the command line of fork-sync.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/config"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/forksync"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/github"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "fork-sync",
	Short: "Brings the releases of a fork's upstream into the fork, as pull requests",
	Long: `fork-sync keeps a fork up to date with the releases of its upstream. It is
run by the sync-upstream workflow, and can be run locally to reproduce it:

  fork-sync target          find the upstream release to sync
  fork-sync merge TAG       create the sync branch for it (no credentials needed)
  fork-sync publish         push the branch and open its pull request

What the fork owns and how it adapts upstream's code is configured in ` + config.DefaultPath + `.`,
	SilenceUsage: true,
}

var (
	repoDir    string
	configPath string
	logLevel   string
)

// Execute runs the command line. It is called by main.main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&repoDir, "repo", ".", "root of the fork's working tree")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultPath, "configuration file, relative to the repository root")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "debug, info, warn or error")
}

func newSyncer(cmd *cobra.Command) (forksync.Syncer, error) {
	logger, err := newLogger(cmd.ErrOrStderr(), logLevel)
	if err != nil {
		return forksync.Syncer{}, err
	}
	cfg, err := config.Load(repoDir, configPath)
	if err != nil {
		return forksync.Syncer{}, err
	}
	out := cmd.ErrOrStderr()
	return forksync.Syncer{Repo: git.Repo{Dir: repoDir, Output: out}, Config: cfg, Logger: logger, Output: out}, nil
}

// newLogger returns a human-readable logger, without stack traces: the logs
// are read in the output of a workflow run.
func newLogger(w io.Writer, level string) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("--log-level: %w", err)
	}
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	return zap.New(zapcore.NewCore(encoder, zapcore.AddSync(w), lvl)), nil
}

// newGitHubClient returns a client for the repository GitHub Actions runs in
// (GITHUB_REPOSITORY), authenticated with the GH_TOKEN environment variable.
func newGitHubClient() (*github.Client, error) {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		return nil, errors.New("the GH_TOKEN environment variable is not set")
	}
	repository := os.Getenv("GITHUB_REPOSITORY")
	if repository == "" {
		return nil, errors.New("the GITHUB_REPOSITORY environment variable (owner/name) is not set")
	}
	return github.New(token, repository, os.Getenv("GITHUB_API_URL"))
}
