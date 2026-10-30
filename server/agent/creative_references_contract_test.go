package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCreativeReferenceCheckerEnforcesParentGitlinks(t *testing.T) {
	root := repositoryRoot(t)
	script := readRepoFile(t, filepath.Join(root, "harness", "scripts", "check-creative-references.sh"))
	for _, want := range []string{
		`gitlink_sha=${2:-}`,
		`checkout_sha=$(git -C "$path" rev-parse HEAD 2>/dev/null || true)`,
		`if [ -z "$gitlink_sha" ] || [ "$checkout_sha" != "$gitlink_sha" ]; then`,
		`does not match parent gitlink`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("creative-reference checker missing gitlink integrity check %q", want)
		}
	}
}
