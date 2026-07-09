package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestKubernetesPodNameIsDeterministicAndDNSSafe(t *testing.T) {
	task := &model.Task{
		UserID:    "USER_With.Mixed/Unsafe_Chars",
		ProjectID: "Project_With.Mixed/Unsafe_Chars",
	}

	first := kubernetesAgentPodName(task)
	second := kubernetesAgentPodName(task)
	if first != second {
		t.Fatalf("pod name not deterministic: %q != %q", first, second)
	}
	if len(first) > 63 {
		t.Fatalf("pod name length = %d, want <= 63: %q", len(first), first)
	}
	if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(first) {
		t.Fatalf("pod name %q is not DNS-1123 safe", first)
	}
	if !strings.HasPrefix(first, "anban-agent-") {
		t.Fatalf("pod name = %q, want anban-agent prefix", first)
	}
}

func TestKubernetesLabelsIncludeUserAndProject(t *testing.T) {
	task := &model.Task{UserID: "user-1", ProjectID: "project-1"}
	labels := kubernetesAgentLabels(task)

	if labels["app.kubernetes.io/name"] != "anban-agent" {
		t.Fatalf("app label = %q, want anban-agent", labels["app.kubernetes.io/name"])
	}
	if labels["anban.ai/user-id"] != "user-1" || labels["anban.ai/project-id"] != "project-1" {
		t.Fatalf("labels = %#v, want user/project IDs", labels)
	}
}

func TestKubernetesWorkspacePathIsUserProjectTaskScoped(t *testing.T) {
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	got := kubernetesWorkspacePath("/workspace", task)
	want := "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace"
	if got != want {
		t.Fatalf("workspace path = %q, want %q", got, want)
	}
}

func TestKubernetesAgentCommandUsesDirectArtifactUpload(t *testing.T) {
	task := &model.Task{
		ID:        "task-1",
		UserID:    "user-1",
		ProjectID: "project-1",
		Type:      model.PlatformArticle,
		Prompt:    "写一篇文章",
	}
	e := &KubernetesExecutor{
		serverURL:         "http://anban-server.anban.svc.cluster.local:8080/",
		maxTurnsOverrides: map[string]int{model.PlatformArticle: 60},
	}

	cmd := e.buildAgentCommand(&ExecutionOptions{Task: task}, "claude-sonnet", 60, "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace", "agent-key")

	assertArgPair(t, cmd, "--server-url", "http://anban-server.anban.svc.cluster.local:8080")
	assertArgPair(t, cmd, "--api-key", "agent-key")
	assertArgPair(t, cmd, "--task-id", "task-1")
	assertArgPair(t, cmd, "--task-type", model.PlatformArticle)
	assertArgPair(t, cmd, "--workspace", "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace")
	assertArgPair(t, cmd, "--model", "claude-sonnet")
	assertArgPair(t, cmd, "--artifact-upload-mode", "direct")
	if !slices.Contains(cmd, "--article-with-cover=true") || !slices.Contains(cmd, "--article-with-content-images=true") {
		t.Fatalf("article image flags missing from command: %#v", cmd)
	}
}

func TestKubernetesAgentEnvIncludesServerProjectAndClaudeEnv(t *testing.T) {
	e := &KubernetesExecutor{
		claudeEnv: map[string]string{
			"ANTHROPIC_API_KEY":  "sk-ant",
			"ANTHROPIC_BASE_URL": "https://anthropic.example.com",
		},
		serverURL: "http://anban-server:8080",
	}
	env := e.buildAgentEnv(&ExecutionOptions{Project: &model.Project{ID: "project-1"}})

	for _, want := range []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"ANTHROPIC_API_KEY=sk-ant",
		"ANTHROPIC_BASE_URL=https://anthropic.example.com",
		"ANBAN_API_URL=http://anban-server:8080",
		"ANBAN_DEFAULT_PROJECT=project-1",
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("env missing %q in %#v", want, env)
		}
	}
}

func TestKubernetesPodSpecUsesConfiguredImageAndPVC(t *testing.T) {
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			AgentImage:         "registry.example.com/anban-agent:latest",
			ServiceAccount:     "anban-agent-runner",
			ImagePullSecret:    "acr-secret",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-agent-nas",
		},
		serverURL: "http://anban-server:8080",
	}
	pod := e.buildAgentPod(&ExecutionOptions{
		Task:    &model.Task{UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	})

	if pod.Name == "" || pod.Namespace != "" {
		t.Fatalf("pod identity = %q/%q, want generated name and namespace filled by caller", pod.Namespace, pod.Name)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != e.kubeCfg.AgentImage {
		t.Fatalf("container spec = %#v, want configured image", pod.Spec.Containers)
	}
	if pod.Spec.ServiceAccountName != "anban-agent-runner" {
		t.Fatalf("service account = %q, want configured", pod.Spec.ServiceAccountName)
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].PersistentVolumeClaim == nil || pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "anban-agent-nas" {
		t.Fatalf("volumes = %#v, want workspace PVC", pod.Spec.Volumes)
	}
	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "acr-secret" {
		t.Fatalf("image pull secrets = %#v, want configured secret", pod.Spec.ImagePullSecrets)
	}
}

func TestKubernetesPrepareWorkspaceBundleMatchesDockerWorkspaceInputs(t *testing.T) {
	store := &fakeStore{
		readData: map[string][]byte{"uploads/ref.png": []byte("png")},
		ownedPred: func(rawURL string) bool {
			return strings.Contains(rawURL, "uploads/ref.png")
		},
	}
	e := &KubernetesExecutor{
		logger:      noopLogger(),
		imageAPICfg: &srvconfig.ImageAPIConfig{},
		store:       store,
	}
	task := &model.Task{
		ID:                "task-1",
		UserID:            "user-1",
		ProjectID:         "project-1",
		Type:              model.PlatformArticle,
		ReferenceImageURL: "https://cdn.example.com/uploads/ref.png",
	}
	project := &model.Project{
		ID:           "project-1",
		UserID:       "user-1",
		Platform:     model.PlatformArticle,
		Instructions: "Always keep the brand voice.",
	}

	bundleDir, cleanup, err := e.prepareWorkspaceBundle(context.Background(), &ExecutionOptions{Task: task, Project: project}, "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace")
	if err != nil {
		t.Fatalf("prepareWorkspaceBundle: %v", err)
	}
	defer cleanup()

	for _, rel := range []string{
		filepath.Join(".anban-creator", "settings.json"),
		"CLAUDE.md",
		filepath.Join(".anban-creator", "reference.png"),
	} {
		if _, err := os.Stat(filepath.Join(bundleDir, rel)); err != nil {
			t.Fatalf("expected staged %s: %v", rel, err)
		}
	}
	claude, err := os.ReadFile(filepath.Join(bundleDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claude), "Always keep the brand voice.") {
		t.Fatalf("CLAUDE.md missing project instructions: %s", claude)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != "uploads/ref.png" {
		t.Fatalf("storage read keys = %#v, want reference image key", store.readKeys)
	}
}

func assertArgPair(t *testing.T, args []string, key, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return
		}
	}
	t.Fatalf("args missing pair %s %s in %#v", key, value, args)
}
