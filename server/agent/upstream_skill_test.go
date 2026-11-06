package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// upstreamSkillRepos are read-only upstream Git submodules vendored directly
// under harness/skills. Their bytes stay upstream-owned and are distributed
// verbatim, so Anban Skill authoring contracts do not apply to their files.
var upstreamSkillRepos = []string{
	"humanizer",
	"seedance2-skill",
	"motion-graphics",
	"ecommerce-visual-copywriting-skill",
}

// isUpstreamSkillPath reports whether path belongs to an upstream Skill
// submodule under harness/skills, including directories.
func isUpstreamSkillPath(path string) bool {
	slash := filepath.ToSlash(path)
	if !strings.HasSuffix(slash, "/") {
		slash += "/"
	}
	for _, repo := range upstreamSkillRepos {
		if strings.Contains(slash, "/harness/skills/"+repo+"/") {
			return true
		}
	}
	return false
}

// isUpstreamSkillFile reports whether path is an upstream SKILL.md.
func isUpstreamSkillFile(path string) bool {
	return filepath.Base(path) == "SKILL.md" && isUpstreamSkillPath(path)
}

func TestUpstreamSkillsLiveUnderTheSingleSkillTree(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, "harness", ".gitmodules"))

	declared := map[string]bool{}
	for _, line := range strings.Split(gitmodules, "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), `[submodule "skills/`)
		if !ok {
			continue
		}
		if name, ok = strings.CutSuffix(name, `"]`); !ok || name == "" || strings.Contains(name, "/") {
			continue
		}
		declared[name] = true
	}

	for _, repo := range upstreamSkillRepos {
		path := "skills/" + repo
		if !declared[repo] {
			t.Fatalf("harness/.gitmodules must declare the %s upstream Skill submodule", path)
		}
		info, err := os.Stat(filepath.Join(root, "harness", filepath.FromSlash(path)))
		if err != nil || !info.IsDir() {
			t.Fatalf("upstream Skill submodule %s must be checked out under harness/skills: %v", repo, err)
		}
	}
	for repo := range declared {
		if !slices.Contains(upstreamSkillRepos, repo) {
			t.Fatalf("upstream Skill submodule %q is missing from the Skill contract exemption list", repo)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "harness", "third_party")); !os.IsNotExist(err) {
		t.Fatal("harness/third_party must not exist; upstream Skills live under harness/skills/<repo>")
	}
}
