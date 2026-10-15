package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestKubernetesRuntimeFactoryPodIsolatedAndReadOnly(t *testing.T) {
	factory := KubernetesRuntimeFactory{Config: srvconfig.DshConfig{
		Image: "registry.example/dsh@sha256:" + repeatHex(64), KubernetesNamespace: "anban",
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

func TestKubernetesRuntimeFactoryCleanupOrphans(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dsh-1", Namespace: "anban", Labels: map[string]string{"com.anban.runtime": "dsh-interactive"}}})
	factory := KubernetesRuntimeFactory{Kube: client, REST: &rest.Config{}, Config: srvconfig.DshConfig{KubernetesNamespace: "anban"}}
	if err := factory.CleanupOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("anban").Get(context.Background(), "dsh-1", metav1.GetOptions{}); err == nil {
		t.Fatal("orphan pod was not deleted")
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
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "dsh",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off pulling image"}},
		}},
	}})
	for _, want := range []string{"phase=Pending", "reason=Unschedulable", "insufficient memory", "dsh:waiting", "ImagePullBackOff", "back-off pulling image"} {
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
