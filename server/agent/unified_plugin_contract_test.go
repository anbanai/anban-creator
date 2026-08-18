package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestUnifiedPluginLayout(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "plugins")

	for _, legacy := range []string{"claudecode", "codex"} {
		if _, err := os.Stat(filepath.Join(root, legacy)); !os.IsNotExist(err) {
			t.Fatalf("legacy plugin root %q must not exist: %v", legacy, err)
		}
	}
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	for _, legacy := range []string{"claudecode", "codex"} {
		if strings.Contains(gitmodules, `submodule "`+legacy+`"`) || strings.Contains(gitmodules, "path = "+legacy) {
			t.Fatalf(".gitmodules still declares legacy plugin %q", legacy)
		}
	}

	skillRoot := filepath.Join(pluginRoot, "skills")
	if info, err := os.Stat(skillRoot); err != nil || !info.IsDir() {
		t.Fatalf("canonical plugin Skill root missing at %s: %v", skillRoot, err)
	}
	nestedSkillRoots, err := filepath.Glob(filepath.Join(pluginRoot, "*", "skills"))
	if err != nil {
		t.Fatalf("glob nested plugin Skill roots: %v", err)
	}
	if len(nestedSkillRoots) != 0 {
		t.Fatalf("nested plugin Skill roots = %v, want none below flattened root %s", nestedSkillRoots, pluginRoot)
	}

	type manifest struct {
		Name      string          `json:"name"`
		Version   string          `json:"version"`
		Skills    string          `json:"skills"`
		Hooks     json.RawMessage `json:"hooks"`
		Interface any             `json:"interface"`
	}
	readManifest := func(path string) manifest {
		t.Helper()
		var got manifest
		raw := readRepoFile(t, path)
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return got
	}
	claudeManifest := readManifest(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"))
	codexManifest := readManifest(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"))
	if claudeManifest.Name != "anban" || codexManifest.Name != "anban" {
		t.Fatalf("native manifest names = %q/%q, want anban", claudeManifest.Name, codexManifest.Name)
	}
	if claudeManifest.Version == "" || claudeManifest.Version != codexManifest.Version {
		t.Fatalf("native manifest versions = %q/%q, want one aligned version", claudeManifest.Version, codexManifest.Version)
	}
	if claudeManifest.Version != "4.1.13" {
		t.Fatalf("native manifest version = %q, want 4.1.13 for the current plugin surface", claudeManifest.Version)
	}
	if codexManifest.Skills != "./skills/" || codexManifest.Interface == nil {
		t.Fatalf("Codex manifest must reference shared Skills and declare interface metadata")
	}
	if err := validateCodexHooksDisabled(codexManifest.Hooks); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(pluginRoot, ".mcp.json"),
		filepath.Join(pluginRoot, "hooks", "hooks.json"),
		filepath.Join(pluginRoot, "install", "agents-registration.toml"),
	} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("required native adapter missing at %s: %v", path, err)
		}
	}
	registration := readRepoFile(t, filepath.Join(pluginRoot, "install", "agents-registration.toml"))
	for _, want := range []string{"[mcp_servers.creator]", `bearer_token_env_var = "ANBAN_API_KEY"`} {
		if !strings.Contains(registration, want) {
			t.Fatalf("Codex registration missing %q", want)
		}
	}

	markdownAgents := pluginAgentNames(t, filepath.Join(pluginRoot, "agents"), ".md")
	tomlAgents := pluginAgentNames(t, filepath.Join(pluginRoot, "agents"), ".toml")
	if len(markdownAgents) != 6 || strings.Join(markdownAgents, "\n") != strings.Join(tomlAgents, "\n") {
		t.Fatalf("native Agent sets differ: Claude=%v Codex=%v", markdownAgents, tomlAgents)
	}
}

func TestCodexHooksDisabledContractRejectsNullAndNonEmptyValues(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "null", raw: json.RawMessage(`null`), wantErr: true},
		{name: "object", raw: json.RawMessage(`{}`), wantErr: true},
		{name: "non-empty array", raw: json.RawMessage(`["./hooks/codex.json"]`), wantErr: true},
		{name: "explicit empty array", raw: json.RawMessage(`[]`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCodexHooksDisabled(test.raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateCodexHooksDisabled(%s) error = %v, wantErr %t", test.raw, err, test.wantErr)
			}
		})
	}
}

func validateCodexHooksDisabled(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("Codex manifest must explicitly override default plugin hook discovery")
	}
	var hooks []json.RawMessage
	if err := json.Unmarshal(raw, &hooks); err != nil {
		return fmt.Errorf("Codex manifest hooks must be an array: %w", err)
	}
	if hooks == nil {
		return fmt.Errorf("Codex manifest hooks must be an explicit empty array, not null")
	}
	if len(hooks) != 0 {
		return fmt.Errorf("Codex manifest hooks = %s, want an explicit empty array until a validated reporter adapter exists", raw)
	}
	return nil
}

func TestRuntimeContractsDoNotUseTaskFileListingAsCompletionGate(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
		"plugins/skills/seednote-visual-design/SKILL.md",
	} {
		body := readRepoFile(t, filepath.Join(root, rel))
		if strings.Contains(body, "list_task_files") {
			t.Fatalf("%s still depends on list_task_files during execution", rel)
		}
	}
}

func TestPluginImagePromptRecordsStayCreativeOnly(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, path := range pluginWorkflowFiles(t, root) {
		for _, finding := range technicalImagePromptRecordFields(readRepoFile(t, path)) {
			t.Errorf("%s image prompt record exposes technical field %q", path, finding)
		}
	}
}

func TestImagePromptRecordScannerRejectsMultilineTechnicalMetadata(t *testing.T) {
	body := "" +
		"Write image-prompts.md with this format:\n\n" +
		"```yaml\n" +
		"prompt: a yellow tea cup\n" +
		"provider: openai\n" +
		"output_path: output/cover.png\n" +
		"```\n"
	got := technicalImagePromptRecordFields(body)
	if strings.Join(got, ",") != "output_path,provider" {
		t.Fatalf("technical fields = %v, want provider and output_path", got)
	}
}

func TestPluginImageCapabilitiesUseTaskOwnedOutput(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, path := range pluginWorkflowFiles(t, root) {
		body := readRepoFile(t, path)
		if strings.Contains(body, "/tmp/anban-") {
			t.Errorf("%s still uses a process-global /tmp image path", path)
		}
		for _, finding := range generateImageContractFindings(body) {
			t.Errorf("%s %s", path, finding)
		}
	}
}

func TestGenerateImageContractScannerRejectsInvalidSingleLineCall(t *testing.T) {
	body := `generate_image(project_id="p", prompt="x", output_path="scratch/cover.png")`
	got := strings.Join(generateImageContractFindings(body), "\n")
	if !strings.Contains(got, "omits required task_id") || !strings.Contains(got, "must use task-owned output/") {
		t.Fatalf("findings = %q, want task_id and output/ violations", got)
	}
}

func pluginWorkflowFiles(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, dir := range []string{filepath.Join(root, "agents"), filepath.Join(root, "skills")} {
		if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != dir && filepath.Base(path) == "humanizer" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".md" || ext == ".toml" {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(paths)
	return paths
}

func technicalImagePromptRecordFields(body string) []string {
	technicalField := regexp.MustCompile(`(?i)^[[:space:]>*\x60"'-]*(provider|model|revised_prompt|verification|selection_reason|actual_width|actual_height|generation_attempts|output_path|ref_image_path)[[:space:]\x60"]*[:=：]`)
	lines := strings.Split(body, "\n")
	seen := make(map[string]struct{})
	for index, line := range lines {
		if !strings.Contains(strings.ToLower(line), "image-prompts.md") {
			continue
		}
		end := index + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		cursor := end
		for cursor < len(lines) && strings.TrimSpace(lines[cursor]) == "" {
			cursor++
		}
		if cursor < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[cursor]), "```") {
			end = cursor + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				end++
			}
		}
		for _, recordLine := range lines[index:end] {
			match := technicalField.FindStringSubmatch(recordLine)
			if len(match) == 2 {
				seen[strings.ToLower(match[1])] = struct{}{}
			}
		}
	}
	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func generateImageContractFindings(body string) []string {
	var findings []string
	for offset := 0; ; {
		start := strings.Index(body[offset:], "generate_image(")
		if start < 0 {
			break
		}
		start += offset
		depth := 0
		end := -1
		for index := start; index < len(body); index++ {
			switch body[index] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = index + 1
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		call := body[start:end]
		offset = end
		if !strings.Contains(call, "project_id=") && !strings.Contains(call, "project_id =") {
			continue
		}
		lineNumber := strings.Count(body[:start], "\n") + 1
		if !strings.Contains(call, "task_id=") && !strings.Contains(call, "task_id =") {
			findings = append(findings, fmt.Sprintf(":%d generate_image call omits required task_id: %s", lineNumber, call))
		}
		if !strings.Contains(call, `output_path="output/`) && !strings.Contains(call, `output_path = "output/`) {
			findings = append(findings, fmt.Sprintf(":%d generate_image call must use task-owned output/: %s", lineNumber, call))
		}
	}
	return findings
}

func pluginAgentNames(t *testing.T, dir, extension string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read Agent directory %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), extension))
	}
	sort.Strings(names)
	return names
}
