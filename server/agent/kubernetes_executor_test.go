package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

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
		serverURL:         "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080/",
		maxTurnsOverrides: map[string]int{model.PlatformArticle: 60},
	}

	cmd := e.buildAgentCommand(&ExecutionOptions{Task: task}, "claude-sonnet", 60, "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace", "agent-key")

	assertArgPair(t, cmd, "--server-url", "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080")
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
		serverURL: "http://creator-api-svc:8080",
	}
	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformMontage},
		Project:            &model.Project{ID: "project-1"},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	})

	for _, want := range []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"ANTHROPIC_API_KEY=sk-ant",
		"ANTHROPIC_BASE_URL=https://anthropic.example.com",
		"ANBAN_API_URL=http://creator-api-svc:8080",
		"ANBAN_DEFAULT_PROJECT=project-1",
		"ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage",
		"FAL_KEY=fal-secret",
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("env missing %q in %#v", want, env)
		}
	}
}

func TestKubernetesAgentEnvSkipsMontageProviderEnvForOtherTasks(t *testing.T) {
	e := &KubernetesExecutor{serverURL: "http://creator-api-svc:8080"}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformArticle},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	})

	if slices.Contains(env, "FAL_KEY=fal-secret") {
		t.Fatalf("env = %#v, non-Montage task must not receive Montage provider env", env)
	}
}

func TestKubernetesPodSpecUsesConfiguredImageAndPVC(t *testing.T) {
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			AgentImage:         "registry.example.com/anban-agent:latest",
			ServiceAccount:     "creator-agent-runner",
			ImagePullSecret:    "acr-secret",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	pod := e.buildAgentPod(&ExecutionOptions{
		Task:    &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	})

	if pod.Name == "" || pod.Namespace != "" {
		t.Fatalf("pod identity = %q/%q, want generated name and namespace filled by caller", pod.Namespace, pod.Name)
	}
	if _, ok := pod.Labels[kubernetesTaskIDLabel]; ok {
		t.Fatalf("project-scoped agent pod labels include stale task id: %#v", pod.Labels)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != e.kubeCfg.AgentImage {
		t.Fatalf("container spec = %#v, want configured image", pod.Spec.Containers)
	}
	if pod.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("imagePullPolicy = %q, want Always for production latest-tag rollouts", pod.Spec.Containers[0].ImagePullPolicy)
	}
	if pod.Spec.ServiceAccountName != "creator-agent-runner" {
		t.Fatalf("service account = %q, want configured", pod.Spec.ServiceAccountName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatalf("automount service account token = %#v, want disabled for agent pods", pod.Spec.AutomountServiceAccountToken)
	}
	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.FSGroup == nil || *pod.Spec.SecurityContext.FSGroup != 1000 {
		t.Fatalf("pod security context = %#v, want fsGroup 1000 for NAS write access", pod.Spec.SecurityContext)
	}
	if len(pod.Spec.InitContainers) != 1 {
		t.Fatalf("init containers = %#v, want workspace permission initializer", pod.Spec.InitContainers)
	}
	init := pod.Spec.InitContainers[0]
	if init.SecurityContext == nil || init.SecurityContext.RunAsUser == nil || *init.SecurityContext.RunAsUser != 0 {
		t.Fatalf("init container security context = %#v, want root initializer", init.SecurityContext)
	}
	if strings.Join(init.Command, " ") == "" || !strings.Contains(strings.Join(init.Command, " "), "chown -R node:node") {
		t.Fatalf("init container command = %#v, want node ownership setup", init.Command)
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].PersistentVolumeClaim == nil || pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "anban-creator" {
		t.Fatalf("volumes = %#v, want workspace PVC", pod.Spec.Volumes)
	}
	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "acr-secret" {
		t.Fatalf("image pull secrets = %#v, want configured secret", pod.Spec.ImagePullSecrets)
	}
	if pod.Annotations[kubernetesPodConfigHashAnnotation] == "" {
		t.Fatalf("pod annotations = %#v, want config hash", pod.Annotations)
	}
}

func TestKubernetesWorkspaceInitRunsAsRootAndChownsWorkdir(t *testing.T) {
	projectDir := "/workspace/users/user-1/projects/project-1"
	script := kubernetesWorkspaceInitScript(projectDir)
	for _, want := range []string{
		"mkdir -p '/workspace/users/user-1/projects/project-1'",
		"chown -R node:node '/workspace/users/user-1/projects/project-1'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("workspace init script missing %q in %q", want, script)
		}
	}
}

func TestKubernetesPodConfigHashDetectsDrift(t *testing.T) {
	opts := &ExecutionOptions{
		Task:    &model.Task{UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	}
	oldExec := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			AgentImage:         "registry.example.com/anban-agent:v1",
			PodRevision:        "rev-1",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	newExec := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			AgentImage:         "registry.example.com/anban-agent:v1",
			PodRevision:        "rev-2",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}

	existing := oldExec.buildAgentPod(opts)
	desired := newExec.buildAgentPod(opts)

	if kubernetesPodConfigMatches(existing, desired) {
		t.Fatalf("config match = true, want revision drift to require pod recreation")
	}
	if !kubernetesPodConfigMatches(existing, oldExec.buildAgentPod(opts)) {
		t.Fatalf("config match = false, want identical config to reuse pod")
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

func TestKubernetesPrepareWorkspaceBundleRestoresResumeInputs(t *testing.T) {
	store := &fakeStore{
		readData: map[string][]byte{"uploads/resume/material.txt": []byte("resume material")},
	}
	e := &KubernetesExecutor{
		logger:      noopLogger(),
		imageAPICfg: &srvconfig.ImageAPIConfig{},
		store:       store,
	}
	task := &model.Task{
		ID:        "task-1",
		UserID:    "user-1",
		ProjectID: "project-1",
		Type:      model.PlatformArticle,
	}
	task.SetInputAttachments([]model.EntryAttachment{
		{
			Role:     model.EntryAttachmentRoleResumeLatest,
			Text:     "# 继续执行补充\n\n## 补充指令\n\n继续优化\n\n## 补充文件\n\n- 相对路径：attachments/material.txt\n",
			FileName: "latest.md",
		},
		{
			Role:     model.EntryAttachmentRoleResumeFile,
			Key:      "uploads/resume/material.txt",
			FileName: "material.txt",
		},
	})
	project := &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformArticle}

	bundleDir, cleanup, err := e.prepareWorkspaceBundle(context.Background(), &ExecutionOptions{Task: task, Project: project}, "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace")
	if err != nil {
		t.Fatalf("prepareWorkspaceBundle: %v", err)
	}
	defer cleanup()

	latest, err := os.ReadFile(filepath.Join(bundleDir, ".anban-creator", "resume", "latest.md"))
	if err != nil {
		t.Fatalf("read resume latest: %v", err)
	}
	if !strings.Contains(string(latest), "继续优化") || !strings.Contains(string(latest), "attachments/material.txt") {
		t.Fatalf("resume latest not restored:\n%s", latest)
	}
	matches, err := filepath.Glob(filepath.Join(bundleDir, ".anban-creator", "resume", "*", "attachments", "material.txt"))
	if err != nil {
		t.Fatalf("glob resume attachment: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("resume attachment matches = %v, want one", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read resume attachment: %v", err)
	}
	if string(data) != "resume material" {
		t.Fatalf("resume attachment = %q", data)
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
