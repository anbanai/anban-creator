package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

// KubernetesRuntimeFactory runs one non-restarting Pod per interactive
// session. The Server owns the pod and attaches a single authenticated exec
// stream; the browser never talks to Kubernetes directly.
type KubernetesRuntimeFactory struct {
	Config            srvconfig.DshConfig
	ServiceAccount    string
	Kube              kubernetes.Interface
	REST              *rest.Config
	ProjectMemoryRoot string
	ServerInstanceID  string
	Provider          string
	API               string
	BaseURL           string
	APIKey            string
	Model             string
	MCPURL            string
}

var _ RuntimeFactory = (*KubernetesRuntimeFactory)(nil)
var _ RuntimeFactoryWithSession = (*KubernetesRuntimeFactory)(nil)

func (f KubernetesRuntimeFactory) Start(ctx context.Context, sessionID, agentID, runtimeToken string, emit func(string, any)) (Runtime, error) {
	return f.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, AgentPackID: agentID, RuntimeToken: runtimeToken}, emit)
}

func (f KubernetesRuntimeFactory) StartSession(ctx context.Context, spec InteractiveRuntimeSpec, emit func(string, any)) (Runtime, error) {
	if f.Kube == nil || f.REST == nil {
		return nil, errors.New("kubernetes DSH client is not configured")
	}
	if strings.TrimSpace(spec.SessionID) == "" || strings.TrimSpace(spec.RuntimeToken) == "" {
		return nil, errors.New("interactive session id and runtime token are required")
	}
	if strings.TrimSpace(f.ServiceAccount) == "" {
		return nil, errors.New("Claude Kubernetes service account is required")
	}
	namespace := strings.TrimSpace(f.Config.KubernetesNamespace)
	if namespace == "" {
		return nil, errors.New("dsh kubernetes namespace is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx := ctx
	var timeoutCancel context.CancelFunc
	if f.Config.TimeoutSec > 0 {
		streamCtx, timeoutCancel = context.WithTimeout(ctx, time.Duration(f.Config.TimeoutSec)*time.Second)
	}
	abort := func() {
		if timeoutCancel != nil {
			timeoutCancel()
		}
	}
	name := "anban-dsh-" + safeContainerPart(spec.SessionID)
	secretName := name + "-secret"
	secret := f.secret(spec, namespace, secretName)
	if _, err := f.Kube.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); apierrors.IsAlreadyExists(err) {
		abort()
		return nil, fmt.Errorf("DSH session secret %q already exists", secretName)
	} else if err != nil {
		abort()
		return nil, fmt.Errorf("create DSH session secret: %w", err)
	}
	pod := f.pod(spec, namespace, name)
	pod.Spec.Containers[0].Env = dshSecretEnv(secretName, spec.SessionID, f.MCPURL, f.Config.ServerURL)
	created, err := f.Kube.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, fmt.Errorf("DSH session pod %q already exists", name)
	}
	if err != nil {
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, fmt.Errorf("create DSH pod: %w", err)
	}
	if err := waitInteractivePod(streamCtx, f.Kube, namespace, created.Name); err != nil {
		abort()
		_ = f.deletePod(context.Background(), namespace, created.Name)
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, err
	}
	request := f.Kube.CoreV1().RESTClient().Post().Namespace(namespace).Resource("pods").Name(created.Name).SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: "dsh",
		Command:   []string{"node", "/app/runtime-dsh/dist/dsh-bridge.js", "--provider", f.Provider, "--model", f.Model, "--route-api", f.API, "--base-url", f.BaseURL},
		Stdin:     true, Stdout: true, Stderr: true, TTY: false,
	}, schemeParameterCodec())
	exec, err := remotecommand.NewSPDYExecutor(f.REST, "POST", request.URL())
	if err != nil {
		abort()
		_ = f.deletePod(context.Background(), namespace, created.Name)
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, fmt.Errorf("create DSH pod exec: %w", err)
	}
	streamCtx, cancel := context.WithCancel(streamCtx)
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	r := &execRuntime{in: stdinWriter, done: make(chan struct{}), pending: map[uint64]chan error{}, redactions: []string{spec.RuntimeToken, f.APIKey}, cleanup: func() {
		cancel()
		if timeoutCancel != nil {
			timeoutCancel()
		}
		_ = stdinReader.Close()
		_ = f.deletePod(context.Background(), namespace, created.Name)
		_ = f.deleteSecret(context.Background(), namespace, secretName)
	}}
	if emit == nil {
		emit = func(string, any) {}
	}
	stderrTail := &interactiveTailBuffer{limit: 16 * 1024}
	go func() { _, _ = io.Copy(stderrTail, stderrReader) }()
	stdoutDone := make(chan error, 1)
	go func() { stdoutDone <- r.read(stdoutReader, emit) }()
	go func() {
		streamErr := exec.StreamWithContext(streamCtx, remotecommand.StreamOptions{Stdin: stdinReader, Stdout: stdoutWriter, Stderr: stderrWriter, Tty: false})
		_ = stdoutWriter.CloseWithError(streamErr)
		_ = stderrWriter.CloseWithError(streamErr)
		stdoutErr := <-stdoutDone
		r.finish(interactiveRuntimeExitError(streamErr, stdoutErr, nil, stderrTail.String(), spec.RuntimeToken, f.APIKey))
		emit("runtime.exit", map[string]any{"error": redactInteractiveRuntimeDiagnostic(fmt.Sprint(streamErr), spec.RuntimeToken, f.APIKey), "pod": created.Name})
	}()
	return r, nil
}

func (f KubernetesRuntimeFactory) pod(spec InteractiveRuntimeSpec, namespace, name string) *corev1.Pod {
	labels := map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.session_id": spec.SessionID, "com.anban.server_instance": strings.TrimSpace(f.ServerInstanceID)}
	resources := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	if q, err := resource.ParseQuantity(fmt.Sprintf("%d", f.Config.CPUCores)); err == nil {
		resources.Limits[corev1.ResourceCPU] = q
		resources.Requests[corev1.ResourceCPU] = q
	}
	if q, err := resource.ParseQuantity(fmt.Sprintf("%dMi", f.Config.MemoryMB)); err == nil {
		resources.Limits[corev1.ResourceMemory] = q
		resources.Requests[corev1.ResourceMemory] = q
	}
	volumeMounts := []corev1.VolumeMount{{Name: "project-memory", MountPath: f.Config.KubernetesProjectMemoryMount, ReadOnly: true}, {Name: "agent-memory", MountPath: f.Config.KubernetesProjectMemoryMount + "/.claude/agent-memory/project-creator"}, {Name: "tmp", MountPath: "/tmp"}}
	volumes := []corev1.Volume{{Name: "agent-memory", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: quantityPtr("16Mi")}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: quantityPtr("64Mi")}}}}
	if spec.ProjectID != "" {
		volumes = append(volumes, corev1.Volume{Name: "project-memory", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: f.Config.KubernetesProjectMemoryClaim}}})
		volumeMounts[0].SubPath = "projects/" + safeContainerPart(spec.ProjectID)
	} else {
		volumes = append(volumes, corev1.Volume{Name: "project-memory", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	deadline := int64(f.Config.TimeoutSec)
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ActiveDeadlineSeconds: &deadline, ServiceAccountName: f.ServiceAccount, AutomountServiceAccountToken: dshBoolPtr(false), ImagePullSecrets: imagePullSecrets(f.Config.KubernetesImagePullSecret), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: dshBoolPtr(true), RunAsUser: int64Ptr(1000), RunAsGroup: int64Ptr(1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: "dsh", Image: f.Config.Image, ImagePullPolicy: corev1.PullIfNotPresent, Env: nil, Command: []string{"tail", "-f", "/dev/null"}, WorkingDir: "/workspace/project", Resources: resources, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: dshBoolPtr(false), ReadOnlyRootFilesystem: dshBoolPtr(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, VolumeMounts: volumeMounts}}, Volumes: volumes}}
}

func (f KubernetesRuntimeFactory) secret(spec InteractiveRuntimeSpec, namespace, name string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.session_id": spec.SessionID, "com.anban.server_instance": strings.TrimSpace(f.ServerInstanceID)}}, Type: corev1.SecretTypeOpaque, StringData: map[string]string{"runtime-token": spec.RuntimeToken, "provider-api-key": f.APIKey}}
}

func dshSecretEnv(secretName, sessionID, mcpURL, serverURL string) []corev1.EnvVar {
	ref := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key}}
	}
	return []corev1.EnvVar{{Name: "HOME", Value: "/tmp/dsh-home"}, {Name: "DSH_HOME", Value: "/tmp/dsh-home"}, {Name: "ANBAN_DSH_BRIDGE_HOME", Value: "/tmp/dsh-home"}, {Name: "ANBAN_DSH_BRIDGE_SESSION_ID", Value: sessionID}, {Name: "ANBAN_DSH_BRIDGE_RUNTIME_TOKEN", ValueFrom: ref("runtime-token")}, {Name: "ANBAN_DSH_BRIDGE_MCP_URL", Value: strings.TrimRight(dshFirstNonEmpty(mcpURL, serverURL+"/api/mcp"), "/")}, {Name: "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY", ValueFrom: ref("provider-api-key")}, {Name: "ANBAN_DSH_BRIDGE_PATCH", Value: "/app/harness/dsh/project-creator.patch.yml"}}
}

func (f KubernetesRuntimeFactory) CleanupOrphans(ctx context.Context) error {
	ns := strings.TrimSpace(f.Config.KubernetesNamespace)
	if f.Kube == nil || ns == "" {
		return nil
	}
	list, err := f.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "com.anban.runtime=dsh-interactive"})
	if err != nil {
		return fmt.Errorf("list orphaned DSH pods: %w", err)
	}
	for _, pod := range list.Items {
		if instance := strings.TrimSpace(f.ServerInstanceID); instance != "" && pod.Labels["com.anban.server_instance"] != instance {
			continue
		}
		if err := f.deletePod(ctx, ns, pod.Name); err != nil {
			return err
		}
	}
	secrets, err := f.Kube.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{LabelSelector: "com.anban.runtime=dsh-interactive"})
	if err != nil {
		return fmt.Errorf("list orphaned DSH secrets: %w", err)
	}
	for _, secret := range secrets.Items {
		if instance := strings.TrimSpace(f.ServerInstanceID); instance != "" && secret.Labels["com.anban.server_instance"] != instance {
			continue
		}
		sessionID := strings.TrimSpace(secret.Labels["com.anban.session_id"])
		if sessionID == "" {
			continue
		}
		_, podErr := f.Kube.CoreV1().Pods(ns).Get(ctx, "anban-dsh-"+safeContainerPart(sessionID), metav1.GetOptions{})
		if podErr == nil || !apierrors.IsNotFound(podErr) {
			continue
		}
		if err := f.deleteSecret(ctx, ns, secret.Name); err != nil {
			return err
		}
	}
	return nil
}

func (f KubernetesRuntimeFactory) deletePod(ctx context.Context, namespace, name string) error {
	grace := int64(0)
	return f.Kube.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: propagationPtr(metav1.DeletePropagationBackground)})
}
func (f KubernetesRuntimeFactory) deleteSecret(ctx context.Context, namespace, name string) error {
	return f.Kube.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}
func waitInteractivePod(ctx context.Context, kube kubernetes.Interface, namespace, name string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(60 * time.Second)
	defer timeout.Stop()
	for {
		pod, err := kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get DSH pod: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("DSH pod exited before exec: %s", pod.Status.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return errors.New("timed out waiting for DSH pod")
		case <-ticker.C:
		}
	}
}
func quantityPtr(value string) *resource.Quantity                             { q := resource.MustParse(value); return &q }
func dshBoolPtr(v bool) *bool                                                 { return &v }
func int64Ptr(v int64) *int64                                                 { return &v }
func propagationPtr(v metav1.DeletionPropagation) *metav1.DeletionPropagation { return &v }
func imagePullSecrets(name string) []corev1.LocalObjectReference {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	return []corev1.LocalObjectReference{{Name: name}}
}
func schemeParameterCodec() runtime.ParameterCodec { return scheme.ParameterCodec }
