package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTypeScriptRuntimeDockerfilesUseBundledAgentSDK(t *testing.T) {
	root := repositoryRoot(t)
	for _, runtime := range []struct {
		name string
		from []string
	}{
		{name: "Dockerfile.agent-wechat-article", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
		{name: "Dockerfile.agent-wechat-picture", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
		{name: "Dockerfile.agent-moments", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
		{name: "Dockerfile.agent-ecommerce", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
		{name: "Dockerfile.agent-seednote", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
		{name: "Dockerfile.agent-montage", from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"}},
	} {
		data := readTextFile(t, filepath.Join(root, "deploy", "docker", runtime.name))
		if got := dockerfileFromInstructions(data); !reflect.DeepEqual(got, runtime.from) {
			t.Fatalf("%s FROM instructions mismatch\nwant: %v\n got: %v", runtime.name, runtime.from, got)
		}
		for _, want := range []string{
			"runtime-claude/package.json runtime-claude/package-lock.json",
			"npm ci",
			"@anthropic-ai/claude-agent-sdk",
			"COPY harness/ /anbanai/",
			"COPY deploy/docker/anban-agent-launcher /usr/local/bin/anban",
			"ENTRYPOINT [\"tini\", \"--\", \"anban\"]",
			"USER 1000:1000",
		} {
			if !strings.Contains(data, want) {
				t.Fatalf("%s missing TypeScript runtime contract %q", runtime.name, want)
			}
		}
		for _, forbidden := range []string{"FROM golang:", "@anthropic-ai/claude-code", "claude plugin marketplace", "claude plugin install", "ANBAN_HOME_TEMPLATE"} {
			if strings.Contains(data, forbidden) {
				t.Fatalf("%s retains obsolete Go/Claude CLI bootstrap %q", runtime.name, forbidden)
			}
		}
	}
	launcher := readTextFile(t, filepath.Join(root, "deploy", "docker", "anban-agent-launcher"))
	if !strings.Contains(launcher, "exec node /app/runtime-claude/dist/main.js \"$@\"") {
		t.Fatal("TypeScript anban launcher must exec the compiled Node entrypoint")
	}
}

func TestManagedAgentImagesUsePinnedNode(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"Dockerfile.agent-ecommerce",
		"Dockerfile.agent-feedback-analysis",
		"Dockerfile.agent-hypit",
		"Dockerfile.agent-moments",
		"Dockerfile.agent-montage",
		"Dockerfile.agent-profile-analysis",
		"Dockerfile.agent-seednote",
		"Dockerfile.agent-wechat-article",
		"Dockerfile.agent-wechat-picture",
		"Dockerfile.agent-whiteboard-animation",
	} {
		body := readTextFile(t, filepath.Join(root, "deploy", "docker", name))
		foundNodeImage := false
		for _, instruction := range dockerfileFromInstructions(body) {
			fields := strings.Fields(instruction)
			if len(fields) < 2 || !strings.HasPrefix(fields[1], "node:") {
				continue
			}
			foundNodeImage = true
			if !strings.HasPrefix(fields[1], "node:24.22.0-bookworm-slim") {
				t.Errorf("%s uses Node image %q; want node:24.22.0-bookworm-slim", name, fields[1])
			}
		}
		if !foundNodeImage {
			t.Errorf("%s has no Node base image", name)
		}
	}
}

func TestClaudeRuntimeUsesPinnedNodeLTS(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{
		filepath.Join(root, "runtime-claude", "package.json"),
		filepath.Join(root, "runtime-claude", "package-lock.json"),
	} {
		body := readTextFile(t, path)
		if !strings.Contains(body, ">=24.22.0 <25") {
			t.Fatalf("%s must require Node >=24.22.0 <25", path)
		}
	}

	ci := readTextFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	claudeJob, _, found := strings.Cut(ci, "  dsh-runtime:")
	if !found || !strings.Contains(claudeJob, "node-version: \"24.22.0\"") {
		t.Fatal("runtime-claude CI must use Node 24.22.0")
	}
}

func TestTypeScriptRuntimeDockerfilesShareLockedSDKWithOptionalPackages(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-wechat-article", "Dockerfile.agent-wechat-picture", "Dockerfile.agent-moments", "Dockerfile.agent-ecommerce", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		body := readTextFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, required := range []string{
			"COPY runtime-claude/package.json runtime-claude/package-lock.json ./",
			"RUN npm ci",
			"npm prune --omit=dev",
			"test -d node_modules/@anthropic-ai/claude-agent-sdk",
		} {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing locked SDK install contract %q", name, required)
			}
		}
		for _, forbidden := range []string{"--omit=optional", "--no-optional", "CLAUDE_CODE_VERSION", "@anthropic-ai/claude-agent-sdk@"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s overrides the shared SDK dependency contract with %q", name, forbidden)
			}
		}
	}
}

func TestRuntimePackageDependencyBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	claudePackage, err := os.ReadFile(filepath.Join(root, "runtime-claude", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	dshPackage, err := os.ReadFile(filepath.Join(root, "runtime-dsh", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(claudePackage), "@deepseek-ai/") || strings.Contains(string(claudePackage), "dsh-bridge") {
		t.Fatal("runtime-claude must not declare DSH or interactive Bridge dependencies")
	}
	if strings.Contains(string(dshPackage), "@anthropic-ai/") || strings.Contains(string(dshPackage), "claude-agent") {
		t.Fatal("runtime-dsh must not declare Claude runtime dependencies")
	}
}

func TestDshRuntimeUsesPinnedNodeLts(t *testing.T) {
	root := repositoryRoot(t)
	packageBody := readTextFile(t, filepath.Join(root, "runtime-dsh", "package.json"))
	var manifest struct {
		Engines map[string]string `json:"engines"`
	}
	if err := json.Unmarshal([]byte(packageBody), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Engines["node"] != ">=24.22.0 <25" {
		t.Fatalf("runtime-dsh Node engine = %q, want >=24.22.0 <25", manifest.Engines["node"])
	}

	dockerfile := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.runtime-dsh"))
	for _, want := range []string{
		"FROM node:24.22.0-alpine3.24 AS builder",
		"FROM node:24.22.0-alpine3.24",
		"USER 1000:1000",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("server Dockerfile missing pinned DSH Node LTS contract %q", want)
		}
	}
	server := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.server"))
	if strings.Contains(server, "runtime-dsh/") || strings.Contains(server, "dsh-builder") {
		t.Fatal("Server image must not embed the independent DSH runtime")
	}

	ci := readTextFile(t, filepath.Join(root, ".github/workflows/ci.yml"))
	if !strings.Contains(ci, "node-version: \"24.22.0\"") {
		t.Fatal("CI must test TypeScript runtimes with the pinned Node LTS")
	}
}
