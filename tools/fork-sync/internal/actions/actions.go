// Package actions writes step outputs and job summaries for GitHub Actions.
// Outside of GitHub Actions, outputs are only printed.
package actions

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// SetOutput prints name=value to w and, in GitHub Actions, records it as a step
// output.
func SetOutput(w io.Writer, name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("output %s: multi-line values are not supported", name)
	}
	if _, err := fmt.Fprintf(w, "%s=%s\n", name, value); err != nil {
		return err
	}
	return appendToEnvFile("GITHUB_OUTPUT", name+"="+value+"\n")
}

// AppendSummary adds Markdown to the job summary, in GitHub Actions.
func AppendSummary(markdown string) error {
	return appendToEnvFile("GITHUB_STEP_SUMMARY", markdown)
}

// appendToEnvFile appends to the file named by the environment variable env,
// which GitHub Actions sets; nothing happens when it is not set.
func appendToEnvFile(env, text string) error {
	path := os.Getenv(env)
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
