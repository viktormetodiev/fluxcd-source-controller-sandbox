package forksync

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/qdrant/fluxcd-source-controller/tools/fork-sync/internal/git"
)

// prBodyData is what a pull request body template can use.
type prBodyData struct {
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
	for _, name := range r.FailedAdaptations {
		parts = append(parts, name+" failed, so its changes are not included.")
	}
	return strings.Join(parts, "\n\n")
}

// PRBody renders the body of the sync pull request: what needs attention first,
// then the template.
func PRBody(tmpl string, r Result, upstreamURL string) (string, error) {
	t, err := template.New("pr-body").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse the pull request body template: %w", err)
	}
	var b bytes.Buffer
	for _, name := range r.FailedAdaptations {
		fmt.Fprintf(&b, "> **`%s` failed**, so its changes are not included. See the workflow run for the error.\n\n", name)
	}
	if len(r.Conflicts) > 0 {
		fmt.Fprintf(&b, "> **Merge conflicts.** The merge is committed as it is, with a list of the conflicting files. "+
			"CI fails until they are resolved and `%s` is deleted. Conflicting files:\n>\n", ConflictsFile)
		for _, path := range r.Conflicts {
			fmt.Fprintf(&b, "> - `%s`\n", path)
		}
		b.WriteString("\n")
	}
	data := prBodyData{Tag: r.Tag, UpstreamURL: strings.TrimSuffix(upstreamURL, ".git")}
	err = t.Execute(&b, data)
	if err != nil {
		return "", fmt.Errorf("failed to render the pull request body: %w", err)
	}
	return b.String(), nil
}

// Summary is a Markdown report of a merge, for the workflow's job summary:
// its status, its commits (one line each) and the pull request body.
func Summary(r Result, commits, prBody string) string {
	return fmt.Sprintf("## Sync upstream %s\n\nMerge status: **%s**.\n\n```\n%s```\n\n### Pull request body\n\n%s",
		r.Tag, r.Status, commits, prBody)
}

func formatChanges(changes []git.Change) string {
	lines := make([]string, len(changes))
	for i, c := range changes {
		lines[i] = c.Status + "\t" + c.Path
	}
	return strings.Join(lines, "\n")
}
