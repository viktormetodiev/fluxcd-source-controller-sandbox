// Package config loads the description of how a fork follows its upstream.
package config

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

// DefaultPath is where the configuration lives, relative to the repository root.
const DefaultPath = ".github/fork-sync.yaml"

// Config describes how a fork follows the releases of its upstream.
type Config struct {
	// Upstream is the URL of the upstream repository.
	Upstream string `mapstructure:"upstream"`
	// BaseBranch is the fork's branch that sync pull requests target.
	BaseBranch string `mapstructure:"baseBranch"`
	// OwnedPaths are git pathspecs (files or directories) that the fork owns:
	// what upstream changes there is not taken.
	OwnedPaths []string `mapstructure:"ownedPaths"`
	// Adaptations run in order after the merge. What they change is committed.
	Adaptations []Command `mapstructure:"adaptations"`
	// Checks run after the adaptations. Their result is reported, nothing they
	// change is committed, and they never stop a sync.
	Checks []Command `mapstructure:"checks"`
	// PRBodyTemplate is a text/template file, relative to the repository root,
	// for the body of the sync pull request.
	PRBodyTemplate string `mapstructure:"prBodyTemplate"`
}

// Command is a program run in the repository during a sync.
type Command struct {
	// Name identifies the command in commit messages and reports.
	Name string `mapstructure:"name"`
	// Run is the program and its arguments.
	Run []string `mapstructure:"run"`
	// AllowFailure makes a failing adaptation drop its changes and get
	// reported, instead of stopping the sync.
	AllowFailure bool `mapstructure:"allowFailure"`
}

// Load reads the configuration at path, relative to the repository root dir,
// fills in defaults and validates it. Unknown keys are an error, so that a typo
// does not silently disable something.
func Load(dir, path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(filepath.Join(dir, path))
	v.SetConfigType("yaml")
	v.SetDefault("baseBranch", "main")
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Upstream == "" {
		return errors.New("upstream is required")
	}
	for _, list := range [][]Command{c.Adaptations, c.Checks} {
		for i, cmd := range list {
			if cmd.Name == "" {
				return fmt.Errorf("command %d has no name", i)
			}
			if len(cmd.Run) == 0 {
				return fmt.Errorf("command %q has nothing to run", cmd.Name)
			}
		}
	}
	return nil
}
