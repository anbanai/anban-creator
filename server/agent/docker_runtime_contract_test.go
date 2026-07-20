package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
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

func TestDockerfileInventoryIsRootOnly(t *testing.T) {
	root := repositoryRoot(t)
	got := trackedDockerfiles(t, root)
	want := []string{
		"Dockerfile.agent",
		"Dockerfile.server",
		"Dockerfile.studio",
		"Dockerfile.wcflink",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repository-owned Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestCollectOwnedDockerfilesDetectsUnexpectedEntrypoints(t *testing.T) {
	got := collectOwnedDockerfilePaths([]string{
		"Dockerfile.server",
		"nested/Dockerfile.extra",
		"claudecode/Dockerfile.plugin",
		"third_party/tool/Dockerfile",
		"web/node_modules/Dockerfile",
		"web/dist/Dockerfile.generated",
		".worktrees/other/Dockerfile",
	})
	want := []string{"Dockerfile.server", "nested/Dockerfile.extra"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fixture Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestStudioDockerfileUsesRootBuildContext(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "Dockerfile.studio"))
	for _, want := range []string{
		"# syntax=docker/dockerfile:1.7",
		"FROM oven/bun:1.3 AS build",
		"WORKDIR /app",
		"COPY studio/package.json studio/bun.lock ./",
		"COPY studio/ .",
		"COPY studio/default.conf.template /etc/nginx/templates/default.conf.template",
		"RUN bun install --frozen-lockfile",
		"ARG VITE_API_BASE_URL=/api/v1",
		"ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}",
		"RUN bun run build",
		"FROM nginx:1.24-alpine",
		"ENV BACKEND_HOST=server",
		"COPY --from=build /app/dist /usr/share/nginx/html",
		"EXPOSE 80",
		"CMD [\"nginx\", \"-g\", \"daemon off;\"]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.studio missing root-context Studio contract %q", want)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "studio", ".dockerignore")); err == nil {
		t.Fatal("studio/.dockerignore must not exist; root-context builds use Dockerfile.studio.dockerignore")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat studio/.dockerignore: %v", err)
	}

	rules := dockerignoreRules(readTextFile(t, filepath.Join(root, "Dockerfile.studio.dockerignore")))
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatalf("Dockerfile.studio.dockerignore must begin its non-comment rules with broad exclusion **; got %v", rules)
	}
	studioDir := dockerignoreRuleIndex(t, rules, "!studio/")
	studioTree := dockerignoreRuleIndex(t, rules, "!studio/**")
	if studioDir <= 0 || studioTree <= studioDir {
		t.Fatalf("Dockerfile.studio.dockerignore must unignore studio/ then studio/** after **; got %v", rules)
	}
	for _, generated := range []string{
		"studio/node_modules/",
		"studio/dist/",
		"studio/build/",
		"studio/.cache/",
		"studio/.vite/",
		"studio/.next/",
		"studio/coverage/",
		"studio/.env*",
		"studio/**/.env*",
		"studio/*.pem",
		"studio/**/*.pem",
		"studio/.vercel/",
		"studio/out/",
		"studio/*.tsbuildinfo",
		"studio/**/*.tsbuildinfo",
		"studio/next-env.d.ts",
		"studio/**/next-env.d.ts",
		"studio/.pnp.*",
		"studio/.yarn/",
		"studio/.yarnrc*",
		"studio/npm-debug.log*",
		"studio/**/npm-debug.log*",
		"studio/yarn-debug.log*",
		"studio/**/yarn-debug.log*",
		"studio/yarn-error.log*",
		"studio/**/yarn-error.log*",
		"studio/pnpm-debug.log*",
		"studio/**/pnpm-debug.log*",
	} {
		if index := dockerignoreRuleIndex(t, rules, generated); index <= studioTree {
			t.Fatalf("Dockerfile.studio.dockerignore must exclude %q after unignoring Studio source; got %v", generated, rules)
		}
	}
}

func TestWcfLinkDockerfileRetainsPinnedBuildContract(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "Dockerfile.wcflink"))
	for _, want := range []string{
		"ARG WCFLINK_REPO=https://github.com/lich0821/wcfLink.git",
		"ARG WCFLINK_REF=refs/tags/v0.1.0",
		"ARG WCFLINK_COMMIT=fb0999b81043c91e8fddb780eb2ecf03f1f8588f",
		"git fetch --depth 1 origin \"$WCFLINK_REF\"",
		"test \"$(git rev-parse HEAD)\" = \"$WCFLINK_COMMIT\"",
		"CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags=\"-s -w\" -o /out/wcfLink ./cmd/wcfLink",
		"FROM alpine:3.20",
		"COPY --from=builder /out/wcfLink /app/wcfLink",
		"ENV WCFLINK_LISTEN_ADDR=:18070 \\\n+    WCFLINK_STATE_DIR=/app/state \\\n+    WCFLINK_DB_PATH=/app/state/wcf.db \\\n+    WCFLINK_LOG_LEVEL=info",
		"EXPOSE 18070",
		"VOLUME [\"/app/state\"]",
		"ENTRYPOINT [\"/app/wcfLink\"]",
	} {
		want = strings.ReplaceAll(want, "\n+", "\n")
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.wcflink missing pinned runtime contract %q", want)
		}
	}
}

func TestComposeAndMakefileUseRootDockerfileBuilds(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"# agent:\n  #   build:\n  #     context: .\n  #     dockerfile: Dockerfile.agent",
		"wcflink:\n    build:\n      context: .\n      dockerfile: Dockerfile.wcflink",
		"server:\n    build:\n      context: .\n      dockerfile: Dockerfile.server",
		"studio:\n    build:\n      context: .\n      dockerfile: Dockerfile.studio",
		"studio:\n    build:\n      context: .\n      dockerfile: Dockerfile.studio\n      args:\n        VITE_API_BASE_URL: /api/v1",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing root Docker build contract %q", want)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"WCFLINK_IMAGE := anban-creator-wcflink:latest",
		"STUDIO_IMAGE := anban-creator-studio:latest",
		"docker-agent-image docker-server-image docker-wcflink-image docker-studio-image docker-images",
		"docker-wcflink-image:",
		"docker-studio-image:",
		"docker-images: docker-agent-image docker-server-image docker-wcflink-image docker-studio-image",
		"docker build -f Dockerfile.wcflink -t $(WCFLINK_IMAGE) .",
		"docker build -f Dockerfile.studio -t $(STUDIO_IMAGE) .",
		"docker-image: docker-agent-image",
		"make docker-agent-image",
		"make docker-server-image",
		"make docker-wcflink-image",
		"make docker-studio-image",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing root Docker build contract %q", want)
		}
	}
}

func TestNoStaleDockerfileReferences(t *testing.T) {
	root := repositoryRoot(t)
	paths := staleDockerfileReferencePaths(t, root)
	if len(paths) > 0 {
		t.Fatalf("stale Dockerfile references remain in repository-owned files: %s", strings.Join(paths, ", "))
	}
}

func trackedDockerfiles(t *testing.T, root string) []string {
	t.Helper()
	return collectOwnedDockerfilePaths(trackedFiles(t, root))
}

func collectOwnedDockerfilePaths(paths []string) []string {
	var dockerfiles []string
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if !ownedDockerContractPath(path) {
			continue
		}
		name := filepath.Base(path)
		if strings.HasSuffix(name, ".dockerignore") {
			continue
		}
		if !strings.HasPrefix(name, "Dockerfile") && !strings.HasSuffix(name, ".Dockerfile") {
			continue
		}
		dockerfiles = append(dockerfiles, path)
	}
	sort.Strings(dockerfiles)
	return dockerfiles
}

func staleDockerfileReferencePaths(t *testing.T, root string) []string {
	t.Helper()
	const contractTestPath = "server/agent/docker_runtime_contract_test.go"
	var paths []string
	for _, rel := range trackedFiles(t, root) {
		rel = filepath.ToSlash(rel)
		if !ownedDockerContractPath(rel) {
			continue
		}
		if rel == contractTestPath {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read tracked file %s: %v", rel, err)
		}
		if bytes.IndexByte(body, 0) >= 0 {
			continue
		}
		if bytes.Contains(body, []byte("studio/Dockerfile")) || bytes.Contains(body, []byte("docker/wcflink.Dockerfile")) {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	return paths
}

func dockerignoreRules(body string) []string {
	var rules []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	return rules
}

func dockerignoreRuleIndex(t *testing.T, rules []string, want string) int {
	t.Helper()
	for index, rule := range rules {
		if rule == want {
			return index
		}
	}
	t.Fatalf("Dockerfile.studio.dockerignore missing %q; got %v", want, rules)
	return -1
}

func ownedDockerContractPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		switch segment {
		case ".git", ".worktrees", "claudecode", "codex", "openclaw", "third_party", "vendor", "node_modules", "dist", "build", "coverage", ".cache", ".vite", ".next", "bin", "data", "release":
			return false
		}
	}
	return true
}
