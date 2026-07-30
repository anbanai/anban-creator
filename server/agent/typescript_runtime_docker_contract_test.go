package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeScriptRuntimeDockerfilesUseBundledAgentSDK(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article-ts", "Dockerfile.agent-seednote-ts", "Dockerfile.agent-montage-ts"} {
		data := readTextFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, want := range []string{
			"node:bookworm-slim",
			"agent-ts/package.json agent-ts/package-lock.json",
			"npm ci",
			"@anthropic-ai/claude-agent-sdk",
			"COPY deploy/docker/anban-ts-launcher /usr/local/bin/anban",
			"ENTRYPOINT [\"tini\", \"--\", \"anban\"]",
			"USER 1000:1000",
		} {
			if !strings.Contains(data, want) {
				t.Fatalf("%s missing TypeScript runtime contract %q", name, want)
			}
		}
		for _, forbidden := range []string{"FROM golang:", "@anthropic-ai/claude-code", "claude plugin marketplace", "claude plugin install", "ANBAN_HOME_TEMPLATE"} {
			if strings.Contains(data, forbidden) {
				t.Fatalf("%s retains obsolete Go/Claude CLI bootstrap %q", name, forbidden)
			}
		}
	}
	launcher := readTextFile(t, filepath.Join(root, "deploy", "docker", "anban-ts-launcher"))
	if !strings.Contains(launcher, "exec node /app/anban-agent/dist/main.js \"$@\"") {
		t.Fatal("TypeScript anban launcher must exec the compiled Node entrypoint")
	}
}

func TestTypeScriptRuntimeDockerfilesShareLockedSDKWithOptionalPackages(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article-ts", "Dockerfile.agent-seednote-ts", "Dockerfile.agent-montage-ts"} {
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
