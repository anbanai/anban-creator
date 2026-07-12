package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestAgentDockerfileAndKubernetesJobAgreeOnNodeIdentity(t *testing.T) {
	if kubernetesAgentUID != 1000 || kubernetesAgentGID != 1000 {
		t.Fatalf("Job identity = %d:%d, want explicit node 1000:1000", kubernetesAgentUID, kubernetesAgentGID)
	}
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "Dockerfile.agent"))
	for _, want := range []string{
		`[ "$(id -u node)" = "1000" ]`,
		`[ "$(id -g node)" = "1000" ]`,
		"useradd --uid 1000 --gid 1000",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing explicit node identity contract %q", want)
		}
	}
}

func TestAgentDockerfileSnapshotsInstalledHomeStateForJobRuntime(t *testing.T) {
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "Dockerfile.agent"))
	for _, want := range []string{
		"ENV ANBAN_HOME_TEMPLATE=/opt/anban-home-template",
		`for skill in music-to-video slideshow remotion-best-practices; do`,
		`test -f "/home/node/.claude/skills/$skill/SKILL.md"`,
		`cp -a "/home/node/.claude/skills/$skill/." "$ANBAN_HOME_TEMPLATE/.claude/skills/$skill/"`,
		`for file in known_marketplaces.json installed_plugins.json; do`,
		`cp -a "/home/node/.claude/plugins/$file" "$ANBAN_HOME_TEMPLATE/.claude/plugins/$file"`,
		`test -d "/home/node/.claude/plugins/cache/anbanai"`,
		`cp -a "/home/node/.claude/plugins/cache/anbanai" "$ANBAN_HOME_TEMPLATE/.claude/plugins/cache/anbanai"`,
		`find "$ANBAN_HOME_TEMPLATE" -type l -print -quit`,
		`chown -R root:root "$ANBAN_HOME_TEMPLATE"`,
		`chmod -R a=rX "$ANBAN_HOME_TEMPLATE"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing immutable home-template contract %q", want)
		}
	}
	installAt := strings.Index(body, "claude plugin install --scope user anban@anbanai")
	snapshotAt := strings.Index(body, `for skill in music-to-video slideshow remotion-best-practices; do`)
	if installAt < 0 || snapshotAt < installAt {
		t.Fatalf("home snapshot must occur after all node-user plugin installation: install=%d snapshot=%d", installAt, snapshotAt)
	}
	for _, forbidden := range []string{
		`for entry in .claude .agents .claude.json; do`,
		`cp -a "/home/node/.claude"`,
		`cp -a "/home/node/.agents"`,
		`cp -a "/home/node/.claude.json"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Dockerfile.agent must not broadly snapshot user home via %q", forbidden)
		}
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
