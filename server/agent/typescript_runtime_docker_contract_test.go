package agent

import (
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
		{name: "Dockerfile.agent-article", from: []string{"FROM node:bookworm-slim AS builder", "FROM node:bookworm-slim"}},
		{name: "Dockerfile.agent-seednote", from: []string{"FROM node:bookworm-slim AS builder", "FROM node:bookworm-slim"}},
		{name: "Dockerfile.agent-montage", from: []string{"FROM node:24-bookworm-slim AS builder", "FROM node:24-bookworm-slim"}},
	} {
		data := readTextFile(t, filepath.Join(root, "deploy", "docker", runtime.name))
		if got := dockerfileFromInstructions(data); !reflect.DeepEqual(got, runtime.from) {
			t.Fatalf("%s FROM instructions mismatch\nwant: %v\n got: %v", runtime.name, runtime.from, got)
		}
		for _, want := range []string{
			"agent-ts/package.json agent-ts/package-lock.json",
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
	if !strings.Contains(launcher, "exec node /app/anban-agent/dist/main.js \"$@\"") {
		t.Fatal("TypeScript anban launcher must exec the compiled Node entrypoint")
	}
}

func TestTypeScriptRuntimeDockerfilesShareLockedSDKWithOptionalPackages(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		body := readTextFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, required := range []string{
			"COPY agent-ts/package.json agent-ts/package-lock.json ./",
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
