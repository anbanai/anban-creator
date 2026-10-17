package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestDshSecretEnvDefaultsToServerMCPRoute(t *testing.T) {
	envs := dshSecretEnv("session-secret", "session-1", "", "https://creator-server:8443")
	for _, env := range envs {
		if env.Name == "ANBAN_DSH_BRIDGE_MCP_URL" {
			if env.Value != "https://creator-server:8443/mcp" {
				t.Fatalf("default MCP URL = %q, want server /mcp route", env.Value)
			}
			return
		}
	}
	t.Fatal("missing ANBAN_DSH_BRIDGE_MCP_URL")
}

func TestKubernetesRuntimeFactoryPodIsolatedAndReadOnly(t *testing.T) {
	factory := KubernetesRuntimeFactory{Config: srvconfig.DshConfig{
		Image: "registry.example/dsh@sha256:" + repeatHex(64), KubernetesNamespace: "anban",
		KubernetesImagePullSecret:    "acr-secret",
		KubernetesProjectMemoryClaim: "creator-project-memory",
		KubernetesProjectMemoryMount: "/workspace/project", CPUCores: 1, MemoryMB: 512,
	}, ServiceAccount: "creator-agent-runner", ServerInstanceID: "server-1"}
	pod := factory.pod(InteractiveRuntimeSpec{SessionID: "s-1", ProjectID: "p-1", RuntimeToken: "token"}, "anban", "anban-dsh-s-1")
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever || pod.Spec.ServiceAccountName != "creator-agent-runner" {
		t.Fatalf("pod lifecycle = %q/%q", pod.Spec.RestartPolicy, pod.Spec.ServiceAccountName)
	}
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem == nil || !*pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("DSH container must use a read-only root filesystem")
	}
	if got := pod.Spec.Containers[0].VolumeMounts[0].SubPath; got != "projects/p-1" {
		t.Fatalf("project memory subPath = %q", got)
	}
	if pod.Labels["com.anban.runtime"] != "dsh-interactive" {
		t.Fatal("missing interactive runtime label")
	}
	if len(pod.Spec.InitContainers) != 0 {
		t.Fatalf("init containers = %#v, want none for read-only project snapshot", pod.Spec.InitContainers)
	}
	for _, mount := range pod.Spec.Containers[0].VolumeMounts {
		if mount.Name == "agent-memory" || strings.HasPrefix(mount.MountPath, "/workspace/project/.claude") {
			t.Fatalf("DSH must not mount writable memory beneath its read-only project snapshot: %#v", mount)
		}
	}
	if len(pod.Spec.Containers[0].VolumeMounts) == 0 || pod.Spec.Containers[0].VolumeMounts[0].Name != "project-memory" || pod.Spec.Containers[0].VolumeMounts[0].MountPath != "/workspace/project" || !pod.Spec.Containers[0].VolumeMounts[0].ReadOnly {
		t.Fatalf("project snapshot mounts = %#v, want read-only project root", pod.Spec.Containers[0].VolumeMounts)
	}
	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "acr-secret" {
		t.Fatalf("image pull secrets = %#v, want acr-secret", pod.Spec.ImagePullSecrets)
	}
	context := `{"project":{"name":"existing"}}`
	secret := factory.secret(InteractiveRuntimeSpec{SessionID: "s-1", RuntimeToken: "token", InitialContext: context}, "anban", "anban-dsh-s-1-secret")
	if secret.StringData["runtime-token"] != "token" {
		t.Fatal("session token was not isolated in the Kubernetes Secret")
	}
	if secret.StringData["initial-context"] != context {
		t.Fatal("initial context was not staged in the session Secret")
	}
	var contextVolume *corev1.Volume
	for index := range pod.Spec.Volumes {
		if pod.Spec.Volumes[index].Name == "dsh-context" {
			contextVolume = &pod.Spec.Volumes[index]
			break
		}
	}
	if contextVolume == nil || contextVolume.VolumeSource.Secret == nil || contextVolume.VolumeSource.Secret.SecretName != "anban-dsh-s-1-secret" {
		t.Fatalf("context Secret volume = %#v", contextVolume)
	}
	hasContextPath := false
	for _, env := range dshSecretEnv(secret.Name, "s-1", "http://mcp", "http://server") {
		if env.Name == "ANBAN_DSH_BRIDGE_RUNTIME_TOKEN" && env.Value != "" {
			t.Fatal("runtime token must not be in Pod env.value")
		}
		if env.Name == "ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE" && env.Value != "/var/run/anban-dsh-context/initial-context" {
			t.Fatalf("initial context path = %q", env.Value)
		}
		if env.Name == "ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE" {
			hasContextPath = true
		}
	}
	if !hasContextPath {
		t.Fatal("missing initial context path")
	}
}

func TestKubernetesRuntimeFactoryBuildsLongLivedJobWithAttachableBridge(t *testing.T) {
	factory := KubernetesRuntimeFactory{ServiceAccount: "creator-agent-runner", Config: srvconfig.DshConfig{
		KubernetesNamespace: "anban", KubernetesImagePullSecret: "acr-secret", KubernetesProjectMemoryClaim: "creator-project-memory",
		KubernetesProjectMemoryMount: "/workspace/project", Image: "registry.example/creator-dsh@sha256:" + repeatHex(64), CPUCores: 1, MemoryMB: 512, TimeoutSec: 600,
	}}
	job := factory.job(InteractiveRuntimeSpec{SessionID: "session-1", AgentPackID: "project-creator", RuntimeToken: "token"}, "anban", "anban-dsh-session-1")
	if job.Kind != "Job" || job.Spec.Completions == nil || *job.Spec.Completions != 1 || job.Spec.Parallelism == nil || *job.Spec.Parallelism != 1 {
		t.Fatalf("job completion policy = %#v, want one completion and one parallel pod", job.Spec)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 || job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("job retry policy = %#v, want backoffLimit=0/restart Never", job.Spec)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Command == nil || len(container.Command) < 2 || container.Command[0] != "node" || container.Command[1] != "/app/runtime-dsh/dist/dsh-bridge.js" {
		t.Fatalf("bridge command = %#v, want bridge as container process", container.Command)
	}
	if !container.Stdin || container.StdinOnce || container.TTY {
		t.Fatalf("bridge stdio = stdin:%v stdinOnce:%v tty:%v, want attachable stdin", container.Stdin, container.StdinOnce, container.TTY)
	}
	if job.Spec.Template.Labels["com.anban.session_id"] != "session-1" || job.Spec.Template.Labels["com.anban.runtime"] != "dsh-interactive" {
		t.Fatalf("job pod labels = %#v", job.Spec.Template.Labels)
	}
}

func TestKubernetesRuntimeFactoryCleanupOrphans(t *testing.T) {
	client := fake.NewSimpleClientset(&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "dsh-1", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive"}}})
	factory := KubernetesRuntimeFactory{Kube: client, REST: &rest.Config{}, Config: srvconfig.DshConfig{KubernetesNamespace: "anban"}}
	if err := factory.CleanupOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs("anban").Get(context.Background(), "dsh-1", metav1.GetOptions{}); err == nil {
		t.Fatal("orphan Job was not deleted")
	}
}

func TestKubernetesRuntimeFactoryCleanupOrphansRetainsDiagnosticResourcesUntilDeadline(t *testing.T) {
	active := time.Now().Add(time.Minute).Unix()
	expired := time.Now().Add(-time.Minute).Unix()
	client := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "anban-dsh-active", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.server_instance": "server-1"}, Annotations: map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(active, 10)}}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "anban-dsh-expired", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.server_instance": "server-1"}, Annotations: map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(expired, 10)}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "active-secret", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.server_instance": "server-1", "com.anban.session_id": "active"}, Annotations: map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(active, 10)}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "expired-secret", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.server_instance": "server-1", "com.anban.session_id": "expired"}, Annotations: map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(expired, 10)}}},
	)
	factory := KubernetesRuntimeFactory{Kube: client, REST: &rest.Config{}, ServerInstanceID: "server-1", Config: srvconfig.DshConfig{KubernetesNamespace: "anban"}}
	if err := factory.CleanupOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs("anban").Get(context.Background(), "anban-dsh-active", metav1.GetOptions{}); err != nil {
		t.Fatalf("active diagnostic Job was deleted: %v", err)
	}
	if _, err := client.BatchV1().Jobs("anban").Get(context.Background(), "anban-dsh-expired", metav1.GetOptions{}); err == nil {
		t.Fatal("expired diagnostic Job was not deleted")
	}
	if _, err := client.CoreV1().Secrets("anban").Get(context.Background(), "active-secret", metav1.GetOptions{}); err != nil {
		t.Fatalf("active diagnostic secret was deleted: %v", err)
	}
	if _, err := client.CoreV1().Secrets("anban").Get(context.Background(), "expired-secret", metav1.GetOptions{}); err == nil {
		t.Fatal("expired diagnostic secret was not deleted")
	}
}

func TestDiagnosticRetentionActive(t *testing.T) {
	if !diagnosticRetentionActive(map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10)}, time.Now()) {
		t.Fatal("future retention deadline was not active")
	}
	if diagnosticRetentionActive(map[string]string{dshDiagnosticRetentionAnnotation: "invalid"}, time.Now()) {
		t.Fatal("invalid retention deadline was active")
	}
}

func TestWaitInteractivePodUsesConfiguredStartupTimeout(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "anban"}, Status: corev1.PodStatus{Phase: corev1.PodPending}})
	started := time.Now()
	err := waitInteractivePod(context.Background(), client, "anban", "pending", 1)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for DSH pod") {
		t.Fatalf("waitInteractivePod error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("startup timeout elapsed = %v, want at least 900ms", elapsed)
	}
}

func TestWaitInteractiveJobPodReturnsRunningJobPod(t *testing.T) {
	client := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "anban-dsh-session-1", Namespace: "anban"}, Status: batchv1.JobStatus{Active: 1}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "anban-dsh-session-1-pod", Namespace: "anban", Labels: map[string]string{"job-name": "anban-dsh-session-1"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	)
	pod, err := waitInteractiveJobPod(context.Background(), client, "anban", "anban-dsh-session-1", 1)
	if err != nil {
		t.Fatalf("waitInteractiveJobPod() error = %v", err)
	}
	if pod.Name != "anban-dsh-session-1-pod" {
		t.Fatalf("running pod = %q", pod.Name)
	}
}

func TestKubernetesRuntimeExitDetailsRetainBridgeDiagnostics(t *testing.T) {
	details := interactiveRuntimeExitDetails(nil, errors.New("stream closed"), nil, errors.New("stderr pipe"), "bridge initialization failed", "runtime-secret")
	if details["error"] != "stream closed" || details["stderr_error"] != "stderr pipe" || details["stderr"] != "bridge initialization failed" {
		t.Fatalf("runtime exit details = %#v", details)
	}
}

func TestInteractivePodDiagnosticIncludesPendingReason(t *testing.T) {
	diagnostic := interactivePodDiagnostic(&corev1.Pod{Status: corev1.PodStatus{
		Phase:   corev1.PodPending,
		Reason:  "Unschedulable",
		Message: "0/3 nodes are available: insufficient memory",
		InitContainerStatuses: []corev1.ContainerStatus{{
			Name:  "project-memory-init",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", Message: "permission denied"}},
		}},
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "dsh",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off pulling image"}},
		}},
	}})
	for _, want := range []string{"phase=Pending", "reason=Unschedulable", "insufficient memory", "project-memory-init:terminated", "exit_code=1", "permission denied", "dsh:waiting", "ImagePullBackOff", "back-off pulling image"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic %q does not contain %q", diagnostic, want)
		}
	}
}

func TestInteractivePodDiagnosticWithEventsIncludesKubeletFailure(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "failed-mount", Namespace: "anban"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "dsh-1"}, Reason: "FailedMount", Message: "MountVolume.SetUp failed for volume agent-memory: read-only file system"},
	)
	diagnostic := interactivePodDiagnosticWithEvents(context.Background(), client, "anban", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dsh-1"}, Status: corev1.PodStatus{Phase: corev1.PodFailed}})
	for _, want := range []string{"phase=Failed", "event=FailedMount: MountVolume.SetUp failed for volume agent-memory: read-only file system"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic %q does not contain %q", diagnostic, want)
		}
	}
}

func repeatHex(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = 'a'
	}
	return string(result)
}
