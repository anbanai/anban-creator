package agentpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestLoadCatalogValidatesAndResolvesManagedPack(t *testing.T) {
	root := writePackFixture(t, `
id: demo-pack
version: 1.0.0
kind: managed
display_name: Demo Pack
description: Demo managed workflow
agent:
  name: demo
  claude_source: agent.claude.md
  codex_source: agent.codex.toml
  skills: [demo-skill]
  max_turns: 60
bindings:
  project_platforms: [article]
  task_types: [demo-task]
runtime:
  profile: article
  adapter: standard
  max_turns: 40
surfaces: [project, task, plan]
billing_operations:
  demo-task: task.demo
progress:
  - id: prepare
    title: Prepare
artifacts:
  - role: final
    path: output/final.md
    mime_type: text/markdown
    required: true
`)

	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	pack, ok := catalog.Pack("demo-pack")
	if !ok {
		t.Fatal("demo-pack missing")
	}
	if pack.Agent.Name != "demo" || pack.Runtime.Profile != "article" || pack.Runtime.Adapter != AdapterStandard {
		t.Fatalf("resolved pack = %#v", pack)
	}
	resolved, ok := catalog.ForTaskType("demo-task")
	if !ok || resolved.ID != "demo-pack" {
		t.Fatalf("ForTaskType = %#v, %v", resolved, ok)
	}
	if len(pack.Digest) != 64 {
		t.Fatalf("digest = %q", pack.Digest)
	}
	if pack.BillingOperations["demo-task"] != "task.demo" {
		t.Fatalf("billing operations = %#v", pack.BillingOperations)
	}
	if operation, ok := catalog.BillingOperation("demo-task"); !ok || operation != "task.demo" {
		t.Fatalf("BillingOperation = %q, %v", operation, ok)
	}
}

func TestLoadCatalogRejectsInvalidPackContracts(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "non kebab id", manifest: strings.ReplaceAll(validFixtureManifest, "demo-pack", "demo_pack"), want: "kebab-case"},
		{name: "unknown adapter", manifest: strings.ReplaceAll(validFixtureManifest, "adapter: standard", "adapter: remote"), want: "unsupported runtime adapter"},
		{name: "missing skill", manifest: strings.ReplaceAll(validFixtureManifest, "demo-skill", "missing-skill"), want: "missing Skill"},
		{name: "plugin task binding", manifest: strings.Replace(validFixtureManifest, "kind: managed", "kind: plugin", 1), want: "plugin Pack must not bind task types"},
		{name: "product surface without billing", manifest: strings.Replace(validFixtureManifest, "billing_operations:\n  demo-task: task.demo\n", "", 1), want: "product surfaces require billing operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackFixture(t, tt.manifest)
			_, err := LoadCatalog(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogAllowsOptionalDSHSource(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "omitted", manifest: validFixtureManifest},
		{
			name:     "valid composition",
			manifest: strings.Replace(validFixtureManifest, "  skills: [demo-skill]", "  dsh_source: agent.dsh.yml\n  skills: [demo-skill]", 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackFixture(t, tt.manifest)
			catalog, err := LoadCatalog(root)
			if err != nil {
				t.Fatalf("LoadCatalog: %v", err)
			}
			_, ok := catalog.Pack("demo-pack")
			if !ok {
				t.Fatal("demo-pack missing")
			}
		})
	}
}

func TestLoadCatalogRejectsInvalidDSH(t *testing.T) {
	dshManifest := strings.Replace(validFixtureManifest, "  skills: [demo-skill]", "  dsh_source: agent.dsh.yml\n  skills: [demo-skill]", 1)
	tests := []struct {
		name     string
		manifest string
		body     string
		want     string
	}{
		{name: "absolute path", manifest: strings.Replace(dshManifest, "agent.dsh.yml", "/tmp/agent.dsh.yml", 1), want: "path must be relative"},
		{name: "traversal path", manifest: strings.Replace(dshManifest, "agent.dsh.yml", "../agent.dsh.yml", 1), want: "path escapes Pack directory"},
		{name: "missing file", manifest: strings.Replace(dshManifest, "agent.dsh.yml", "missing.dsh.yml", 1), want: "no such file"},
		{name: "mapping root", manifest: dshManifest, body: "persona:\n  name: '@deepseek-ai/dsh-persona'\n", want: "root must be a sequence"},
		{name: "row without id", manifest: dshManifest, body: "- name: '@deepseek-ai/dsh-persona'\n", want: "id is required"},
		{name: "row without name", manifest: dshManifest, body: "- id: persona\n", want: "name is required"},
		{name: "duplicate id key", manifest: dshManifest, body: "- id: persona\n  id: persona-other\n  name: '@deepseek-ai/dsh-persona'\n", want: "duplicate id key"},
		{name: "duplicate name key", manifest: dshManifest, body: "- id: persona\n  name: '@deepseek-ai/dsh-persona'\n  name: '@deepseek-ai/dsh-persona-other'\n", want: "duplicate name key"},
		{name: "duplicate row id", manifest: dshManifest, body: "- id: persona\n  name: first\n- id: persona\n  name: second\n", want: `duplicate id "persona"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackFixture(t, tt.manifest)
			if tt.body != "" {
				path := filepath.Join(root, "packs", "demo-pack", "agent.dsh.yml")
				if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadCatalog(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadCatalog error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogDSHSourceAffectsDigest(t *testing.T) {
	manifest := strings.Replace(validFixtureManifest, "  skills: [demo-skill]", "  dsh_source: agent.dsh.yml\n  skills: [demo-skill]", 1)
	root := writePackFixture(t, manifest)
	first, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog first: %v", err)
	}

	path := filepath.Join(root, "packs", "demo-pack", "agent.dsh.yml")
	changed := strings.Replace(validDSHComposition, "text: Demo", "text: Changed", 1)
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog second: %v", err)
	}
	if first.Packs[0].Digest == second.Packs[0].Digest {
		t.Fatalf("Pack digest did not change after DSH source changed: %s", first.Packs[0].Digest)
	}
}

func TestLoadCatalogRejectsDuplicateTaskTypeBindings(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	packDir := filepath.Join(root, "packs", "other-pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(validFixtureManifest, "id: demo-pack", "id: other-pack", 1)
	manifest = strings.Replace(manifest, "name: demo", "name: other", 1)
	manifest = strings.Replace(manifest, "project_platforms: [article]", "project_platforms: [seednote]", 1)
	for name, content := range map[string]string{
		"agent-pack.yaml":  manifest,
		"agent.claude.md":  "---\nname: other\nmaxTurns: 60\n---\n\n# Other Claude\n",
		"agent.codex.toml": "name = \"other\"\n",
	} {
		if err := os.WriteFile(filepath.Join(packDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := LoadCatalog(root)
	if err == nil || !strings.Contains(err.Error(), `task type "demo-task" is bound by both`) {
		t.Fatalf("LoadCatalog error = %v, want duplicate task type rejection", err)
	}
}

func TestLoadCatalogRejectsDuplicateAgentNames(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	packDir := filepath.Join(root, "packs", "other-pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(validFixtureManifest, "id: demo-pack", "id: other-pack", 1)
	manifest = strings.Replace(manifest, "project_platforms: [article]", "project_platforms: [seednote]", 1)
	manifest = strings.Replace(manifest, "task_types: [demo-task]", "task_types: [other-task]", 1)
	manifest = strings.Replace(manifest, "demo-task: task.demo", "other-task: task.other", 1)
	for name, content := range map[string]string{
		"agent-pack.yaml":  manifest,
		"agent.claude.md":  "---\nname: demo\nmaxTurns: 60\n---\n",
		"agent.codex.toml": "name = \"demo\"\n",
	} {
		if err := os.WriteFile(filepath.Join(packDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), `agent name "demo" is declared by both`) {
		t.Fatalf("LoadCatalog error = %v, want duplicate Agent name rejection", err)
	}
}

func TestLoadCatalogRejectsNativeAgentIdentityDrift(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		body    string
		wantErr string
	}{
		{name: "Claude name", file: "agent.claude.md", body: "---\nname: other\nmaxTurns: 60\n---\n", wantErr: "Claude Agent name"},
		{name: "Claude max turns", file: "agent.claude.md", body: "---\nname: demo\nmaxTurns: 61\n---\n", wantErr: "Claude Agent maxTurns"},
		{name: "Codex name", file: "agent.codex.toml", body: "name = \"other\"\n", wantErr: "Codex Agent name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackFixture(t, validFixtureManifest)
			path := filepath.Join(root, "packs", "demo-pack", tt.file)
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadCatalog error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateProducesDeterministicNativeAgentsAndCatalog(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	out := filepath.Join(t.TempDir(), "generated")

	first, err := Generate(root, out)
	if err != nil {
		t.Fatalf("Generate first: %v", err)
	}
	second, err := Generate(root, out)
	if err != nil {
		t.Fatalf("Generate second: %v", err)
	}
	if first.CatalogDigest != second.CatalogDigest || second.Changed {
		t.Fatalf("generation is not deterministic: first=%#v second=%#v", first, second)
	}

	assertFileContent(t, filepath.Join(out, "agents", "demo.md"), "---\nname: demo\nmaxTurns: 60\n---\n\n# Demo Claude\n")
	assertFileContent(t, filepath.Join(out, "agents", "demo.toml"), "name = \"demo\"\n")
	catalogJSON, err := os.ReadFile(filepath.Join(out, "catalog.generated.json"))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(catalogJSON), `"id": "demo-pack"`) {
		t.Fatalf("generated catalog = %s", catalogJSON)
	}
}

func TestLoadCatalogDigestIgnoresSkillGitMetadata(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	gitMetadata := filepath.Join(root, "skills", "demo-skill", ".git")
	if err := os.WriteFile(gitMetadata, []byte("gitdir: /tmp/checkout-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog first: %v", err)
	}

	if err := os.WriteFile(gitMetadata, []byte("gitdir: /tmp/checkout-two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog second: %v", err)
	}

	if first.Packs[0].Digest != second.Packs[0].Digest {
		t.Fatalf("digest depends on Skill .git metadata: first=%s second=%s", first.Packs[0].Digest, second.Packs[0].Digest)
	}
}

func TestPackDigestIncludesEveryReferencedSkillFile(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	scriptDir := filepath.Join(root, "skills", "demo-skill", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptDir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("echo first\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	firstPack, _ := first.Pack("demo-pack")

	if err := os.WriteFile(scriptPath, []byte("echo second\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	secondPack, _ := second.Pack("demo-pack")
	if firstPack.Digest == secondPack.Digest {
		t.Fatalf("Pack digest did not change after referenced Skill script changed: %s", firstPack.Digest)
	}
}

func TestRepositoryAgentPacksCoverCurrentNativeAgentsAndManagedRoutes(t *testing.T) {
	pluginRoot := filepath.Clean(filepath.Join("..", "..", "plugins"))
	catalog, err := LoadCatalog(pluginRoot)
	if err != nil {
		t.Fatalf("LoadCatalog repository Packs: %v", err)
	}
	if len(catalog.Packs) != 6 {
		t.Fatalf("Pack count = %d, want 6", len(catalog.Packs))
	}

	wantRoutes := map[string]struct {
		packID  string
		profile string
		adapter string
	}{
		"article":        {packID: "article", profile: "article", adapter: AdapterStandard},
		"seednote":       {packID: "seednote", profile: "seednote", adapter: AdapterStandard},
		"viral_analysis": {packID: "seednote", profile: "seednote", adapter: AdapterStandard},
		"moments":        {packID: "moments", profile: "article", adapter: AdapterStandard},
		"ecommerce":      {packID: "ecommerce", profile: "article", adapter: AdapterStandard},
		"montage":        {packID: "montage", profile: "montage", adapter: AdapterOpenMontage},
		"live-slicer":    {packID: "live-slicer", profile: "montage", adapter: AdapterStandard},
	}
	for taskType, want := range wantRoutes {
		pack, ok := catalog.ForTaskType(taskType)
		if !ok {
			t.Errorf("task type %q has no Agent Pack", taskType)
			continue
		}
		if pack.ID != want.packID || pack.Runtime.Profile != want.profile || pack.Runtime.Adapter != want.adapter {
			t.Errorf("task type %q resolved to %s/%s/%s, want %s/%s/%s", taskType, pack.ID, pack.Runtime.Profile, pack.Runtime.Adapter, want.packID, want.profile, want.adapter)
		}
	}
	if pack, ok := catalog.ForProjectPlatform("montage"); !ok || pack.ID != "montage" {
		t.Fatalf("montage project Pack = %#v, %v", pack, ok)
	}
	if _, ok := catalog.Pack("designer"); ok {
		t.Fatal("removed Designer Pack is still present")
	}
	for _, pack := range catalog.Packs {
		for _, platform := range pack.Bindings.ProjectPlatforms {
			if !model.IsProjectPlatform(platform) {
				t.Errorf("Pack %q binds undeclared project platform %q", pack.ID, platform)
			}
		}
		for _, taskType := range pack.Bindings.TaskTypes {
			if !model.IsTaskType(taskType) {
				t.Errorf("Pack %q binds undeclared task type %q", pack.ID, taskType)
			}
		}
	}
}

func TestEmbeddedCatalogResolvesCurrentManagedRoutes(t *testing.T) {
	catalog := Default()
	for _, taskType := range []string{"article", "seednote", "viral_analysis", "moments", "ecommerce", "montage", "live-slicer"} {
		if _, ok := catalog.ForTaskType(taskType); !ok {
			t.Errorf("embedded Catalog has no route for %q", taskType)
		}
	}
}

func TestCheckRepositoryDetectsGeneratedDrift(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	catalogPath := filepath.Join(t.TempDir(), "catalog.generated.json")
	if _, err := GenerateRepository(root, catalogPath); err != nil {
		t.Fatalf("GenerateRepository: %v", err)
	}
	if err := CheckRepository(root, catalogPath); err != nil {
		t.Fatalf("CheckRepository clean: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "demo.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepository(root, catalogPath); err == nil || !strings.Contains(err.Error(), "generated Agent drift") {
		t.Fatalf("CheckRepository drift error = %v", err)
	}
}

func TestCheckRepositoryDetectsRuntimeCatalogDrift(t *testing.T) {
	root := writePackFixture(t, validFixtureManifest)
	catalogPath := filepath.Join(t.TempDir(), "catalog.generated.json")
	if _, err := GenerateRepository(root, catalogPath); err != nil {
		t.Fatalf("GenerateRepository: %v", err)
	}
	runtimeCatalog := filepath.Join(root, "agent-pack-catalog.json")
	if err := os.WriteFile(runtimeCatalog, []byte(`{"packs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepository(root, catalogPath); err == nil || !strings.Contains(err.Error(), runtimeCatalog) {
		t.Fatalf("CheckRepository error = %v, want runtime Catalog drift", err)
	}
}

func TestScaffoldCreatesMinimalManagedPackWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	options := ScaffoldOptions{
		ID: "new-scene", Kind: KindManaged, TaskType: "new-scene",
		RuntimeProfile: "article", Adapter: AdapterStandard,
	}
	if err := Scaffold(root, options); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	manifestPath := filepath.Join(root, "packs", "new-scene", "agent-pack.yaml")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read scaffold manifest: %v", err)
	}
	for _, want := range []string{"id: new-scene", "kind: managed", "task_types: [new-scene]", "profile: article", "adapter: standard"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("scaffold manifest missing %q:\n%s", want, body)
		}
	}
	if err := Scaffold(root, options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Scaffold error = %v", err)
	}
}

func TestValidateTaskInputUsesPackJSONSchema(t *testing.T) {
	manifest := strings.Replace(validFixtureManifest, "runtime:\n", "schemas:\n  task_input: task-input.schema.json\nruntime:\n", 1)
	root := writePackFixture(t, manifest)
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	pack, _ := catalog.Pack("demo-pack")
	if err := ValidateTaskInput(pack, map[string]any{"tone": "concise", "count": float64(2)}); err != nil {
		t.Fatalf("valid task input: %v", err)
	}
	for _, input := range []map[string]any{
		{"count": float64(2)},
		{"tone": "verbose", "count": float64(2)},
		{"tone": "concise", "count": float64(2), "unknown": true},
	} {
		if err := ValidateTaskInput(pack, input); err == nil {
			t.Errorf("invalid task input accepted: %#v", input)
		}
	}
}

func TestLoadCatalogRejectsUnsupportedExtensionSchemaContracts(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		manifest string
		want     string
	}{
		{
			name:     "root type is required",
			schema:   `{"properties": {}}`,
			manifest: validFixtureManifest,
			want:     `root type must be "object"`,
		},
		{
			name:     "unknown JSON Schema keyword",
			schema:   `{"type":"object","properties":{},"oneOf":[]}`,
			manifest: validFixtureManifest,
			want:     "unknown field",
		},
		{
			name:     "generic form rejects undeclared keys",
			schema:   `{"type":"object","properties":{"tone":{"type":"string"}}}`,
			manifest: validFixtureManifest,
			want:     "additionalProperties must be false",
		},
		{
			name:     "generic form cannot render nested values",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"filters":{"type":"object","properties":{}}}}`,
			want:     "requires ui.renderer",
			manifest: validFixtureManifest,
		},
		{
			name:     "default must match declared type",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","default":"bad"}}}`,
			want:     "default must be an integer",
			manifest: validFixtureManifest,
		},
		{
			name:     "explicit null default is not treated as absent",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","default":null}}}`,
			want:     "default must be an integer",
			manifest: validFixtureManifest,
		},
		{
			name:     "default must match enum",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"tone":{"type":"string","enum":["concise"],"default":"verbose"}}}`,
			want:     "default is not an allowed value",
			manifest: validFixtureManifest,
		},
		{
			name:     "default must satisfy bounds",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","maximum":5,"default":6}}}`,
			want:     "default must be at most 5",
			manifest: validFixtureManifest,
		},
		{
			name:     "enum member must match declared type",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","enum":[1,"1"]}}}`,
			want:     "enum[1] must be an integer",
			manifest: validFixtureManifest,
		},
		{
			name:     "enum values must be unique after normalization",
			schema:   `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"number","enum":[1,1.0]}}}`,
			want:     "duplicate enum value",
			manifest: validFixtureManifest,
		},
		{
			name:     "required boolean implicit false must be allowed",
			schema:   `{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"type":"boolean","enum":[true]}}}`,
			want:     "required boolean implicit default",
			manifest: validFixtureManifest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := strings.Replace(tt.manifest, "runtime:\n", "schemas:\n  task_input: task-input.schema.json\nruntime:\n", 1)
			root := writePackFixture(t, manifest)
			path := filepath.Join(root, "packs", "demo-pack", "task-input.schema.json")
			if err := os.WriteFile(path, []byte(tt.schema), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadCatalog error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogAllowsNestedExtensionSchemaWithCustomRenderer(t *testing.T) {
	manifest := strings.Replace(validFixtureManifest, "runtime:\n", "schemas:\n  task_input: task-input.schema.json\nui:\n  renderer: custom:demo-pack\nruntime:\n", 1)
	root := writePackFixture(t, manifest)
	path := filepath.Join(root, "packs", "demo-pack", "task-input.schema.json")
	schema := `{"type":"object","additionalProperties":false,"properties":{"filters":{"type":"object","additionalProperties":false,"properties":{"tags":{"type":"array","items":{"type":"string"}}}}}}`
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(root); err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
}

func TestLoadCatalogRejectsOpenCustomRendererSchema(t *testing.T) {
	manifest := strings.Replace(validFixtureManifest, "runtime:\n", "schemas:\n  task_input: task-input.schema.json\nui:\n  renderer: custom:demo-pack\nruntime:\n", 1)
	root := writePackFixture(t, manifest)
	path := filepath.Join(root, "packs", "demo-pack", "task-input.schema.json")
	if err := os.WriteFile(path, []byte(`{"type":"object","properties":{"filters":{"type":"object","properties":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), "additionalProperties must be false") {
		t.Fatalf("LoadCatalog error = %v, want closed custom schema rejection", err)
	}
}

func TestLoadCatalogRejectsSymlinkedSourceParent(t *testing.T) {
	manifest := strings.Replace(validFixtureManifest, "agent.claude.md", "sources/agent.claude.md", 1)
	root := writePackFixture(t, manifest)
	packDir := filepath.Join(root, "packs", "demo-pack")
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "agent.claude.md"), []byte("---\nname: demo\ndescription: Demo managed workflow\ntools: []\nmodel: inherit\nmaxTurns: 60\n---\nPrompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(packDir, "sources")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadCatalog error = %v, want symlinked parent rejection", err)
	}
}

const validFixtureManifest = `
id: demo-pack
version: 1.0.0
kind: managed
display_name: Demo Pack
description: Demo managed workflow
agent:
  name: demo
  claude_source: agent.claude.md
  codex_source: agent.codex.toml
  skills: [demo-skill]
  max_turns: 60
bindings:
  project_platforms: [article]
  task_types: [demo-task]
runtime:
  profile: article
  adapter: standard
  max_turns: 40
surfaces: [project, task, plan]
billing_operations:
  demo-task: task.demo
progress:
  - id: prepare
    title: Prepare
artifacts:
  - role: final
    path: output/final.md
    mime_type: text/markdown
    required: true
`

const validDSHComposition = `
- id: persona
  name: '@deepseek-ai/dsh-persona'
  config:
    text: Demo
- id: tool-bash
  name: '@deepseek-ai/dsh-tool-bash'
  disabled: !!js process.platform === 'win32'
`

func writePackFixture(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	packDir := filepath.Join(root, "packs", "demo-pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "demo-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(packDir, "agent-pack.yaml"):  manifest,
		filepath.Join(packDir, "agent.claude.md"):  "---\nname: demo\nmaxTurns: 60\n---\n\n# Demo Claude\n",
		filepath.Join(packDir, "agent.codex.toml"): "name = \"demo\"\n",
		filepath.Join(packDir, "task-input.schema.json"): `{
  "type": "object",
  "additionalProperties": false,
  "required": ["tone"],
  "properties": {
    "tone": {"type": "string", "enum": ["concise", "detailed"]},
    "count": {"type": "integer", "minimum": 1, "maximum": 5}
  }
}`,
		filepath.Join(root, "skills", "demo-skill", "SKILL.md"): "---\nname: demo-skill\n---\n",
	}
	if strings.Contains(manifest, "dsh_source: agent.dsh.yml") {
		files[filepath.Join(packDir, "agent.dsh.yml")] = validDSHComposition
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
