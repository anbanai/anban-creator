package agent

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

var explicitOutputPathRE = regexp.MustCompile(`output/[A-Za-z0-9][A-Za-z0-9_.*:/-]*\.(?:json|md|html|png|jpg|jpeg|mp3|mp4|zip)`)

func TestPackExplicitOutputPathsAreDeclared(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "harness")
	catalog, err := agentpack.LoadCatalog(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}

	for _, pack := range catalog.Packs {
		pack := pack
		t.Run(pack.ID, func(t *testing.T) {
			declared := make([]string, 0, len(pack.Artifacts))
			for _, artifact := range pack.Artifacts {
				declared = append(declared, filepath.ToSlash(artifact.Path))
			}
			for _, artifacts := range pack.ArtifactsByTaskType {
				for _, artifact := range artifacts {
					declared = append(declared, filepath.ToSlash(artifact.Path))
				}
			}
			sort.Strings(declared)

			files := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.ClaudeSource),
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.CodexSource),
			}
			for _, skill := range pack.Agent.Skills {
				files = append(files, filepath.Join(pluginRoot, "skills", skill, "SKILL.md"))
			}
			seen := map[string]bool{}
			for _, file := range files {
				body, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("read %s: %v", file, err)
				}
				for _, raw := range explicitOutputPathRE.FindAllString(string(body), -1) {
					candidate := normalizeOutputPath(raw)
					if candidate == "" || seen[candidate] {
						continue
					}
					seen[candidate] = true
					if !matchesArtifactPath(declared, candidate) {
						t.Errorf("%s declares %s but Pack %q has no matching artifact", filepath.ToSlash(file), candidate, pack.ID)
					}
				}
			}
		})
	}
}

func normalizeOutputPath(raw string) string {
	cleaned := strings.TrimRight(raw, "`'\"),.;:、。")
	if cleaned == "output/failure-state.json" {
		return cleaned
	}
	// Examples such as img_N.png and cover-NN.jpg describe the declared glob.
	cleaned = strings.ReplaceAll(cleaned, "-NN.", "-*.")
	cleaned = strings.ReplaceAll(cleaned, "-N.", "-*.")
	cleaned = strings.ReplaceAll(cleaned, "_N.", "_*.")
	cleaned = strings.ReplaceAll(cleaned, "_0N.", "_*.")
	cleaned = strings.ReplaceAll(cleaned, "_0i.", "_*.")
	return cleaned
}

func matchesArtifactPath(declared []string, candidate string) bool {
	for _, pattern := range declared {
		matched, err := path.Match(pattern, candidate)
		if err == nil && matched {
			return true
		}
		// A concrete example in an Agent contract is covered by a Pack glob.
		if strings.Contains(pattern, "*") {
			if matched, _ := path.Match(pattern, candidate); matched {
				return true
			}
		}
	}
	return false
}

func TestJSONOutputContractsDeclareSharedMetadata(t *testing.T) {
	root := filepath.Join(repoRoot(t), "harness")
	catalog, err := agentpack.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, pack := range catalog.Packs {
		for _, source := range []string{pack.Agent.ClaudeSource, pack.Agent.CodexSource} {
			file := filepath.Join(root, "packs", pack.ID, source)
			body := readRepoFile(t, file)
			var hasJSON bool
			for _, artifact := range append(append([]agentpack.ArtifactSpec{}, pack.Artifacts...), flattenArtifactOverrides(pack.ArtifactsByTaskType)...) {
				// Progress and failure state are runner-owned envelopes. They do not
				// make the Agent's domain output a JSON result requiring the shared
				// analytics metadata contract.
				if artifact.Role != "progress_state" && artifact.Role != "failure_state" && strings.HasSuffix(artifact.Path, ".json") {
					hasJSON = true
					break
				}
			}
			if !hasJSON {
				continue
			}
			for _, field := range []string{"schema_version", "status", "source", "data_at", "missing", "evidence_paths"} {
				if !strings.Contains(body, field) {
					t.Errorf("%s has JSON Pack outputs but does not declare %s", file, field)
				}
			}
			if !strings.Contains(body, "output/failure-state.json") || !strings.Contains(body, "resume_from") {
				t.Errorf("%s has JSON Pack outputs but does not declare the shared failure contract", file)
			}
		}
	}
}

func flattenArtifactOverrides(overrides map[string][]agentpack.ArtifactSpec) []agentpack.ArtifactSpec {
	var out []agentpack.ArtifactSpec
	for _, artifacts := range overrides {
		out = append(out, artifacts...)
	}
	return out
}

func TestSkillBoundariesStayHostNeutral(t *testing.T) {
	root := filepath.Join(repoRoot(t), "harness", "skills")
	forbidden := []string{"set_task_progress_plan", "update_task_progress", "anban_stage_id", "TaskCreate", "TaskUpdate"}
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" || strings.Contains(filepath.ToSlash(file), "/humanizer/") {
			return nil
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		for _, token := range forbidden {
			if strings.Contains(string(body), token) {
				t.Errorf("%s contains Agent lifecycle token %q", file, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
