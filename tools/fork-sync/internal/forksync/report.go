package forksync

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
)

// DefaultPRBodyTemplate is used when the configuration names no template.
const DefaultPRBodyTemplate = "Merges upstream [`{{ .Tag }}`]({{ .UpstreamURL }}/releases/tag/{{ .Tag }}) into the fork.\n"

// PRBodyData is what a pull request body template can use.
type PRBodyData struct {
	// Tag is the upstream release, vX.Y.Z.
	Tag string
	// UpstreamURL is the upstream repository's URL, without ".git".
	UpstreamURL string
}

// adaptationsMessage is the body of the adaptations commit: what each part of it
// did, so the pull request can be judged from it.
func adaptationsMessage(r Result) string {
	parts := []string{"Reapplies what the fork owns on top of the upstream merge."}
	if len(r.Restored) > 0 {
		parts = append(parts, "Fork-owned paths, put back as the fork has them (status is relative to the merge commit):\n"+
			formatChanges(r.Restored))
	}
	for _, a := range r.Adaptations {
		switch {
		case a.Failed:
			parts = append(parts, a.Name+" failed, so its changes are not included.")
		case a.Changed > 0:
			parts = append(parts, fmt.Sprintf("%s changed %d file(s).", a.Name, a.Changed))
		}
	}
	return strings.Join(parts, "\n\n")
}

// PRBody renders the body of the sync pull request: what needs attention first,
// then the template.
func PRBody(tmpl string, r Result, upstreamURL string) (string, error) {
	t, err := template.New("pr-body").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing the pull request body template: %w", err)
	}
	var b bytes.Buffer
	for _, name := range r.FailedAdaptations() {
		fmt.Fprintf(&b, "> **`%s` failed**, so its changes are not included. See the workflow run for the error.\n\n", name)
	}
	for _, name := range r.FailedChecks() {
		fmt.Fprintf(&b, "> **Check `%s` failed** after the merge. See the workflow run for the error.\n\n", name)
	}
	if len(r.Conflicts) > 0 {
		fmt.Fprintf(&b, "> **Merge conflicts.** The merge is committed as it is, with a list of the conflicting files. "+
			"CI fails until they are resolved and `%s` is deleted. Conflicting files:\n>\n", ConflictsFile)
		for _, path := range r.Conflicts {
			fmt.Fprintf(&b, "> - `%s`\n", path)
		}
		b.WriteString("\n")
	}
	data := PRBodyData{Tag: r.Tag, UpstreamURL: strings.TrimSuffix(upstreamURL, ".git")}
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("rendering the pull request body: %w", err)
	}
	return b.String(), nil
}

// Summary is a Markdown report of a merge, for the workflow's job summary.
func Summary(r Result, commits, prBody string, dryRun bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Sync upstream %s\n\nMerge status: **%s**.", r.Tag, r.Status)
	for _, c := range r.Checks {
		outcome := "passed"
		if c.Failed {
			outcome = "failed"
		}
		fmt.Fprintf(&b, " Check %s: **%s**.", c.Name, outcome)
	}
	b.WriteString("\n")
	if dryRun {
		b.WriteString("Dry run: nothing was pushed and no pull request was opened.\n")
	}
	fmt.Fprintf(&b, "\n```\n%s```\n\n### Pull request body\n\n%s", commits, prBody)
	return b.String()
}

func formatChanges(changes []git.Change) string {
	lines := make([]string, len(changes))
	for i, c := range changes {
		lines[i] = c.Status + "\t" + c.Path
	}
	return strings.Join(lines, "\n")
}
