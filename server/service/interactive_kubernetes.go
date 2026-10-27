package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

const dshDiagnosticRetentionAnnotation = "anban.ai/dsh-diagnostic-retain-until"

// KubernetesRuntimeFactory runs one non-restarting Job per Assistant
// Workspace. The Job's dsh container is the Bridge process; the Server owns
// one authenticated attach stream and the browser never talks to Kubernetes.
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
	Logger            *zerolog.Logger
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
	if _, err := interactivePresetPatchPath(spec.AgentPackID); err != nil {
		return nil, err
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
	stageStarted := time.Now()
	secret := f.secret(spec, namespace, secretName)
	f.log(spec.SessionID, "creating DSH session Secret", nil)
	if _, err := f.Kube.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); apierrors.IsAlreadyExists(err) {
		abort()
		return nil, fmt.Errorf("DSH session secret %q already exists", secretName)
	} else if err != nil {
		f.log(spec.SessionID, "failed to create DSH session Secret", err)
		abort()
		return nil, fmt.Errorf("create DSH session secret: %w", err)
	}
	f.logStartupStage(spec.SessionID, "created DSH session Secret", stageStarted)
	stageStarted = time.Now()
	job, err := f.job(spec, namespace, name)
	if err != nil {
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, err
	}
	envs, err := dshSecretEnv(secretName, spec.SessionID, f.MCPURL, f.Config.ServerURL, spec.AgentPackID, spec.InteractionID, spec.Generation)
	if err != nil {
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, err
	}
	envs = append(envs,
		corev1.EnvVar{Name: "ANBAN_DSH_BRIDGE_WORKSPACE_ID", Value: spec.WorkspaceID},
		corev1.EnvVar{Name: "ANBAN_DSH_BRIDGE_INTERACTION_ID", Value: spec.InteractionID},
		corev1.EnvVar{Name: "ANBAN_DSH_BRIDGE_GENERATION", Value: strconv.FormatInt(spec.Generation, 10)},
		corev1.EnvVar{Name: "ANBAN_DSH_BRIDGE_PRESETS", Value: strings.Join(spec.PresetIDs, ",")},
		corev1.EnvVar{Name: "ANBAN_DSH_BRIDGE_PATCHES", Value: strings.Join(patchesForSpec(spec), ",")},
	)
	job.Spec.Template.Spec.Containers[0].Env = envs
	created, err := f.Kube.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, fmt.Errorf("DSH session Job %q already exists", name)
	}
	if err != nil {
		f.log(spec.SessionID, "failed to create DSH Job", err)
		abort()
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return nil, fmt.Errorf("create DSH Job: %w", err)
	}
	f.logStartupStage(spec.SessionID, "created DSH Job", stageStarted)
	stageStarted = time.Now()
	pod, err := waitInteractiveJobPod(streamCtx, f.Kube, namespace, created.Name, f.Config.StartupTimeoutSec)
	if err != nil {
		f.log(spec.SessionID, "DSH Job Pod did not become ready", err)
		abort()
		f.retainOrDeleteDiagnosticResources(namespace, created.Name, "", secretName)
		return nil, err
	}
	f.logStartupStage(spec.SessionID, "DSH Pod running", stageStarted)
	stageStarted = time.Now()
	request := f.Kube.CoreV1().RESTClient().Post().Namespace(namespace).Resource("pods").Name(pod.Name).SubResource("attach")
	request.VersionedParams(&corev1.PodAttachOptions{
		Container: "dsh", Stdin: true, Stdout: true, Stderr: true, TTY: false,
	}, schemeParameterCodec())
	attach, err := remotecommand.NewSPDYExecutor(f.REST, "POST", request.URL())
	if err != nil {
		abort()
		f.retainOrDeleteDiagnosticResources(namespace, created.Name, pod.Name, secretName)
		return nil, fmt.Errorf("create DSH pod attach: %w", err)
	}
	f.logStartupStage(spec.SessionID, "created DSH Bridge attach executor", stageStarted)
	streamCtx, cancel := context.WithCancel(streamCtx)
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	r := &execRuntime{in: stdinWriter, done: make(chan struct{}), pending: map[uint64]chan error{}, redactions: []string{spec.RuntimeToken, f.APIKey}}
	r.cleanup = func() {
		cancel()
		if timeoutCancel != nil {
			timeoutCancel()
		}
		_ = stdinReader.Close()
		r.mu.Lock()
		closedByOwner := r.closed
		r.mu.Unlock()
		if closedByOwner {
			_ = f.deleteJob(context.Background(), namespace, created.Name)
			_ = f.deleteSecret(context.Background(), namespace, secretName)
			f.log(spec.SessionID, "deleted DSH Job", nil)
			return
		}
		f.retainOrDeleteDiagnosticResources(namespace, created.Name, pod.Name, secretName)
	}
	if emit == nil {
		emit = func(string, any) {}
	}
	stderrTail := &interactiveTailBuffer{limit: 16 * 1024}
	stderrDone := make(chan error, 1)
	go func() { _, copyErr := io.Copy(stderrTail, stderrReader); stderrDone <- copyErr }()
	stdoutDone := make(chan error, 1)
	go func() { stdoutDone <- r.read(stdoutReader, emit) }()
	go func() {
		streamErr := attach.StreamWithContext(streamCtx, remotecommand.StreamOptions{Stdin: stdinReader, Stdout: stdoutWriter, Stderr: stderrWriter, Tty: false})
		_ = stdoutWriter.CloseWithError(streamErr)
		_ = stderrWriter.CloseWithError(streamErr)
		stdoutErr := <-stdoutDone
		stderrErr := <-stderrDone
		r.finish(interactiveRuntimeExitError(streamErr, stdoutErr, stderrErr, stderrTail.String(), spec.RuntimeToken, f.APIKey))
		details := interactiveRuntimeExitDetails(nil, streamErr, stdoutErr, stderrErr, stderrTail.String(), spec.RuntimeToken, f.APIKey)
		details["job"] = created.Name
		details["pod"] = pod.Name
		if diagnostic := f.runtimeDiagnostics(namespace, created.Name, pod.Name); diagnostic != "" {
			details["kubernetes"] = diagnostic
		}
		emit("runtime.exit", details)
	}()
	return r, nil
}

func (f KubernetesRuntimeFactory) runtimeDiagnostics(namespace, jobName, podName string) string {
	if f.Kube == nil {
		return ""
	}
	parts := []string{}
	if job, err := f.Kube.BatchV1().Jobs(namespace).Get(context.Background(), jobName, metav1.GetOptions{}); err == nil {
		parts = append(parts, interactiveJobDiagnostic(job))
	}
	if pod, err := f.Kube.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{}); err == nil {
		parts = append(parts, interactivePodDiagnosticWithEvents(context.Background(), f.Kube, namespace, pod))
	}
	return strings.Join(parts, "; ")
}

func (f KubernetesRuntimeFactory) log(sessionID, message string, err error) {
	if f.Logger == nil {
		return
	}
	event := f.Logger.Info().Str("session_id", sessionID).Str("executor", "kubernetes").Str("server_instance", strings.TrimSpace(f.ServerInstanceID))
	if err != nil {
		event = f.Logger.Error().Err(err).Str("session_id", sessionID).Str("executor", "kubernetes").Str("server_instance", strings.TrimSpace(f.ServerInstanceID))
	}
	event.Msg(message)
}

func (f KubernetesRuntimeFactory) logStartupStage(sessionID, message string, started time.Time) {
	if f.Logger == nil {
		return
	}
	f.Logger.Info().
		Str("session_id", sessionID).
		Str("executor", "kubernetes").
		Str("server_instance", strings.TrimSpace(f.ServerInstanceID)).
		Int64("duration_ms", time.Since(started).Milliseconds()).
		Msg(message)
}

func (f KubernetesRuntimeFactory) pod(spec InteractiveRuntimeSpec, namespace, name string) (*corev1.Pod, error) {
	patchPath, err := interactivePresetPatchPath(spec.AgentPackID)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.session_id": spec.SessionID, "com.anban.preset_id": spec.AgentPackID, "com.anban.server_instance": strings.TrimSpace(f.ServerInstanceID)}
	resources := dshResourceRequirements(f.Config)
	projectMemoryMount := corev1.VolumeMount{Name: "project-memory", MountPath: f.Config.KubernetesProjectMemoryMount, ReadOnly: true}
	volumeMounts := []corev1.VolumeMount{projectMemoryMount, {Name: "tmp", MountPath: "/tmp"}, {Name: "dsh-context", MountPath: "/var/run/anban-dsh-context", ReadOnly: true}}
	volumes := []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: quantityPtr("64Mi")}}}, {Name: "dsh-context", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name + "-secret", Items: []corev1.KeyToPath{{Key: "initial-context", Path: "initial-context"}}}}}}
	if spec.ProjectID != "" {
		volumes = append(volumes, corev1.Volume{Name: "project-memory", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: f.Config.KubernetesProjectMemoryClaim}}})
		volumeMounts[0].SubPath = "projects/" + safeContainerPart(spec.ProjectID)
	} else {
		volumes = append(volumes, corev1.Volume{Name: "project-memory", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	deadline := int64(f.Config.TimeoutSec)
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ActiveDeadlineSeconds: &deadline, ServiceAccountName: f.ServiceAccount, AutomountServiceAccountToken: dshBoolPtr(false), ImagePullSecrets: imagePullSecrets(f.Config.KubernetesImagePullSecret), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: dshBoolPtr(true), RunAsUser: int64Ptr(1000), RunAsGroup: int64Ptr(1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: "dsh", Image: f.Config.Image, ImagePullPolicy: corev1.PullAlways, Env: []corev1.EnvVar{{Name: "ANBAN_DSH_BRIDGE_PRESET_ID", Value: spec.AgentPackID}, {Name: "ANBAN_DSH_BRIDGE_PATCH", Value: patchPath}}, Command: []string{"node", "/app/runtime-dsh/dist/dsh-bridge.js"}, Args: []string{"--provider", f.Provider, "--model", f.Model, "--route-api", f.API, "--base-url", f.BaseURL}, WorkingDir: "/workspace/project", Stdin: true, StdinOnce: false, TTY: false, Resources: resources, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: dshBoolPtr(false), ReadOnlyRootFilesystem: dshBoolPtr(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, VolumeMounts: volumeMounts}}, Volumes: volumes}}, nil
}

func dshResourceRequirements(cfg srvconfig.DshConfig) corev1.ResourceRequirements {
	configured := cfg.Resources
	if len(configured.Requests) == 0 && len(configured.Limits) == 0 {
		configured = srvconfig.KubernetesResourceConfig{
			Requests: map[string]string{
				"cpu":    strconv.FormatInt(cfg.CPUCores, 10),
				"memory": fmt.Sprintf("%dMi", cfg.MemoryMB),
			},
			Limits: map[string]string{
				"cpu":    strconv.FormatInt(cfg.CPUCores, 10),
				"memory": fmt.Sprintf("%dMi", cfg.MemoryMB),
			},
		}
	}
	parse := func(values map[string]string) corev1.ResourceList {
		result := corev1.ResourceList{}
		for name, raw := range values {
			quantity, err := resource.ParseQuantity(strings.TrimSpace(raw))
			if err == nil && quantity.Sign() > 0 {
				result[corev1.ResourceName(name)] = quantity
			}
		}
		return result
	}
	return corev1.ResourceRequirements{Requests: parse(configured.Requests), Limits: parse(configured.Limits)}
}

func (f KubernetesRuntimeFactory) job(spec InteractiveRuntimeSpec, namespace, name string) (*batchv1.Job, error) {
	pod, err := f.pod(spec, namespace, name)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for key, value := range pod.Labels {
		labels[key] = value
	}
	labels["job-name"] = name
	backoffLimit := int32(0)
	completions := int32(1)
	parallelism := int32(1)
	return &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			Completions: &completions, Parallelism: &parallelism, BackoffLimit: &backoffLimit,
			ActiveDeadlineSeconds: pod.Spec.ActiveDeadlineSeconds,
			Selector:              &metav1.LabelSelector{MatchLabels: map[string]string{"job-name": name}},
			Template:              corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: pod.Spec},
		},
	}, nil
}

func (f KubernetesRuntimeFactory) secret(spec InteractiveRuntimeSpec, namespace, name string) *corev1.Secret {
	data := map[string]string{"runtime-token": spec.RuntimeToken, "provider-api-key": f.APIKey, "initial-context": spec.InitialContext}
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"com.anban.runtime": "dsh-interactive", "com.anban.session_id": spec.SessionID, "com.anban.server_instance": strings.TrimSpace(f.ServerInstanceID)}}, Type: corev1.SecretTypeOpaque, StringData: data}
}

func dshSecretEnv(secretName, sessionID, mcpURL, serverURL, presetID string, capability ...any) ([]corev1.EnvVar, error) {
	interactionID := ""
	var generation int64
	if len(capability) > 0 {
		interactionID, _ = capability[0].(string)
	}
	if len(capability) > 1 {
		generation, _ = capability[1].(int64)
	}
	patchPath, err := interactivePresetPatchPath(presetID)
	if err != nil {
		return nil, err
	}
	ref := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key}}
	}
	return []corev1.EnvVar{{Name: "HOME", Value: "/tmp/dsh-home"}, {Name: "DSH_HOME", Value: "/tmp/dsh-home"}, {Name: "ANBAN_DSH_BRIDGE_HOME", Value: "/tmp/dsh-home"}, {Name: "ANBAN_DSH_BRIDGE_SESSION_ID", Value: sessionID}, {Name: "ANBAN_DSH_BRIDGE_INTERACTION_ID", Value: interactionID}, {Name: "ANBAN_DSH_BRIDGE_GENERATION", Value: strconv.FormatInt(generation, 10)}, {Name: "ANBAN_DSH_BRIDGE_PRESET_ID", Value: presetID}, {Name: "ANBAN_DSH_BRIDGE_RUNTIME_TOKEN", ValueFrom: ref("runtime-token")}, {Name: "ANBAN_DSH_BRIDGE_MCP_URL", Value: strings.TrimRight(dshFirstNonEmpty(mcpURL, serverURL+"/mcp"), "/")}, {Name: "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY", ValueFrom: ref("provider-api-key")}, {Name: "ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE", Value: "/var/run/anban-dsh-context/initial-context"}, {Name: "ANBAN_DSH_BRIDGE_PATCH", Value: patchPath}}, nil
}

func (f KubernetesRuntimeFactory) CleanupOrphans(ctx context.Context) error {
	ns := strings.TrimSpace(f.Config.KubernetesNamespace)
	if f.Kube == nil || ns == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	list, err := f.Kube.BatchV1().Jobs(ns).List(cleanupCtx, metav1.ListOptions{LabelSelector: "com.anban.runtime=dsh-interactive"})
	if err != nil {
		return fmt.Errorf("list orphaned DSH jobs: %w", err)
	}
	for _, job := range list.Items {
		if diagnosticRetentionActive(job.Annotations, time.Now()) {
			continue
		}
		if err := f.deleteJob(cleanupCtx, ns, job.Name); err != nil {
			return err
		}
		if err := f.waitForJobDeletion(cleanupCtx, ns, job.Name); err != nil {
			return err
		}
	}
	secrets, err := f.Kube.CoreV1().Secrets(ns).List(cleanupCtx, metav1.ListOptions{LabelSelector: "com.anban.runtime=dsh-interactive"})
	if err != nil {
		return fmt.Errorf("list orphaned DSH secrets: %w", err)
	}
	for _, secret := range secrets.Items {
		if diagnosticRetentionActive(secret.Annotations, time.Now()) {
			continue
		}
		sessionID := strings.TrimSpace(secret.Labels["com.anban.session_id"])
		if sessionID == "" {
			continue
		}
		_, jobErr := f.Kube.BatchV1().Jobs(ns).Get(cleanupCtx, "anban-dsh-"+safeContainerPart(sessionID), metav1.GetOptions{})
		if jobErr == nil || !apierrors.IsNotFound(jobErr) {
			continue
		}
		if err := f.deleteSecret(cleanupCtx, ns, secret.Name); err != nil {
			return err
		}
	}
	return nil
}

func (f KubernetesRuntimeFactory) waitForJobDeletion(ctx context.Context, namespace, name string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := f.Kube.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("check deleted DSH job %s: %w", name, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for DSH job %s deletion: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f KubernetesRuntimeFactory) deleteJob(ctx context.Context, namespace, name string) error {
	grace := int64(0)
	err := f.Kube.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: propagationPtr(metav1.DeletePropagationForeground)})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
func (f KubernetesRuntimeFactory) deleteSecret(ctx context.Context, namespace, name string) error {
	err := f.Kube.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (f KubernetesRuntimeFactory) retainOrDeleteDiagnosticResources(namespace, jobName, podName, secretName string) {
	retention := time.Duration(f.Config.RuntimeDiagnosticRetentionSeconds) * time.Second
	if retention <= 0 {
		_ = f.deleteJob(context.Background(), namespace, jobName)
		_ = f.deleteSecret(context.Background(), namespace, secretName)
		return
	}
	deadline := time.Now().Add(retention)
	// The timer handles the common case. The annotation lets a fresh Server
	// process reclaim the same resources after a restart without retaining
	// bearer credentials indefinitely.
	_ = f.markDiagnosticJob(context.Background(), namespace, jobName, deadline)
	if podName != "" {
		_ = f.markDiagnosticPod(context.Background(), namespace, podName, deadline)
	}
	_ = f.markDiagnosticSecret(context.Background(), namespace, secretName, deadline)
	time.AfterFunc(retention, func() {
		_ = f.deleteJob(context.Background(), namespace, jobName)
		_ = f.deleteSecret(context.Background(), namespace, secretName)
	})
}

func (f KubernetesRuntimeFactory) markDiagnosticJob(ctx context.Context, namespace, name string, deadline time.Time) error {
	job, err := f.Kube.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[dshDiagnosticRetentionAnnotation] = strconv.FormatInt(deadline.Unix(), 10)
	_, err = f.Kube.BatchV1().Jobs(namespace).Update(ctx, job, metav1.UpdateOptions{})
	return err
}

func (f KubernetesRuntimeFactory) markDiagnosticPod(ctx context.Context, namespace, name string, deadline time.Time) error {
	pod, err := f.Kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[dshDiagnosticRetentionAnnotation] = strconv.FormatInt(deadline.Unix(), 10)
	_, err = f.Kube.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
	return err
}

func (f KubernetesRuntimeFactory) markDiagnosticSecret(ctx context.Context, namespace, name string, deadline time.Time) error {
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{dshDiagnosticRetentionAnnotation: strconv.FormatInt(deadline.Unix(), 10)}}})
	if err != nil {
		return err
	}
	_, err = f.Kube.CoreV1().Secrets(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func diagnosticRetentionActive(annotations map[string]string, now time.Time) bool {
	value := strings.TrimSpace(annotations[dshDiagnosticRetentionAnnotation])
	if value == "" {
		return false
	}
	deadline, err := strconv.ParseInt(value, 10, 64)
	return err == nil && now.Unix() < deadline
}

func waitInteractiveJobPod(ctx context.Context, kube kubernetes.Interface, namespace, jobName string, startupTimeoutSec int) (*corev1.Pod, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	if startupTimeoutSec <= 0 {
		startupTimeoutSec = 180
	}
	timeout := time.NewTimer(time.Duration(startupTimeoutSec) * time.Second)
	defer timeout.Stop()
	for {
		pods, listErr := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
		if listErr != nil {
			return nil, fmt.Errorf("list DSH Job pods: %w", listErr)
		}
		if len(pods.Items) > 0 {
			pod := &pods.Items[0]
			switch pod.Status.Phase {
			case corev1.PodRunning:
				return pod, nil
			case corev1.PodFailed, corev1.PodSucceeded:
				return nil, fmt.Errorf("DSH Job pod exited before attach: %s", interactivePodDiagnosticWithEvents(ctx, kube, namespace, pod))
			}
		}
		job, jobErr := kube.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if jobErr != nil && !apierrors.IsNotFound(jobErr) {
			return nil, fmt.Errorf("get DSH Job: %w", jobErr)
		}
		if job != nil && job.Status.Failed > 0 {
			return nil, fmt.Errorf("DSH Job failed before attach: %s", interactiveJobDiagnostic(job))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			var pod *corev1.Pod
			if len(pods.Items) > 0 {
				pod = &pods.Items[0]
			}
			return nil, fmt.Errorf("timed out waiting for DSH Job pod: %s", interactiveJobDiagnosticWithPod(job, pod, ctx, kube, namespace))
		case <-ticker.C:
		}
	}
}

func interactiveJobDiagnostic(job *batchv1.Job) string {
	if job == nil {
		return "job status unavailable"
	}
	parts := []string{"job=" + job.Name}
	if job.Status.Active > 0 {
		parts = append(parts, "active="+strconv.Itoa(int(job.Status.Active)))
	}
	if job.Status.Failed > 0 {
		parts = append(parts, "failed="+strconv.Itoa(int(job.Status.Failed)))
	}
	if job.Status.Succeeded > 0 {
		parts = append(parts, "succeeded="+strconv.Itoa(int(job.Status.Succeeded)))
	}
	if len(job.Status.Conditions) > 0 {
		condition := job.Status.Conditions[len(job.Status.Conditions)-1]
		parts = append(parts, "condition="+string(condition.Type))
		if condition.Reason != "" {
			parts = append(parts, "reason="+sanitizeDiagnosticValue(condition.Reason))
		}
		if condition.Message != "" {
			parts = append(parts, "message="+sanitizeDiagnosticValue(condition.Message))
		}
	}
	return strings.Join(parts, "; ")
}

func interactiveJobDiagnosticWithPod(job *batchv1.Job, pod *corev1.Pod, ctx context.Context, kube kubernetes.Interface, namespace string) string {
	diagnostic := interactiveJobDiagnostic(job)
	if pod == nil {
		return diagnostic
	}
	return diagnostic + "; " + interactivePodDiagnosticWithEvents(ctx, kube, namespace, pod)
}

func waitInteractivePod(ctx context.Context, kube kubernetes.Interface, namespace, name string, startupTimeoutSec int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	if startupTimeoutSec <= 0 {
		startupTimeoutSec = 180
	}
	timeout := time.NewTimer(time.Duration(startupTimeoutSec) * time.Second)
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
			return fmt.Errorf("DSH pod exited before exec: %s", interactivePodDiagnosticWithEvents(ctx, kube, namespace, pod))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for DSH pod: %s", interactivePodDiagnosticWithEvents(ctx, kube, namespace, pod))
		case <-ticker.C:
		}
	}
}

// interactivePodDiagnosticWithEvents includes the kubelet event that explains
// failures which do not reach a normal container state, such as FailedMount or
// CreateContainerError during init-container setup.
func interactivePodDiagnosticWithEvents(ctx context.Context, kube kubernetes.Interface, namespace string, pod *corev1.Pod) string {
	diagnostic := interactivePodDiagnostic(pod)
	if kube == nil || pod == nil || strings.TrimSpace(pod.Name) == "" {
		return diagnostic
	}
	events, err := kube.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.kind=Pod,involvedObject.name=" + pod.Name})
	if err != nil {
		return diagnostic + "; events_error=" + sanitizeDiagnosticValue(err.Error())
	}
	for _, event := range events.Items {
		parts := []string{}
		if reason := strings.TrimSpace(event.Reason); reason != "" {
			parts = append(parts, reason)
		}
		if message := strings.TrimSpace(event.Message); message != "" {
			parts = append(parts, message)
		}
		if len(parts) == 0 {
			continue
		}
		diagnostic += "; event=" + sanitizeDiagnosticValue(strings.Join(parts, ": "))
	}
	return diagnostic
}

func sanitizeDiagnosticValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.ReplaceAll(value, ";", ",")
}

func interactivePodDiagnostic(pod *corev1.Pod) string {
	if pod == nil {
		return "pod status unavailable"
	}
	parts := []string{"phase=" + string(pod.Status.Phase)}
	if reason := strings.TrimSpace(pod.Status.Reason); reason != "" {
		parts = append(parts, "reason="+reason)
	}
	if message := strings.TrimSpace(pod.Status.Message); message != "" {
		parts = append(parts, "message="+message)
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Status == corev1.ConditionTrue || strings.TrimSpace(condition.Reason) != "" || strings.TrimSpace(condition.Message) != "" {
			conditionParts := []string{string(condition.Type) + "=" + string(condition.Status)}
			if reason := strings.TrimSpace(condition.Reason); reason != "" {
				conditionParts = append(conditionParts, "reason="+reason)
			}
			if message := strings.TrimSpace(condition.Message); message != "" {
				conditionParts = append(conditionParts, "message="+message)
			}
			parts = append(parts, strings.Join(conditionParts, ","))
		}
	}
	appendContainerStatuses := func(statuses []corev1.ContainerStatus) {
		for _, status := range statuses {
			if status.State.Waiting != nil {
				waiting := []string{status.Name + ":waiting"}
				if reason := strings.TrimSpace(status.State.Waiting.Reason); reason != "" {
					waiting = append(waiting, "reason="+reason)
				}
				if message := strings.TrimSpace(status.State.Waiting.Message); message != "" {
					waiting = append(waiting, "message="+message)
				}
				parts = append(parts, strings.Join(waiting, ","))
			}
			if status.State.Terminated != nil {
				terminated := []string{status.Name + ":terminated", fmt.Sprintf("exit_code=%d", status.State.Terminated.ExitCode)}
				if reason := strings.TrimSpace(status.State.Terminated.Reason); reason != "" {
					terminated = append(terminated, "reason="+reason)
				}
				if message := strings.TrimSpace(status.State.Terminated.Message); message != "" {
					terminated = append(terminated, "message="+message)
				}
				parts = append(parts, strings.Join(terminated, ","))
			}
		}
	}
	appendContainerStatuses(pod.Status.InitContainerStatuses)
	appendContainerStatuses(pod.Status.ContainerStatuses)
	return strings.Join(parts, "; ")
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
