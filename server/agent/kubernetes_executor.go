package agent

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"
)

var _ TaskExecutor = (*KubernetesExecutor)(nil)

const (
	kubernetesAgentContainerName           = kubernetesAgentAppName
	kubernetesAgentPodReconcileMaxAttempts = 5
	kubernetesPodTTLAnnotation             = "anban.ai/pod-ttl-seconds"
	kubernetesPodRevisionAnnotation        = "anban.ai/pod-revision"
	kubernetesPodConfigHashAnnotation      = "anban.ai/pod-config-hash"
)

var errKubernetesAgentPodNeedsReconciliation = errors.New("agent pod needs reconciliation")

type KubernetesExecutor struct {
	logger            *zerolog.Logger
	imageAPICfg       *srvconfig.ImageAPIConfig
	claudeEnv         map[string]string
	kubeCfg           srvconfig.KubernetesConfig
	serverURL         string
	defaultModel      string
	keyProvider       UserKeyProvider
	maxTurnsOverrides map[string]int
	store             storage.Provider
	memoryMgr         *projectmemory.ProjectMemoryManager
	restConfig        *rest.Config
	kube              kubeclient.Interface
}

func NewKubernetesExecutor(
	logger *zerolog.Logger,
	imageAPICfg *srvconfig.ImageAPIConfig,
	claudeEnv map[string]string,
	kubeCfg srvconfig.KubernetesConfig,
	serverURL string,
	defaultModel string,
	keyProvider UserKeyProvider,
	maxTurnsOverrides map[string]int,
	store storage.Provider,
	memoryMgr *projectmemory.ProjectMemoryManager,
) (*KubernetesExecutor, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes in-cluster config: %w", err)
	}
	clientset, err := kubeclient.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return &KubernetesExecutor{
		logger:            logger,
		imageAPICfg:       imageAPICfg,
		claudeEnv:         filterAgentEnv(claudeEnv),
		kubeCfg:           kubeCfg,
		serverURL:         strings.TrimRight(serverURL, "/"),
		defaultModel:      defaultModel,
		keyProvider:       keyProvider,
		maxTurnsOverrides: maxTurnsOverrides,
		store:             store,
		memoryMgr:         memoryMgr,
		restConfig:        restCfg,
		kube:              clientset,
	}, nil
}

func (e *KubernetesExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	if e == nil || e.kube == nil || e.restConfig == nil {
		return nil, fmt.Errorf("kubernetes executor is not configured")
	}
	if opts == nil || opts.Task == nil {
		return nil, fmt.Errorf("execution task is required")
	}
	agentModel := e.defaultModel
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type, e.maxTurnsOverrides)
	}
	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Prompt, agentModel, maxTurns)
	}
	apiKey, err := e.resolveAgentAPIKey(ctx, opts)
	if err != nil {
		return nil, err
	}

	workDir := kubernetesWorkspacePath(e.kubeCfg.WorkspaceMountPath, opts.Task)
	bundleDir, cleanupBundle, err := e.prepareWorkspaceBundle(ctx, opts, workDir)
	if err != nil {
		return nil, err
	}
	defer cleanupBundle()

	podName, err := e.ensureAgentPod(ctx, opts)
	if err != nil {
		return nil, err
	}
	cmd := e.buildAgentCommand(opts, agentModel, maxTurns, workDir, apiKey)
	env := e.buildAgentEnv(opts)

	if err := e.copyWorkspaceBundle(ctx, podName, workDir, bundleDir); err != nil {
		return nil, err
	}

	execRes := e.execAgentCommand(ctx, opts.Task.ID, podName, workDir, cmd, env, opts.HeartbeatFunc)
	if opts.LogWriter != nil {
		if execRes.stderr.Len() > 0 {
			opts.LogWriter.WriteStderr(strings.TrimSpace(execRes.stderr.String()))
		}
	}

	result := &ExecutionResult{Success: false, WorkDir: workDir, RemoteArtifacts: true}
	if parsed, err := parseAgentResult(execRes.stdout.String()); err == nil {
		result = parsed
	} else if execRes.err == nil {
		execRes.err = fmt.Errorf("parse agent result: %w", err)
	}

	result.WorkDir = workDir
	result.RemoteArtifacts = true
	result.Model = agentModel
	if execRes.err != nil {
		if result.Error == "" {
			result.Error = execRes.err.Error()
		}
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(result.Error)
			opts.LogWriter.WriteResult(false, result.DurationMs, result.NumTurns, result.TotalCostUSD, result.TokenUsage)
		}
		return result, nil
	}
	if result.Success && e.memoryMgr != nil && e.memoryMgr.Enabled() && strings.TrimSpace(opts.AutoMemoryDirectory) != "" {
		if archive, err := e.captureProjectMemoryArchive(ctx, podName, opts.AutoMemoryDirectory); err != nil {
			if e.logger != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to capture remote project memory archive")
			}
		} else if len(archive) > 0 {
			result.RemoteMemoryArchive = archive
		}
	}
	if opts.LogWriter != nil {
		opts.LogWriter.WriteResult(result.Success, result.DurationMs, result.NumTurns, result.TotalCostUSD, result.TokenUsage)
	}
	return result, nil
}

func (e *KubernetesExecutor) prepareWorkspaceBundle(ctx context.Context, opts *ExecutionOptions, podWorkDir string) (string, func(), error) {
	if opts == nil || opts.Task == nil {
		return "", func() {}, fmt.Errorf("execution task is required")
	}
	bundleDir, err := os.MkdirTemp("", "anban-kubernetes-workspace-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create kubernetes workspace bundle: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(bundleDir) }
	fail := func(err error) (string, func(), error) {
		cleanup()
		return "", func() {}, err
	}

	if opts.Project != nil {
		effectiveProject := EffectiveProject(opts.Project, opts.Task)
		resolved := resolver.ResolveStyle(effectiveProject, opts.Task)
		cfg, err := BuildAppConfig(effectiveProject, resolved, e.imageAPICfg, opts.Task.ImageRatio, opts.Task.SkipReferenceImage, opts.Task.ReferenceImageURL)
		if err != nil {
			return fail(fmt.Errorf("build app config: %w", err))
		}
		if err := writeSettingsJSON(bundleDir, cfg); err != nil {
			return fail(fmt.Errorf("write settings: %w", err))
		}
		if err := writeProjectCLAUDEMD(bundleDir, effectiveProject); err != nil && e.logger != nil {
			e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to stage project CLAUDE.md, continuing")
		}
		if e.memoryMgr != nil && e.memoryMgr.Enabled() {
			runtimeDir, err := e.memoryMgr.Stage(ctx, effectiveProject.ID, opts.Task.ID, bundleDir)
			if err != nil {
				return fail(fmt.Errorf("stage project memory: %w", err))
			}
			opts.AutoMemoryDirectory = kubernetesBundlePath(podWorkDir, bundleDir, runtimeDir)
		}
		if opts.Task.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, bundleDir, opts.Task.ReferenceImageURL); err != nil && e.logger != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to stage task reference image")
			}
		} else if opts.Project.ReferenceImageURL != "" && !opts.Task.SkipReferenceImage {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, bundleDir, opts.Project.ReferenceImageURL); err != nil && e.logger != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to stage project reference image")
			}
		}
	}

	if opts.Task.Type == model.PlatformEcommerce {
		photos := opts.Task.Ecommerce.Data().ProductPhotos
		if n := DownloadProductImages(ctx, e.store, e.logger, bundleDir, photos); n == 0 && len(photos) > 0 {
			return fail(fmt.Errorf("ecommerce task: %d product photo(s) provided but none could be downloaded to the workspace; aborting to avoid inconsistent output", len(photos)))
		}
	}
	if attachments := opts.Task.InputAttachments.Data(); len(attachments) > 0 {
		if _, err := MaterializeResumeInputs(ctx, e.store, e.logger, bundleDir, attachments); err != nil {
			return fail(fmt.Errorf("materialize resume inputs: %w", err))
		}
		if n := DownloadInputAttachments(ctx, e.store, e.logger, bundleDir, attachments); n == 0 && hasNonResumeInputAttachments(attachments) && e.logger != nil {
			e.logger.Warn().Str("task_id", opts.Task.ID).Int("provided", len(attachments)).Msg("no AI entry input attachments could be materialized")
		}
	}
	if err := writeMontageInputJSON(bundleDir, opts.Task); err != nil {
		return fail(err)
	}
	if err := writeMontageRuntimeFiles(bundleDir, opts); err != nil {
		return fail(err)
	}

	return bundleDir, cleanup, nil
}

func kubernetesBundlePath(podWorkDir, bundleDir, localPath string) string {
	if strings.TrimSpace(localPath) == "" {
		return ""
	}
	rel, err := filepath.Rel(bundleDir, localPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return filepath.ToSlash(localPath)
	}
	return path.Join(podWorkDir, filepath.ToSlash(rel))
}

func (e *KubernetesExecutor) resolveAgentAPIKey(ctx context.Context, opts *ExecutionOptions) (string, error) {
	if e.keyProvider == nil {
		return "", fmt.Errorf("agent API key provider is not configured")
	}
	if opts.Task.UserID != "" {
		if rawKey, err := e.keyProvider.EnsureUserKey(ctx, opts.Task.UserID); err == nil {
			return rawKey, nil
		} else if e.logger != nil {
			e.logger.Error().Err(err).
				Str("task_id", opts.Task.ID).
				Str("user_id", opts.Task.UserID).
				Msg("failed to resolve user API key for Kubernetes agent, falling back to system key")
		}
	}
	rawKey, err := e.keyProvider.EnsureSystemKey(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve system API key: %w", err)
	}
	return rawKey, nil
}

func (e *KubernetesExecutor) buildAgentCommand(opts *ExecutionOptions, agentModel string, maxTurns int, workspace, apiKey string) []string {
	cmd := []string{
		AgentBinaryName,
		"run",
		"--server-url", strings.TrimRight(e.serverURL, "/"),
		"--api-key", apiKey,
		"--task-id", opts.Task.ID,
		"--task-type", opts.Task.Type,
		"--topic", opts.Task.Prompt,
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--workspace", workspace,
		"--agent-flag", "anban:" + TaskToAgent(opts.Task),
		"--artifact-upload-mode", "direct",
	}
	if strings.TrimSpace(opts.Task.Goal) != "" {
		cmd = append(cmd, "--goal", opts.Task.Goal)
	}
	if opts.Task.Type == model.PlatformSeednote {
		cmd = append(cmd,
			"--has-content-image="+strconv.FormatBool(opts.Task.HasContentImage),
			"--has-tail-image="+strconv.FormatBool(opts.Task.HasTailImage),
		)
	}
	if opts.Task.Type == model.PlatformArticle {
		cmd = append(cmd,
			"--article-with-cover="+strconv.FormatBool(opts.Task.ArticleWithCover == nil || *opts.Task.ArticleWithCover),
			"--article-with-content-images="+strconv.FormatBool(opts.Task.ArticleWithContentImages == nil || *opts.Task.ArticleWithContentImages),
		)
	}
	if strings.TrimSpace(opts.AutoMemoryDirectory) != "" {
		cmd = append(cmd, "--auto-memory-directory", opts.AutoMemoryDirectory)
	}
	if agentModel != "" {
		cmd = append(cmd, "--model", agentModel)
	}
	return cmd
}

func (e *KubernetesExecutor) buildAgentEnv(opts *ExecutionOptions) []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	for k, v := range e.claudeEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	if opts != nil && opts.Project != nil {
		env = append(env, fmt.Sprintf("ANBAN_DEFAULT_PROJECT=%s", opts.Project.ID))
	}
	env = append(env, fmt.Sprintf("ANBAN_API_URL=%s", strings.TrimRight(e.serverURL, "/")))
	env = append(env, fmt.Sprintf("%s=%s", MontageSubmoduleEnvName, ContainerMontageSubmodulePath))
	for key, value := range montageProviderEnvForTask(opts) {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

func (e *KubernetesExecutor) buildAgentPod(opts *ExecutionOptions) *corev1.Pod {
	task := opts.Task
	labels := kubernetesAgentLabels(task)
	delete(labels, kubernetesTaskIDLabel)
	labels[kubernetesProjectIDLabel] = kubernetesLabelValue(task.ProjectID)
	fsGroup := int64(1000)
	nodeUser := int64(1000)
	rootUser := int64(0)
	automountServiceAccountToken := false
	projectDir := kubernetesProjectWorkspaceRoot(e.kubeCfg.WorkspaceMountPath, task)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   kubernetesAgentPodName(task),
			Labels: labels,
			Annotations: map[string]string{
				kubernetesPodTTLAnnotation:        fmt.Sprintf("%d", e.kubeCfg.PodTTLSeconds),
				kubernetesPodRevisionAnnotation:   e.kubeCfg.PodRevision,
				kubernetesPodConfigHashAnnotation: e.kubernetesPodConfigHash(opts),
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           e.kubeCfg.ServiceAccount,
			AutomountServiceAccountToken: &automountServiceAccountToken,
			RestartPolicy:                corev1.RestartPolicyAlways,
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: &fsGroup,
			},
			InitContainers: []corev1.Container{{
				Name:            "workspace-permissions",
				Image:           e.kubeCfg.AgentImage,
				ImagePullPolicy: corev1.PullAlways,
				Command:         []string{"/bin/sh", "-lc", kubernetesWorkspaceInitScript(projectDir)},
				SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUser},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      kubernetesWorkspaceMountName,
					MountPath: e.kubeCfg.WorkspaceMountPath,
				}},
			}},
			Containers: []corev1.Container{{
				Name:            kubernetesAgentContainerName,
				Image:           e.kubeCfg.AgentImage,
				ImagePullPolicy: corev1.PullAlways,
				Command:         []string{"/bin/sh", "-c", "trap : TERM INT; sleep infinity & wait"},
				SecurityContext: &corev1.SecurityContext{RunAsUser: &nodeUser},
				Env:             kubernetesEnvVars(e.buildAgentEnv(opts)),
				VolumeMounts: []corev1.VolumeMount{{
					Name:      kubernetesWorkspaceMountName,
					MountPath: e.kubeCfg.WorkspaceMountPath,
				}},
				Resources: corev1.ResourceRequirements{
					Requests: kubernetesResourceList(e.kubeCfg.Resources.Requests),
					Limits:   kubernetesResourceList(e.kubeCfg.Resources.Limits),
				},
			}},
			Volumes: []corev1.Volume{{
				Name: kubernetesWorkspaceMountName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: e.kubeCfg.WorkspacePVCName,
					},
				},
			}},
		},
	}
	if strings.TrimSpace(e.kubeCfg.ImagePullSecret) != "" {
		pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: e.kubeCfg.ImagePullSecret}}
	}
	return pod
}

func kubernetesProjectWorkspaceRoot(mountPath string, task *model.Task) string {
	userID := "unknown-user"
	projectID := "unknown-project"
	if task != nil {
		if task.UserID != "" {
			userID = task.UserID
		}
		if task.ProjectID != "" {
			projectID = task.ProjectID
		}
	}
	return path.Join(strings.TrimRight(mountPath, "/"), "users", kubernetesSafePathPart(userID), "projects", kubernetesSafePathPart(projectID))
}

func kubernetesWorkspaceInitScript(projectDir string) string {
	return "mkdir -p " + shellQuote(projectDir) + " && chown -R node:node " + shellQuote(projectDir)
}

func (e *KubernetesExecutor) kubernetesPodConfigHash(opts *ExecutionOptions) string {
	var parts []string
	parts = append(parts,
		"image="+e.kubeCfg.AgentImage,
		"serviceAccount="+e.kubeCfg.ServiceAccount,
		"imagePullSecret="+e.kubeCfg.ImagePullSecret,
		"podRevision="+e.kubeCfg.PodRevision,
		"workspaceMountPath="+e.kubeCfg.WorkspaceMountPath,
		"workspacePVCName="+e.kubeCfg.WorkspacePVCName,
		"serverURL="+strings.TrimRight(e.serverURL, "/"),
	)
	parts = appendResourceParts(parts, "requests", e.kubeCfg.Resources.Requests)
	parts = appendResourceParts(parts, "limits", e.kubeCfg.Resources.Limits)
	env := e.buildAgentEnv(opts)
	sort.Strings(env)
	for _, pair := range env {
		parts = append(parts, "env="+pair)
	}
	return kubernetesHashSuffix(strings.Join(parts, "\x00"))
}

func appendResourceParts(parts []string, prefix string, values map[string]string) []string {
	if len(values) == 0 {
		return parts
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, prefix+"."+key+"="+values[key])
	}
	return parts
}

func kubernetesPodConfigMatches(existing, desired *corev1.Pod) bool {
	if existing == nil || desired == nil {
		return false
	}
	if existing.Annotations[kubernetesPodConfigHashAnnotation] != desired.Annotations[kubernetesPodConfigHashAnnotation] {
		return false
	}
	if len(existing.Spec.Containers) == 0 || len(desired.Spec.Containers) == 0 {
		return false
	}
	existingSecurity := existing.Spec.Containers[0].SecurityContext
	desiredSecurity := desired.Spec.Containers[0].SecurityContext
	if existingSecurity == nil || desiredSecurity == nil || existingSecurity.RunAsUser == nil || desiredSecurity.RunAsUser == nil {
		return false
	}
	return *existingSecurity.RunAsUser == *desiredSecurity.RunAsUser
}

func kubernetesEnvVars(env []string) []corev1.EnvVar {
	var vars []corev1.EnvVar
	for _, pair := range env {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		vars = append(vars, corev1.EnvVar{Name: key, Value: value})
	}
	return vars
}

func kubernetesResourceList(values map[string]string) corev1.ResourceList {
	if len(values) == 0 {
		return nil
	}
	out := corev1.ResourceList{}
	for name, raw := range values {
		q, err := resource.ParseQuantity(raw)
		if err != nil {
			continue
		}
		out[corev1.ResourceName(name)] = q
	}
	return out
}

func (e *KubernetesExecutor) ensureAgentPod(ctx context.Context, opts *ExecutionOptions) (string, error) {
	pod := e.buildAgentPod(opts)
	pod.Namespace = e.kubeCfg.Namespace
	pods := e.kube.CoreV1().Pods(e.kubeCfg.Namespace)
	createAgentPod := func(action string) (bool, error) {
		if _, err := pods.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return false, nil
			}
			return false, fmt.Errorf("%s: %w", action, err)
		}
		return true, nil
	}
	deleteObservedAgentPod := func(observed *corev1.Pod, reason string) error {
		if observed.UID == "" {
			return fmt.Errorf("%s agent pod %s UID is empty; refusing deletion", reason, observed.Name)
		}
		if observed.ResourceVersion == "" {
			return fmt.Errorf("%s agent pod %s resourceVersion is empty; refusing deletion", reason, observed.Name)
		}
		uid := observed.UID
		resourceVersion := observed.ResourceVersion
		deleteErr := pods.Delete(ctx, observed.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID:             &uid,
				ResourceVersion: &resourceVersion,
			},
		})
		if apierrors.IsConflict(deleteErr) || apierrors.IsInvalid(deleteErr) || apierrors.IsNotFound(deleteErr) {
			return nil
		}
		if deleteErr != nil {
			return fmt.Errorf("delete %s agent pod: %w", reason, deleteErr)
		}
		return e.waitForAgentPodDeleted(ctx, observed.Name, uid)
	}
	createAction := "create agent pod"
	for attempt := 1; attempt <= kubernetesAgentPodReconcileMaxAttempts; attempt++ {
		existing, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return "", fmt.Errorf("get agent pod: %w", err)
			}
			created, err := createAgentPod(createAction)
			if err != nil {
				return "", err
			}
			if !created {
				continue
			}
		} else {
			if existing.DeletionTimestamp != nil {
				if existing.UID == "" {
					return "", fmt.Errorf("terminating agent pod %s UID is empty; refusing deletion wait", pod.Name)
				}
				if err := e.waitForAgentPodDeleted(ctx, pod.Name, existing.UID); err != nil {
					return "", err
				}
				createAction = "recreate deleted agent pod"
				continue
			}
			replaceReason := ""
			if existing.Status.Phase == corev1.PodFailed || existing.Status.Phase == corev1.PodSucceeded {
				replaceReason = "terminal"
			} else if !kubernetesPodConfigMatches(existing, pod) {
				replaceReason = "drifted"
			}
			if replaceReason != "" {
				if err := deleteObservedAgentPod(existing, replaceReason); err != nil {
					return "", err
				}
				createAction = "recreate agent pod"
				continue
			}
		}
		if err := e.waitForAgentPodReady(ctx, pod); err != nil {
			if errors.Is(err, errKubernetesAgentPodNeedsReconciliation) {
				continue
			}
			return "", err
		}
		return pod.Name, nil
	}
	return "", fmt.Errorf("reconcile agent pod %s exhausted after %d attempts", pod.Name, kubernetesAgentPodReconcileMaxAttempts)
}

func (e *KubernetesExecutor) waitForAgentPodDeleted(ctx context.Context, podName string, expectedUID types.UID) error {
	if expectedUID == "" {
		return fmt.Errorf("wait for agent pod %s deletion: expected UID is empty", podName)
	}
	return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pod, err := e.kube.CoreV1().Pods(e.kubeCfg.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if pod.UID == "" {
			return false, fmt.Errorf("wait for agent pod %s deletion: observed UID is empty", podName)
		}
		if pod.UID != expectedUID {
			return true, nil
		}
		return false, nil
	})
}

func (e *KubernetesExecutor) waitForAgentPodReady(ctx context.Context, desired *corev1.Pod) error {
	podName := desired.Name
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pod, err := e.kube.CoreV1().Pods(e.kubeCfg.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, fmt.Errorf("%w: agent pod %s was deleted", errKubernetesAgentPodNeedsReconciliation, podName)
		}
		if err != nil {
			return false, err
		}
		if pod.DeletionTimestamp != nil {
			return false, fmt.Errorf("%w: agent pod %s is terminating", errKubernetesAgentPodNeedsReconciliation, podName)
		}
		if !kubernetesPodConfigMatches(pod, desired) {
			return false, fmt.Errorf("%w: agent pod %s config changed before readiness", errKubernetesAgentPodNeedsReconciliation, podName)
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return false, fmt.Errorf("%w: agent pod %s is terminal: %s", errKubernetesAgentPodNeedsReconciliation, podName, pod.Status.Phase)
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func (e *KubernetesExecutor) execAgentCommand(ctx context.Context, taskID, podName, workDir string, cmd []string, env []string, heartbeatFunc func(string)) execResult {
	timeout := time.Duration(e.kubeCfg.ExecTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := startKubernetesHeartbeat(execCtx, taskID, heartbeatFunc)
	defer func() {
		cancel()
		<-done
	}()

	script := "mkdir -p " + shellQuote(workDir) + " && cd " + shellQuote(workDir) + " && " + shellEnvCommand(env, cmd)
	return e.execShell(execCtx, podName, script)
}

func (e *KubernetesExecutor) copyWorkspaceBundle(ctx context.Context, podName, workDir, bundleDir string) error {
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		err := writeTarDirectory(pw, bundleDir)
		_ = pw.CloseWithError(err)
	}()
	script := kubernetesCopyWorkspaceScript(workDir)
	res := e.execShellWithStdin(ctx, podName, script, pr)
	if res.err != nil {
		return fmt.Errorf("copy workspace bundle to pod: %w", res.err)
	}
	return nil
}

func kubernetesCopyWorkspaceScript(workDir string) string {
	return "mkdir -p " + shellQuote(workDir) + " && tar -C " + shellQuote(workDir) + " -xf -"
}

func (e *KubernetesExecutor) captureProjectMemoryArchive(ctx context.Context, podName, memoryDir string) ([]byte, error) {
	memoryDir = strings.TrimSpace(memoryDir)
	if memoryDir == "" {
		return nil, nil
	}
	script := "if [ -d " + shellQuote(memoryDir) + " ]; then tar -C " + shellQuote(memoryDir) + " -czf - .; fi"
	res := e.execShell(ctx, podName, script)
	if res.err != nil {
		return nil, fmt.Errorf("archive remote project memory: %w", res.err)
	}
	return res.stdout.Bytes(), nil
}

func writeTarDirectory(w io.Writer, root string) error {
	tw := tar.NewWriter(w)
	if err := filepath.WalkDir(root, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if d.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if d.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	}); err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}

func startKubernetesHeartbeat(ctx context.Context, taskID string, heartbeatFunc func(string)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if heartbeatFunc == nil {
			return
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		heartbeatFunc(taskID)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeatFunc(taskID)
			}
		}
	}()
	return done
}

func (e *KubernetesExecutor) execShell(ctx context.Context, podName string, script string) execResult {
	return e.execShellWithStdin(ctx, podName, script, nil)
}

func (e *KubernetesExecutor) execShellWithStdin(ctx context.Context, podName string, script string, stdin io.Reader) execResult {
	var res execResult
	req := e.kube.CoreV1().RESTClient().
		Post().
		Namespace(e.kubeCfg.Namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: kubernetesAgentContainerName,
		Command:   []string{"/bin/sh", "-lc", script},
		Stdin:     stdin != nil,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.restConfig, http.MethodPost, req.URL())
	if err != nil {
		res.err = fmt.Errorf("create pod exec: %w", err)
		return res
	}
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &res.stdout,
		Stderr: &res.stderr,
	}); err != nil {
		res.err = fmt.Errorf("pod exec: %w", err)
	}
	if res.err != nil && strings.TrimSpace(res.stderr.String()) != "" {
		res.err = fmt.Errorf("%w: %s", res.err, strings.TrimSpace(res.stderr.String()))
	}
	return res
}

func shellEnvCommand(env []string, cmd []string) string {
	var parts []string
	for _, pair := range env {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		parts = append(parts, shellQuote(pair))
	}
	for _, arg := range cmd {
		parts = append(parts, shellQuote(arg))
	}
	return "env " + strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
