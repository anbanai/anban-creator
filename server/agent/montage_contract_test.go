package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"gopkg.in/yaml.v3"
)

func TestMontageTaskMapsToDedicatedAgent(t *testing.T) {
	if got := TaskTypeToAgent(model.PlatformMontage); got != "montage" {
		t.Fatalf("TaskTypeToAgent(montage) = %q, want montage", got)
	}
}

func TestMontageIsCanonicalAgentName(t *testing.T) {
	if got := TaskTypeToAgent("montage"); got != "montage" {
		t.Fatalf("TaskTypeToAgent(montage) = %q, want montage", got)
	}
	if got := TaskTypeToAgent("open" + "montage"); got == "montage" {
		t.Fatalf("TaskTypeToAgent(%s) = %q, old %s name must not remain canonical", "open"+"montage", got, "open"+"montage")
	}
}

func TestContainerRuntimePathUsesPackRuntimeAdapter(t *testing.T) {
	if got := runtimeAdapterForTaskType(model.PlatformMontage); got != "openmontage" {
		t.Fatalf("montage adapter = %q", got)
	}
	if got := runtimeAdapterForTaskType(model.TaskTypeLiveSlicer); got != "standard" {
		t.Fatalf("live-slicer adapter = %q", got)
	}
	if got := containerRuntimePath(model.PlatformSeednote); got != ContainerContentRuntimePath {
		t.Fatalf("seednote runtime PATH = %q, want content PATH", got)
	}
	if got := containerRuntimePath(model.PlatformMontage); got != ContainerMontageRuntimePath {
		t.Fatalf("montage runtime PATH = %q", got)
	}
	if got := containerRuntimePath(model.TaskTypeLiveSlicer); got != ContainerContentRuntimePath {
		t.Fatalf("live-slicer runtime PATH = %q, standard adapter must not inherit OpenMontage PATH", got)
	}
}

func TestMontageWorkspaceInputFileIsWritten(t *testing.T) {
	workDir := t.TempDir()
	task := &model.Task{Type: model.PlatformMontage}
	task.SetMontageInput(model.MontageInput{
		Brief:       "make a launch video",
		PipelineKey: "social-short",
		Preferences: model.MontagePreferences{
			DurationSeconds: 30,
		},
	})

	if err := writeMontageInputJSON(workDir, task); err != nil {
		t.Fatalf("writeMontageInputJSON: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workDir, "montage-input.json"))
	if err != nil {
		t.Fatalf("read montage-input.json: %v", err)
	}
	var got model.MontageInput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal montage-input.json: %v", err)
	}
	if got.Brief != "make a launch video" {
		t.Fatalf("Brief = %q, want make a launch video", got.Brief)
	}
	if got.PipelineKey != "social-short" {
		t.Fatalf("PipelineKey = %q, want social-short", got.PipelineKey)
	}
	if got.Preferences.DurationSeconds != 30 {
		t.Fatalf("Preferences = %+v, want duration 30", got.Preferences)
	}
	if strings.Contains(string(data), "aspect_ratio") {
		t.Fatalf("montage-input.json contains non-authoritative aspect ratio: %s", data)
	}
}

func TestMontageRuntimeManifestFilesAreWrittenWithoutSecrets(t *testing.T) {
	workDir := t.TempDir()
	task := &model.Task{Type: model.PlatformMontage}
	opts := &ExecutionOptions{
		Task: task,
		MontageEnv: map[string]string{
			"NEW_PROVIDER_TOKEN": "future-secret",
		},
		MontageToolPolicy: map[string]srvconfig.MontageToolCapabilityPolicy{
			"video_generation": {Preferred: []string{"fal"}},
		},
		MontagePipelineDefaults: map[string]map[string]any{
			"cinematic": {"budget_usd": 2.0, "video_generation": "auto"},
		},
	}

	if err := writeMontageRuntimeFiles(workDir, opts); err != nil {
		t.Fatalf("writeMontageRuntimeFiles: %v", err)
	}

	policy := readRepoFile(t, filepath.Join(workDir, "montage-tool-policy.json"))
	if !strings.Contains(policy, "video_generation") || !strings.Contains(policy, "fal") {
		t.Fatalf("montage-tool-policy.json = %s, want configured policy", policy)
	}
	defaults := readRepoFile(t, filepath.Join(workDir, "montage-pipeline-defaults.json"))
	if !strings.Contains(defaults, "cinematic") || !strings.Contains(defaults, "budget_usd") {
		t.Fatalf("montage-pipeline-defaults.json = %s, want configured defaults", defaults)
	}
	combined := policy + defaults
	if strings.Contains(combined, "future-secret") {
		t.Fatalf("runtime manifests leaked environment secret: %s", combined)
	}
}

func TestMontageEnvForTaskAcceptsFutureKeysAndSkipsEmptyValues(t *testing.T) {
	opts := &ExecutionOptions{
		Task: &model.Task{Type: model.PlatformMontage},
		MontageEnv: map[string]string{
			"NEW_PROVIDER_TOKEN": "future-secret",
			"EMPTY_PROVIDER_KEY": "",
		},
	}

	got := montageEnvForTask(opts)
	if got["NEW_PROVIDER_TOKEN"] != "future-secret" {
		t.Fatalf("env = %#v, want future provider key", got)
	}
	if _, ok := got["EMPTY_PROVIDER_KEY"]; ok {
		t.Fatalf("env = %#v, want empty value omitted", got)
	}
}

func TestMontageRuntimeManifestFilesUseEmptyObjectsWhenUnset(t *testing.T) {
	workDir := t.TempDir()
	opts := &ExecutionOptions{Task: &model.Task{Type: model.PlatformMontage}}

	if err := writeMontageRuntimeFiles(workDir, opts); err != nil {
		t.Fatalf("writeMontageRuntimeFiles: %v", err)
	}
	for _, name := range []string{"montage-tool-policy.json", "montage-pipeline-defaults.json"} {
		data, err := os.ReadFile(filepath.Join(workDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.TrimSpace(string(data)) != "{}" {
			t.Fatalf("%s = %q, want empty JSON object", name, data)
		}
	}
}

func TestMontageTaskFilesRequireFinalVideoAndManifest(t *testing.T) {
	task := &model.Task{Type: model.PlatformMontage}

	t.Run("missing manifest", func(t *testing.T) {
		got := ValidateTaskArtifactsFromTaskFiles(task, []*model.TaskFile{{FileName: "final.mp4"}})
		if got.Valid {
			t.Fatal("ValidateTaskArtifactsFromTaskFiles valid = true, want false")
		}
		if got.Reason != "montage missing required deliverables: delivery-manifest.json" {
			t.Fatalf("Reason = %q, want missing delivery manifest", got.Reason)
		}
	})

	t.Run("accepts final video aliases with manifest", func(t *testing.T) {
		for _, name := range []string{"final.mp4", "final_video.mp4", "final-video.mp4"} {
			t.Run(name, func(t *testing.T) {
				got := ValidateTaskArtifactsFromTaskFiles(task, []*model.TaskFile{
					{FileName: name},
					{FileName: "delivery-manifest.json"},
				})
				if !got.Valid {
					t.Fatalf("ValidateTaskArtifactsFromTaskFiles valid = false, reason=%q missing=%v", got.Reason, got.Missing)
				}
			})
		}
	})
}

func TestMontagePluginContractsAreDistributed(t *testing.T) {
	root := repoRoot(t)
	claudeAgent := readRepoFile(t, filepath.Join(root, "harness", "agents", "montage.md"))
	for _, want := range []string{
		"name: montage",
		"  - montage",
		"  - video-cover-design",
		"montage-input.json",
		"montage-tool-policy.json",
		"montage-pipeline-defaults.json",
		"montage-project.json",
		"Video aspect ratio: <ratio>",
		"$VIDEO_ASPECT_RATIO",
		"不得读取 `montage_input.preferences.aspect_ratio`",
		"output/cover.png",
		"output/cover-quality.json",
		"video-cover-design Skill",
		"delivery_targets",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"/workspace/openmontage",
		"provider_menu_summary",
		"env_keys",
		"delivery-manifest.json",
		"final.mp4",
		"submit_agent_feedback",
	} {
		if !strings.Contains(claudeAgent, want) {
			t.Fatalf("claudecode montage agent missing %q", want)
		}
	}
	codexAgent := readRepoFile(t, filepath.Join(root, "harness", "agents", "montage.toml"))
	for _, want := range []string{
		`name = "montage"`,
		"montage-input.json",
		"montage-tool-policy.json",
		"montage-pipeline-defaults.json",
		"montage-project.json",
		"Video aspect ratio: <ratio>",
		"$VIDEO_ASPECT_RATIO",
		"不得读取 montage_input.preferences.aspect_ratio",
		"output/cover.png",
		"output/cover-quality.json",
		"video-cover-design Skill",
		"delivery_targets",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"/workspace/openmontage",
		"provider_menu_summary",
		"delivery-manifest.json",
		"final_video",
		`submit_agent_feedback(agent_name=\"montage\"`,
		"__PLUGIN_ROOT__/skills/montage/SKILL.md",
		"__PLUGIN_ROOT__/skills/video-cover-design/SKILL.md",
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex montage agent missing %q", want)
		}
	}
	if strings.Contains(codexAgent, "provider_env") {
		t.Fatal("codex montage agent must use env_keys without the legacy provider_env name")
	}

	reg := readRepoFile(t, filepath.Join(root, "harness", "install", "agents-registration.toml"))
	for _, want := range []string{
		"[agents.montage]",
		"montage.toml",
		"视频生成（业务需求 + 素材 → 成片与交付清单）",
	} {
		if !strings.Contains(reg, want) {
			t.Fatalf("codex agents registration missing %q", want)
		}
	}
}

func TestVideoPackBrandingKeepsLegacyDiscoveryAliases(t *testing.T) {
	root := repoRoot(t)
	requiredKeywords := []string{"Montage", "Hypit", "视频生成", "视频复刻"}
	assertValues := func(path string, actual, expected []string) {
		t.Helper()
		if len(actual) != len(expected) {
			t.Fatalf("%s values = %v, want %v", path, actual, expected)
		}
		for index, value := range expected {
			if actual[index] != value {
				t.Fatalf("%s values = %v, want %v", path, actual, expected)
			}
		}
	}
	assertKeywords := func(path string, actual []string) {
		t.Helper()
		for _, keyword := range requiredKeywords {
			found := false
			for _, candidate := range actual {
				if candidate == keyword {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s missing keyword %q", path, keyword)
			}
		}
	}

	for _, relativePath := range []string{".claude-plugin/plugin.json", ".codex-plugin/plugin.json"} {
		var manifest struct {
			Description string   `json:"description"`
			Keywords    []string `json:"keywords"`
			Interface   struct {
				LongDescription string `json:"longDescription"`
			} `json:"interface"`
		}
		body := readRepoFile(t, filepath.Join(root, "harness", filepath.FromSlash(relativePath)))
		if err := json.Unmarshal([]byte(body), &manifest); err != nil {
			t.Fatalf("parse %s: %v", relativePath, err)
		}
		if !strings.Contains(manifest.Description, "video generation") || !strings.Contains(manifest.Description, "video replication") {
			t.Fatalf("%s description = %q", relativePath, manifest.Description)
		}
		assertKeywords(relativePath, manifest.Keywords)
		if strings.HasSuffix(relativePath, ".codex-plugin/plugin.json") && !strings.Contains(manifest.Interface.LongDescription, "video generation and video replication") {
			t.Fatalf("Codex longDescription = %q", manifest.Interface.LongDescription)
		}
	}

	var marketplace struct {
		Description string `json:"description"`
		Plugins     []struct {
			Description string   `json:"description"`
			Keywords    []string `json:"keywords"`
		} `json:"plugins"`
	}
	marketplaceBody := readRepoFile(t, filepath.Join(root, "harness", ".claude-plugin", "marketplace.json"))
	if err := json.Unmarshal([]byte(marketplaceBody), &marketplace); err != nil {
		t.Fatalf("parse Claude marketplace: %v", err)
	}
	if !strings.Contains(marketplace.Description, "video generation") || !strings.Contains(marketplace.Description, "video replication") {
		t.Fatalf("Claude marketplace header description = %q", marketplace.Description)
	}
	if len(marketplace.Plugins) != 1 {
		t.Fatalf("Claude marketplace plugin count = %d, want 1", len(marketplace.Plugins))
	}
	if !strings.Contains(marketplace.Plugins[0].Description, "video generation") || !strings.Contains(marketplace.Plugins[0].Description, "video replication") {
		t.Fatalf("Claude marketplace description = %q", marketplace.Plugins[0].Description)
	}
	assertKeywords("Claude marketplace", marketplace.Plugins[0].Keywords)

	type codexRegistrationEntry struct {
		Description        string   `toml:"description"`
		NicknameCandidates []string `toml:"nickname_candidates"`
	}
	var registration struct {
		Agents map[string]toml.Primitive `toml:"agents"`
	}
	registrationBody := readRepoFile(t, filepath.Join(root, "harness", "install", "agents-registration.toml"))
	if _, err := toml.Decode(registrationBody, &registration); err != nil {
		t.Fatalf("parse Codex agent registration: %v", err)
	}

	for _, test := range []struct {
		id          string
		displayName string
		candidates  []string
	}{
		{id: "montage", displayName: "视频生成", candidates: []string{"VideoGenerator", "VideoGen", "Montage", "视频生成"}},
		{id: "hypit", displayName: "视频复刻", candidates: []string{"VideoReplica", "Hypit", "视频复刻"}},
	} {
		packPath := filepath.Join("packs", test.id, "agent-pack.yaml")
		var pack struct {
			DisplayName string `yaml:"display_name"`
		}
		packBody := readRepoFile(t, filepath.Join(root, "harness", packPath))
		if err := yaml.Unmarshal([]byte(packBody), &pack); err != nil {
			t.Fatalf("parse %s: %v", packPath, err)
		}
		if pack.DisplayName != test.displayName {
			t.Fatalf("%s display_name = %q, want %q", packPath, pack.DisplayName, test.displayName)
		}

		for _, relativePath := range []string{
			filepath.Join("packs", test.id, "agent.codex.toml"),
			filepath.Join("agents", test.id+".toml"),
		} {
			var codexAgent struct {
				Name               string   `toml:"name"`
				Description        string   `toml:"description"`
				NicknameCandidates []string `toml:"nickname_candidates"`
			}
			body := readRepoFile(t, filepath.Join(root, "harness", relativePath))
			if _, err := toml.Decode(body, &codexAgent); err != nil {
				t.Fatalf("parse %s: %v", relativePath, err)
			}
			if codexAgent.Name != test.id || !strings.Contains(codexAgent.Description, test.displayName) {
				t.Fatalf("%s identity = %q/%q", relativePath, codexAgent.Name, codexAgent.Description)
			}
			assertValues(relativePath, codexAgent.NicknameCandidates, test.candidates)
		}

		primitive, ok := registration.Agents[test.id]
		if !ok {
			t.Fatalf("Codex registration missing agent %s", test.id)
		}
		var agent codexRegistrationEntry
		if err := toml.PrimitiveDecode(primitive, &agent); err != nil {
			t.Fatalf("decode Codex registration for %s: %v", test.id, err)
		}
		if !strings.Contains(agent.Description, test.displayName) {
			t.Fatalf("Codex registration description for %s = %q", test.id, agent.Description)
		}
		assertValues("Codex registration for "+test.id, agent.NicknameCandidates, test.candidates)

		for _, relativePath := range []string{
			filepath.Join("packs", test.id, "agent.claude.md"),
			filepath.Join("agents", test.id+".md"),
		} {
			path := filepath.Join(root, "harness", relativePath)
			frontmatter := parseSkillFrontmatter(t, path, readRepoFile(t, path))
			if frontmatterStringValue(frontmatter["name"]) != test.id || !strings.Contains(frontmatterStringValue(frontmatter["description"]), test.displayName) {
				t.Fatalf("%s Claude identity = %#v", relativePath, frontmatter)
			}
		}
	}
}

func TestMontageSkillMirrorsStayInSync(t *testing.T) {
	root := repoRoot(t)
	canonical := readRepoFile(t, filepath.Join(root, "harness", "skills", "montage", "SKILL.md"))
	for _, distro := range []string{"harness"} {
		path := filepath.Join(root, distro, "skills", "montage", "SKILL.md")
		if got := readRepoFile(t, path); got != canonical {
			t.Fatalf("%s must match claudecode montage skill", path)
		}
	}
	for _, want := range []string{
		"name: montage",
		"montage-input.json",
		"montage-tool-policy.json",
		"montage-pipeline-defaults.json",
		"montage-project.json",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"/workspace/openmontage",
		"output/montage-project.json",
		`"output_dir": "output"`,
		"provider_menu_summary",
		"Secrets only arrive through environment variables",
		"env_keys",
		"delivery-manifest.json",
		"immutable image template at `/opt/montage-template`",
		"Do not modify the immutable image template",
	} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("montage skill missing %q", want)
		}
	}
	if strings.Contains(canonical, "provider_env") {
		t.Fatal("montage skill must use env_keys without the legacy provider_env name")
	}
	for _, forbidden := range []string{
		"/workspace/montage",
		"third_party/OpenMontage",
		"workspace preparation",
	} {
		if strings.Contains(canonical, forbidden) {
			t.Fatalf("managed montage skill contains obsolete runtime contract %q", forbidden)
		}
	}

	examples := readRepoFile(t, filepath.Join(root, "harness", "skills", "montage", "references", "examples.md"))
	for _, want := range []string{
		"output/montage-project.json",
		"output/delivery-manifest.json",
		"output/failure-diagnosis.md",
	} {
		if !strings.Contains(examples, want) {
			t.Fatalf("montage examples missing managed artifact path %q", want)
		}
	}
	for _, forbidden := range []string{
		"/workspace/montage",
		"configured submodule or runner",
		"workspace and task-file tools",
	} {
		if strings.Contains(examples, forbidden) {
			t.Fatalf("montage examples contain obsolete runtime contract %q", forbidden)
		}
	}
}

func TestMontageManagedApprovalPolicy(t *testing.T) {
	root := repoRoot(t)
	agentText := readRepoFile(t, filepath.Join(root, "harness", "agents", "montage.md"))
	skillText := readRepoFile(t, filepath.Join(root, "harness", "skills", "montage", "SKILL.md"))
	for _, want := range []string{
		`"approval_policy"`,
		`"mode": "auto"`,
		`"source": "anban_managed_task"`,
		`"scope": "full_run"`,
		"自动批准常规 creative gate",
		"不得跳过 checkpoint",
	} {
		if !strings.Contains(agentText+skillText, want) {
			t.Fatalf("Montage autonomous contract missing %q", want)
		}
	}
}

func TestMontagePluginManifestsAdvertiseSupport(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		filepath.Join(root, "harness", ".claude-plugin", "plugin.json"),
		filepath.Join(root, "harness", ".codex-plugin", "plugin.json"),
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "video generation") || !strings.Contains(body, "video replication") {
			t.Fatalf("%s must advertise video generation and replication support", path)
		}
	}
}

func TestMontageRuntimeSourceIsNotAParentSubmodule(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	if strings.Contains(gitmodules, "third_party/OpenMontage") {
		t.Fatal(".gitmodules retains third_party/OpenMontage submodule")
	}
	dockerfile := readRepoFile(t, filepath.Join(root, "deploy/docker/Dockerfile.agent-montage"))
	if !strings.Contains(dockerfile, "https://github.com/calesthio/OpenMontage.git") {
		t.Fatal("Montage Dockerfile missing OpenMontage upstream URL")
	}
}
