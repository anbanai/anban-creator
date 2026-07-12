package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatorAgentImageNamingContract(t *testing.T) {
	root := repositoryRoot(t)

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	if err := validateCreatorAgentImageNaming(makefile, compose); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCreatorAgentImageNamingRejectsDecoysAndLegacyValues(t *testing.T) {
	validMakefile := "AGENT_IMAGE := creator-agent:latest\n"
	validCompose := `services:
  # agent:
  #   image: creator-agent:latest
  #   container_name: creator-agent
`

	for _, tc := range []struct {
		name     string
		makefile string
		compose  string
	}{
		{
			name:     "Make variable name decoy",
			makefile: "LEGACY_AGENT_IMAGE := creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "commented Make assignment",
			makefile: "# AGENT_IMAGE := creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "legacy Make identity remains",
			makefile: validMakefile + "LEGACY_AGENT_IMAGE := anban-creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "retired short Make identity remains",
			makefile: validMakefile + "LEGACY_AGENT_IMAGE := anban-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "duplicate Make assignment overrides expected value",
			makefile: validMakefile + "AGENT_IMAGE := wrong-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "Compose values outside agent block",
			makefile: validMakefile,
			compose: `services:
  # worker:
  #   image: creator-agent:latest
  #   container_name: creator-agent
`,
		},
		{
			name:     "legacy Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  # legacy-agent:
  #   image: anban-creator-agent:latest
  #   container_name: anban-creator-agent
`,
		},
		{
			name:     "retired short Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  # legacy-agent:
  #   image: anban-agent:latest
  #   container_name: anban-agent
`,
		},
		{
			name:     "duplicate Compose image overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "  #   image: wrong-agent:latest\n",
		},
		{
			name:     "duplicate Compose container name overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "  #   container_name: wrong-agent\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCreatorAgentImageNaming(tc.makefile, tc.compose); err == nil {
				t.Fatal("validateCreatorAgentImageNaming accepted an invalid runtime identity contract")
			}
		})
	}
}

func validateCreatorAgentImageNaming(makefile, compose string) error {
	if containsRetiredAgentIdentity(makefile) {
		return fmt.Errorf("Makefile must not retain anban-creator-agent or anban-agent identities")
	}
	makeValues := makeVariableAssignments(makefile, "AGENT_IMAGE")
	if len(makeValues) != 1 || makeValues[0] != "creator-agent:latest" {
		return fmt.Errorf("Makefile must define AGENT_IMAGE exactly once with value creator-agent:latest")
	}

	if containsRetiredAgentIdentity(compose) {
		return fmt.Errorf("docker-compose.yml must not retain anban-creator-agent or anban-agent identities")
	}
	lines := strings.Split(compose, "\n")
	agentBlockCount := 0
	var imageValues []string
	var containerNameValues []string
	for i, line := range lines {
		if line != "  # agent:" {
			continue
		}
		agentBlockCount++

		for _, blockLine := range lines[i+1:] {
			if !strings.HasPrefix(blockLine, "  #   ") {
				break
			}
			content := strings.TrimPrefix(blockLine, "  #   ")
			if strings.HasPrefix(content, " ") {
				continue
			}
			key, value, found := strings.Cut(content, ":")
			if !found {
				continue
			}
			switch key {
			case "image":
				imageValues = append(imageValues, strings.TrimSpace(value))
			case "container_name":
				containerNameValues = append(containerNameValues, strings.TrimSpace(value))
			}
		}
	}
	if agentBlockCount != 1 || len(imageValues) != 1 || imageValues[0] != "creator-agent:latest" || len(containerNameValues) != 1 || containerNameValues[0] != "creator-agent" {
		return fmt.Errorf("docker-compose.yml must define exactly one optional # agent: block with one image creator-agent:latest and one container_name creator-agent")
	}
	return nil
}

func containsRetiredAgentIdentity(text string) bool {
	for _, retired := range []string{"anban-creator-agent", "anban-agent"} {
		if strings.Contains(text, retired) {
			return true
		}
	}
	return false
}

func makeVariableAssignments(text, variable string) []string {
	var values []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, operator := range []string{":=", "?=", "+=", "="} {
			name, value, found := strings.Cut(line, operator)
			if !found || strings.TrimSpace(name) != variable {
				continue
			}
			values = append(values, strings.TrimSpace(value))
			break
		}
	}
	return values
}

func TestDockerfilesUseOpenHandsAgentRuntime(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		name       string
		path       string
		wantServer bool
	}{
		{
			name:       "server",
			path:       filepath.Join(root, "Dockerfile.server"),
			wantServer: true,
		},
		{
			name: "agent",
			path: filepath.Join(root, "Dockerfile.agent"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readTextFile(t, tc.path)
			for _, want := range []string{
				"FROM ghcr.io/openhands/agent-server:1.23.0-python",
				"apt-get install -y --no-install-recommends ca-certificates curl git jq fontconfig fonts-noto-cjk python3",
				"if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then",
				"apt-get install -y --no-install-recommends ffmpeg",
				"npm install -g @anthropic-ai/claude-code",
				"COPY claudecode/",
				"COPY third_party/OpenMontage/ /app/third_party/OpenMontage/",
				"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
				"ENV ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage",
				"npx -y skills@latest add heygen-com/hyperframes",
				"--skill music-to-video",
				"--skill slideshow",
				"npx -y skills@latest add remotion-dev/skills",
				"--skill remotion-best-practices",
				"claude plugin install --scope user anban@anbanai",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
			if tc.wantServer {
				for _, want := range []string{
					"go build -ldflags=\"-s -w\" -o /anban-creator-server ./server/",
					"go build -ldflags=\"-s -w\" -o /anban ./agent",
					"COPY --from=builder /anban-creator-server /app/anban-creator-server",
					"COPY --from=builder /anban /usr/local/bin/anban",
					"ENTRYPOINT []",
					`CMD ["/app/anban-creator-server", "-config", "/app/conf/config.yaml"]`,
				} {
					if !strings.Contains(body, want) {
						t.Fatalf("%s missing server runtime contract %q", tc.path, want)
					}
				}
			}
		})
	}
}

func TestOpenHandsRuntimeDockerfilesInstallPackagesAsRoot(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{
		filepath.Join(root, "Dockerfile.server"),
		filepath.Join(root, "Dockerfile.agent"),
	} {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			body := readTextFile(t, path)
			from := strings.Index(body, "FROM ghcr.io/openhands/agent-server:1.23.0-python")
			if from < 0 {
				t.Fatalf("%s missing OpenHands runtime stage", path)
			}
			apt := strings.Index(body[from:], "apt-get update")
			if apt < 0 {
				t.Fatalf("%s missing apt-get update in OpenHands runtime stage", path)
			}
			beforeApt := body[from : from+apt]
			if !strings.Contains(beforeApt, "USER root") {
				t.Fatalf("%s must switch to USER root before apt-get update because the OpenHands base image may default to a non-root user", path)
			}
		})
	}
}

func TestDockerignoreExcludesLargeNonRuntimeTrees(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, ".dockerignore"))
	for _, want := range []string{
		"desktop/",
		"miniapp/",
		"openclaw/",
		"codex/",
		"**/node_modules/",
		"**/dist/",
		"**/.cache/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf(".dockerignore missing %q", want)
		}
	}
}

func TestAgentDockerfileIsRootEntrypoint(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile.agent")); err != nil {
		t.Fatalf("Dockerfile.agent must exist at repository root for CI builds with root context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "Dockerfile")); err == nil {
		t.Fatalf("agent/Dockerfile must not exist; use root Dockerfile.agent so CI context is unambiguous")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat agent/Dockerfile: %v", err)
	}
}

func TestServerDockerfileIsRootEntrypoint(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile.server")); err != nil {
		t.Fatalf("Dockerfile.server must exist at repository root for CI builds with root context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "server", "Dockerfile")); err == nil {
		t.Fatalf("server/Dockerfile must not exist; use root Dockerfile.server so CI context is unambiguous")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat server/Dockerfile: %v", err)
	}
}
