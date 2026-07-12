package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	kubernetestesting "k8s.io/client-go/testing"

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
	if !strings.HasPrefix(first, "creator-agent-") {
		t.Fatalf("pod name = %q, want creator-agent prefix", first)
	}
}

func TestKubernetesPodNamesDistinguishCurrentAndLegacyIdentity(t *testing.T) {
	task := &model.Task{UserID: "user-1", ProjectID: "project-1"}

	if got, want := kubernetesAgentPodName(task), "creator-agent-user-1-project-1-94df591a66"; got != want {
		t.Fatalf("current pod name = %q, want %q", got, want)
	}
	if got, want := kubernetesLegacyAgentPodName(task), "anban-agent-user-1-project-1-94df591a66"; got != want {
		t.Fatalf("legacy pod name = %q, want %q", got, want)
	}
}

func TestKubernetesLabelsIncludeUserAndProject(t *testing.T) {
	task := &model.Task{UserID: "user-1", ProjectID: "project-1"}
	labels := kubernetesAgentLabels(task)

	if labels["app.kubernetes.io/name"] != "creator-agent" {
		t.Fatalf("app label = %q, want creator-agent", labels["app.kubernetes.io/name"])
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
			AgentImage:         "registry.example.com/creator-agent:latest",
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
	if pod.Spec.Containers[0].Name != "creator-agent" {
		t.Fatalf("container name = %q, want creator-agent", pod.Spec.Containers[0].Name)
	}
	if pod.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("imagePullPolicy = %q, want Always for production latest-tag rollouts", pod.Spec.Containers[0].ImagePullPolicy)
	}
	agentContainer := pod.Spec.Containers[0]
	if agentContainer.SecurityContext == nil || agentContainer.SecurityContext.RunAsUser == nil || *agentContainer.SecurityContext.RunAsUser != 1000 {
		t.Fatalf("agent container security context = %#v, want node user 1000", agentContainer.SecurityContext)
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
			AgentImage:         "registry.example.com/creator-agent:v1",
			PodRevision:        "rev-1",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	newExec := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			AgentImage:         "registry.example.com/creator-agent:v1",
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

	legacyRootPod := newExec.buildAgentPod(opts)
	legacyRootPod.Spec.Containers[0].SecurityContext = nil
	if kubernetesPodConfigMatches(legacyRootPod, newExec.buildAgentPod(opts)) {
		t.Fatal("config match = true, want legacy root pod to be recreated")
	}
}

func TestKubernetesEnsureAgentPodPreservesActiveLegacyPod(t *testing.T) {
	opts := &ExecutionOptions{
		Task:    &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	}
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			Namespace:          "agents",
			AgentImage:         "registry.example.com/creator-agent:v1",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	current := e.buildAgentPod(opts)
	current.Namespace = e.kubeCfg.Namespace
	current.Status.Conditions = []corev1.PodCondition{{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}}
	legacy := current.DeepCopy()
	legacy.Name = kubernetesLegacyAgentPodName(opts.Task)
	legacy.UID = types.UID("legacy-pod-uid")
	legacy.Labels["app.kubernetes.io/name"] = "anban-agent"
	client := kubernetesfake.NewSimpleClientset(current, legacy)
	e.kube = client

	got, err := e.ensureAgentPod(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if got != current.Name {
		t.Fatalf("pod name = %q, want current pod %q", got, current.Name)
	}
	if gotLegacy, err := e.kube.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), legacy.Name, metav1.GetOptions{}); err != nil || gotLegacy.UID != legacy.UID {
		t.Fatalf("active legacy pod was not preserved: pod=%#v err=%v", gotLegacy, err)
	}
	if _, err := e.kube.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), current.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("current pod was not preserved: %v", err)
	}
}

func TestKubernetesEnsureAgentPodDeletesDriftedCurrentPodWithIdentityPreconditions(t *testing.T) {
	opts := &ExecutionOptions{Task: &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}}
	e := &KubernetesExecutor{kubeCfg: srvconfig.KubernetesConfig{Namespace: "agents"}}
	desired := e.buildAgentPod(opts)
	desired.Namespace = e.kubeCfg.Namespace
	stale := desired.DeepCopy()
	stale.UID = types.UID("stale-current-uid")
	stale.ResourceVersion = "42"
	stale.Annotations[kubernetesPodConfigHashAnnotation] = "stale-config"
	client := kubernetesfake.NewSimpleClientset(stale)
	var deleteOptions metav1.DeleteOptions
	client.PrependReactor("delete", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		deleteOptions = action.(kubernetestesting.DeleteAction).GetDeleteOptions()
		return false, nil, nil
	})
	client.PrependReactor("create", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		created := action.(kubernetestesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
		created.UID = types.UID("desired-current-uid")
		created.ResourceVersion = "43"
		created.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), created, e.kubeCfg.Namespace); err != nil {
			return true, nil, err
		}
		return true, created, nil
	})
	e.kube = client

	if _, err := e.ensureAgentPod(context.Background(), opts); err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if deleteOptions.Preconditions == nil || deleteOptions.Preconditions.UID == nil || *deleteOptions.Preconditions.UID != stale.UID {
		t.Fatalf("delete UID precondition = %#v, want %q", deleteOptions.Preconditions, stale.UID)
	}
	if deleteOptions.Preconditions.ResourceVersion == nil || *deleteOptions.Preconditions.ResourceVersion != stale.ResourceVersion {
		t.Fatalf("delete resourceVersion precondition = %#v, want %q", deleteOptions.Preconditions, stale.ResourceVersion)
	}
}

func TestKubernetesEnsureAgentPodPreservesChangedCurrentPodIdentity(t *testing.T) {
	opts := &ExecutionOptions{Task: &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}}
	e := &KubernetesExecutor{kubeCfg: srvconfig.KubernetesConfig{Namespace: "agents"}}
	desired := e.buildAgentPod(opts)
	desired.Namespace = e.kubeCfg.Namespace
	stale := desired.DeepCopy()
	stale.UID = types.UID("stale-current-uid")
	stale.ResourceVersion = "42"
	stale.Annotations[kubernetesPodConfigHashAnnotation] = "stale-config"
	replacement := desired.DeepCopy()
	replacement.UID = types.UID("replacement-current-uid")
	replacement.ResourceVersion = "43"
	replacement.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	client := kubernetesfake.NewSimpleClientset(stale)
	client.PrependReactor("delete", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		name := action.(kubernetestesting.DeleteAction).GetName()
		if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), e.kubeCfg.Namespace, name); err != nil {
			return true, nil, err
		}
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), replacement.DeepCopy(), e.kubeCfg.Namespace); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, name, fmt.Errorf("identity precondition failed"))
	})
	e.kube = client

	got, err := e.ensureAgentPod(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if got != desired.Name {
		t.Fatalf("pod name = %q, want replacement %q", got, desired.Name)
	}
	current, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), desired.Name, metav1.GetOptions{})
	if err != nil || current.UID != replacement.UID {
		t.Fatalf("replacement current pod was not preserved: pod=%#v err=%v", current, err)
	}
}

func TestKubernetesEnsureAgentPodRejectsDriftedCurrentPodWithoutIdentity(t *testing.T) {
	tests := []struct {
		name            string
		uid             types.UID
		resourceVersion string
		wantError       string
	}{
		{name: "empty UID", resourceVersion: "42", wantError: "UID is empty"},
		{name: "empty resource version", uid: types.UID("stale-current-uid"), wantError: "resourceVersion is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ExecutionOptions{Task: &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}}
			e := &KubernetesExecutor{kubeCfg: srvconfig.KubernetesConfig{Namespace: "agents"}}
			desired := e.buildAgentPod(opts)
			desired.Namespace = e.kubeCfg.Namespace
			stale := desired.DeepCopy()
			stale.UID = tt.uid
			stale.ResourceVersion = tt.resourceVersion
			stale.Annotations[kubernetesPodConfigHashAnnotation] = "stale-config"
			client := kubernetesfake.NewSimpleClientset(stale)
			deleteCalls := 0
			client.PrependReactor("delete", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
				deleteCalls++
				return false, nil, nil
			})
			client.PrependReactor("create", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
				created := action.(kubernetestesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
				created.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), created, e.kubeCfg.Namespace); err != nil {
					return true, nil, err
				}
				return true, created, nil
			})
			e.kube = client

			_, err := e.ensureAgentPod(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ensureAgentPod error = %v, want %q", err, tt.wantError)
			}
			if deleteCalls != 0 {
				t.Fatalf("delete calls = %d, want fail closed before delete", deleteCalls)
			}
		})
	}
}

func TestKubernetesEnsureAgentPodAcceptsConcurrentCurrentPodCreation(t *testing.T) {
	opts := &ExecutionOptions{
		Task:    &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	}
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			Namespace:          "agents",
			AgentImage:         "registry.example.com/creator-agent:v1",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	current := e.buildAgentPod(opts)
	current.Namespace = e.kubeCfg.Namespace
	current.Status.Conditions = []corev1.PodCondition{{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}}
	legacy := current.DeepCopy()
	legacy.Name = kubernetesLegacyAgentPodName(opts.Task)
	legacy.UID = types.UID("legacy-pod-uid")
	legacy.Labels["app.kubernetes.io/name"] = "anban-agent"
	client := kubernetesfake.NewSimpleClientset(legacy)
	client.PrependReactor("create", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), current.DeepCopy(), e.kubeCfg.Namespace); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, current.Name)
	})
	e.kube = client

	got, err := e.ensureAgentPod(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if got != current.Name {
		t.Fatalf("pod name = %q, want concurrently created pod %q", got, current.Name)
	}
	if _, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), legacy.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("active legacy pod was not preserved: %v", err)
	}
	if _, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), current.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("concurrently created current pod was not preserved: %v", err)
	}
}

func TestKubernetesEnsureAgentPodReconcilesStaleConcurrentWinner(t *testing.T) {
	opts := &ExecutionOptions{
		Task:    &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	}
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			Namespace:          "agents",
			AgentImage:         "registry.example.com/creator-agent:v2",
			PodRevision:        "rev-2",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	desired := e.buildAgentPod(opts)
	desired.Namespace = e.kubeCfg.Namespace
	legacy := desired.DeepCopy()
	legacy.Name = kubernetesLegacyAgentPodName(opts.Task)
	legacy.UID = types.UID("legacy-pod-uid")
	legacy.Labels["app.kubernetes.io/name"] = "anban-agent"
	stale := desired.DeepCopy()
	stale.UID = types.UID("stale-current-uid")
	stale.ResourceVersion = "41"
	stale.Spec.Containers[0].Image = "registry.example.com/creator-agent:v1"
	stale.Spec.Containers[0].SecurityContext = nil
	stale.Annotations[kubernetesPodRevisionAnnotation] = "rev-1"
	stale.Annotations[kubernetesPodConfigHashAnnotation] = "stale-config"
	stale.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}

	client := kubernetesfake.NewSimpleClientset(legacy)
	createAttempts := 0
	client.PrependReactor("create", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		createAttempts++
		switch createAttempts {
		case 1:
			if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), stale.DeepCopy(), e.kubeCfg.Namespace); err != nil {
				return true, nil, err
			}
			return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, stale.Name)
		case 2:
			createAction, ok := action.(kubernetestesting.CreateAction)
			if !ok {
				return true, nil, fmt.Errorf("create action type = %T, want CreateAction", action)
			}
			created := createAction.GetObject().(*corev1.Pod).DeepCopy()
			created.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), created, e.kubeCfg.Namespace); err != nil {
				return true, nil, err
			}
			return true, created, nil
		default:
			return true, nil, fmt.Errorf("unexpected create attempt %d", createAttempts)
		}
	})
	e.kube = client

	got, err := e.ensureAgentPod(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if got != desired.Name {
		t.Fatalf("pod name = %q, want reconciled pod %q", got, desired.Name)
	}
	if createAttempts != 2 {
		t.Fatalf("create attempts = %d, want stale winner replaced by desired pod", createAttempts)
	}
	current, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), desired.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get reconciled current pod: %v", err)
	}
	if !kubernetesPodConfigMatches(current, desired) {
		t.Fatalf("current pod config = %#v, want desired config", current)
	}
	if _, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), legacy.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("active legacy pod was not preserved: %v", err)
	}
}

func TestKubernetesEnsureAgentPodReconcilesReplacementBeforeReadiness(t *testing.T) {
	opts := &ExecutionOptions{
		Task:    &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"},
		Project: &model.Project{ID: "project-1"},
	}
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{
			Namespace:          "agents",
			AgentImage:         "registry.example.com/creator-agent:v2",
			PodRevision:        "rev-2",
			ServiceAccount:     "creator-agent-runner",
			WorkspaceMountPath: "/workspace",
			WorkspacePVCName:   "anban-creator",
		},
		serverURL: "http://creator-api-svc:8080",
	}
	desired := e.buildAgentPod(opts)
	desired.Namespace = e.kubeCfg.Namespace
	legacy := desired.DeepCopy()
	legacy.Name = kubernetesLegacyAgentPodName(opts.Task)
	legacy.UID = types.UID("legacy-pod-uid")
	legacy.Labels["app.kubernetes.io/name"] = "anban-agent"
	matching := desired.DeepCopy()
	stale := desired.DeepCopy()
	stale.UID = types.UID("stale-current-uid")
	stale.ResourceVersion = "42"
	stale.Spec.Containers[0].Image = "registry.example.com/creator-agent:v1"
	stale.Annotations[kubernetesPodRevisionAnnotation] = "rev-1"
	stale.Annotations[kubernetesPodConfigHashAnnotation] = "stale-config"
	stale.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}

	client := kubernetesfake.NewSimpleClientset(legacy)
	createAttempts := 0
	client.PrependReactor("create", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		createAttempts++
		switch createAttempts {
		case 1:
			if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), matching.DeepCopy(), e.kubeCfg.Namespace); err != nil {
				return true, nil, err
			}
			return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, matching.Name)
		case 2:
			createAction := action.(kubernetestesting.CreateAction)
			created := createAction.GetObject().(*corev1.Pod).DeepCopy()
			created.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), created, e.kubeCfg.Namespace); err != nil {
				return true, nil, err
			}
			return true, created, nil
		default:
			return true, nil, fmt.Errorf("unexpected create attempt %d", createAttempts)
		}
	})
	currentGets := 0
	client.PrependReactor("get", "pods", func(action kubernetestesting.Action) (bool, runtime.Object, error) {
		getAction := action.(kubernetestesting.GetAction)
		if getAction.GetName() != desired.Name {
			return false, nil, nil
		}
		currentGets++
		if currentGets == 3 {
			if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), e.kubeCfg.Namespace, desired.Name); err != nil {
				return true, nil, err
			}
			if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), stale.DeepCopy(), e.kubeCfg.Namespace); err != nil {
				return true, nil, err
			}
		}
		return false, nil, nil
	})
	e.kube = client

	got, err := e.ensureAgentPod(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensureAgentPod: %v", err)
	}
	if got != desired.Name {
		t.Fatalf("pod name = %q, want reconciled pod %q", got, desired.Name)
	}
	if createAttempts != 2 {
		t.Fatalf("create attempts = %d, want readiness replacement reconciled", createAttempts)
	}
	current, err := client.CoreV1().Pods(e.kubeCfg.Namespace).Get(context.Background(), desired.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get reconciled current pod: %v", err)
	}
	if !kubernetesPodConfigMatches(current, desired) {
		t.Fatalf("current pod config = %#v, want desired config", current)
	}
}

func TestKubernetesWaitForAgentPodReadyRejectsTerminatingPod(t *testing.T) {
	now := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "creator-agent-project", Namespace: "agents", DeletionTimestamp: &now},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		}}},
	}
	e := &KubernetesExecutor{
		kubeCfg: srvconfig.KubernetesConfig{Namespace: pod.Namespace},
		kube:    kubernetesfake.NewSimpleClientset(pod),
	}
	desired := pod.DeepCopy()
	desired.DeletionTimestamp = nil

	err := e.waitForAgentPodReady(context.Background(), desired)
	if !errors.Is(err, errKubernetesAgentPodNeedsReconciliation) || !strings.Contains(err.Error(), "terminating") {
		t.Fatalf("waitForAgentPodReady error = %v, want terminating needs-reconciliation error", err)
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
		Result:    nil,
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

func TestKubernetesCopyWorkspaceScriptPreservesExistingNASWorkspace(t *testing.T) {
	workDir := "/workspace/users/user-1/projects/project-1/tasks/task-1/workspace"
	script := kubernetesCopyWorkspaceScript(workDir)
	if strings.Contains(script, "rm ") || strings.Contains(script, "rm-") {
		t.Fatalf("copy script must not delete existing NAS content: %q", script)
	}
	for _, want := range []string{"mkdir -p '" + workDir + "'", "tar -C '" + workDir + "' -xf -"} {
		if !strings.Contains(script, want) {
			t.Fatalf("copy script missing %q: %q", want, script)
		}
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
