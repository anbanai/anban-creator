package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	kubernetesPhasePending   = "pending"
	kubernetesPhaseRunning   = "running"
	kubernetesPhaseSucceeded = "succeeded"
	kubernetesPhaseFailed    = "failed"
)

type KubernetesDispatcher interface {
	ResolveRuntime(taskType string) srvconfig.RuntimeImageSelection
	Dispatch(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*KubernetesRuntimeIdentity, error)
	Delete(ctx context.Context, execution *model.TaskExecution) error
	DeleteProjectMemory(ctx context.Context, projectID string) error
	Inspect(ctx context.Context, execution *model.TaskExecution) (*KubernetesExecutionState, error)
}

func (d *kubernetesJobDispatcher) ResolveRuntime(taskType string) srvconfig.RuntimeImageSelection {
	if d == nil {
		return srvconfig.RuntimeImageSelection{}
	}
	return d.config.ImageForTask(taskType)
}

type KubernetesRuntimeIdentity struct {
	Namespace string
	JobName   string
}

type KubernetesExecutionState struct {
	Phase       string
	PodUID      string
	Reason      string
	Message     string
	ExitCode    *int32
	CompletedAt *time.Time
}

type kubernetesJobDispatcher struct {
	config kubernetesJobConfig
	kube   kubernetes.Interface
}

var _ KubernetesDispatcher = (*kubernetesJobDispatcher)(nil)

func NewKubernetesDispatcher(cfg srvconfig.KubernetesConfig, serverURL string) (KubernetesDispatcher, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return NewKubernetesDispatcherWithClient(cfg, serverURL, client)
}

func NewKubernetesDispatcherWithClient(cfg srvconfig.KubernetesConfig, serverURL string, client kubernetes.Interface) (KubernetesDispatcher, error) {
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	jobCfg := kubernetesJobConfig{KubernetesConfig: cfg, ServerURL: serverURL}
	return &kubernetesJobDispatcher{config: jobCfg, kube: client}, nil
}

func (d *kubernetesJobDispatcher) Dispatch(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*KubernetesRuntimeIdentity, error) {
	if err := d.validate(execution); err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	if task == nil {
		return nil, NewPermanentDispatchError(fmt.Errorf("task is required"))
	}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.ProjectID) == "" {
		return nil, NewPermanentDispatchError(fmt.Errorf("task ID and project ID are required"))
	}
	if execution.TaskID != task.ID {
		return nil, NewPermanentDispatchError(fmt.Errorf("execution task identity mismatch: execution has %q, task has %q", execution.TaskID, task.ID))
	}

	allowPVCCreation := execution.ParentExecutionID == ""
	if err := d.ensurePVC(ctx, buildProjectMemoryPVC(d.config, task.ProjectID), "project memory", task.ProjectID, allowPVCCreation); err != nil {
		return nil, err
	}
	if err := d.ensurePVC(ctx, buildTaskWorkspacePVC(d.config, task), "task workspace", task.ID, allowPVCCreation); err != nil {
		return nil, err
	}

	desiredJob := buildKubernetesJob(d.config, execution, task)
	jobs := d.kube.BatchV1().Jobs(d.config.Namespace)
	existingJob, err := jobs.Get(ctx, desiredJob.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		existingJob, err = jobs.Create(ctx, desiredJob, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("create Kubernetes Job %q: %w", desiredJob.Name, classifyKubernetesDispatchAPIError(err, true))
		}
		if apierrors.IsAlreadyExists(err) {
			existingJob, err = jobs.Get(ctx, desiredJob.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("get Kubernetes Job %q after create conflict: %w", desiredJob.Name, classifyKubernetesDispatchAPIError(err, false))
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("get Kubernetes Job %q: %w", desiredJob.Name, classifyKubernetesDispatchAPIError(err, false))
	}
	if existingJob != nil {
		if err := verifyJob(existingJob, desiredJob, execution, task); err != nil {
			return nil, NewPermanentDispatchError(err)
		}
	}
	return &KubernetesRuntimeIdentity{Namespace: desiredJob.Namespace, JobName: desiredJob.Name}, nil
}

func (d *kubernetesJobDispatcher) ensurePVC(ctx context.Context, desired *corev1.PersistentVolumeClaim, kind, identity string, allowCreation bool) error {
	pvcs := d.kube.CoreV1().PersistentVolumeClaims(d.config.Namespace)
	existing, err := pvcs.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if !allowCreation {
			return NewPermanentDispatchError(fmt.Errorf("%s PVC %q is missing; the original execution state cannot be resumed", kind, desired.Name))
		}
		existing, err = pvcs.Create(ctx, desired, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create %s PVC %q: %w", kind, desired.Name, classifyKubernetesDispatchAPIError(err, true))
		}
		if apierrors.IsAlreadyExists(err) {
			existing, err = pvcs.Get(ctx, desired.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get %s PVC %q after create conflict: %w", kind, desired.Name, classifyKubernetesDispatchAPIError(err, false))
			}
		}
	} else if err != nil {
		return fmt.Errorf("get %s PVC %q: %w", kind, desired.Name, classifyKubernetesDispatchAPIError(err, false))
	}
	if existing != nil {
		if err := verifyPVC(existing, desired, kind, identity); err != nil {
			return NewPermanentDispatchError(err)
		}
	}
	return nil
}

func classifyKubernetesDispatchAPIError(err error, create bool) error {
	if err == nil {
		return nil
	}
	if apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) ||
		(create && apierrors.IsNotFound(err)) {
		return NewPermanentDispatchError(err)
	}
	return err
}

func (d *kubernetesJobDispatcher) Delete(ctx context.Context, execution *model.TaskExecution) error {
	if err := d.validate(execution); err != nil {
		return err
	}
	name := kubernetesJobName(execution.ID)
	jobs := d.kube.BatchV1().Jobs(d.config.Namespace)
	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Kubernetes Job %q before delete: %w", name, err)
	}
	desiredLabels := kubernetesExecutionLabels(execution, nil)
	if err := verifyRequiredLabels(job.Labels, desiredLabels); err != nil {
		return fmt.Errorf("Kubernetes Job %q identity mismatch: %w", name, err)
	}
	foreground := metav1.DeletePropagationForeground
	err = jobs.Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
		Preconditions:     &metav1.Preconditions{UID: &job.UID},
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete Kubernetes Job %q: %w", name, err)
	}
	return nil
}

func (d *kubernetesJobDispatcher) DeleteProjectMemory(ctx context.Context, projectID string) error {
	if d == nil || d.kube == nil {
		return fmt.Errorf("kubernetes dispatcher is not configured")
	}
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("project ID is required")
	}
	name := kubernetesProjectMemoryPVCName(projectID)
	pvcs := d.kube.CoreV1().PersistentVolumeClaims(d.config.Namespace)
	pvc, err := pvcs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get project memory PVC %q: %w", name, err)
	}
	desired := buildProjectMemoryPVC(d.config, projectID)
	if err := verifyRequiredLabels(pvc.Labels, desired.Labels); err != nil {
		return fmt.Errorf("project memory PVC %q identity mismatch: %w", name, err)
	}
	if err := pvcs.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pvc.UID}}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete project memory PVC %q: %w", name, err)
	}
	return nil
}

func (d *kubernetesJobDispatcher) DeleteTaskWorkspace(ctx context.Context, task *model.Task) error {
	if d == nil || d.kube == nil {
		return fmt.Errorf("kubernetes dispatcher is not configured")
	}
	if task == nil || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.ProjectID) == "" || strings.TrimSpace(task.UserID) == "" {
		return fmt.Errorf("task identity is required")
	}
	name := kubernetesTaskWorkspacePVCName(task.ID)
	pvcs := d.kube.CoreV1().PersistentVolumeClaims(d.config.Namespace)
	pvc, err := pvcs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get task workspace PVC %q: %w", name, err)
	}
	desired := buildTaskWorkspacePVC(d.config, task)
	if err := verifyRequiredLabels(pvc.Labels, desired.Labels); err != nil {
		return fmt.Errorf("task workspace PVC %q identity mismatch: %w", name, err)
	}
	if err := pvcs.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pvc.UID}}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete task workspace PVC %q: %w", name, err)
	}
	return nil
}

func (d *kubernetesJobDispatcher) Inspect(ctx context.Context, execution *model.TaskExecution) (*KubernetesExecutionState, error) {
	if err := d.validate(execution); err != nil {
		return nil, err
	}
	name := kubernetesJobName(execution.ID)
	job, err := d.kube.BatchV1().Jobs(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get Kubernetes Job %q: %w", name, err)
	}
	state := inspectJob(job)
	pods, err := d.kube.CoreV1().Pods(d.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: kubernetesExecutionIDLabel + "=" + kubernetesLabelValue(execution.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("list pods for Kubernetes Job %q: %w", name, err)
	}
	ownedPods := make([]corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		if podControlledByJob(&pods.Items[i], job) {
			ownedPods = append(ownedPods, pods.Items[i])
		}
	}
	if pod := newestPod(ownedPods); pod != nil {
		state.PodUID = string(pod.UID)
		applyPodDiagnostics(state, pod)
	}
	return state, nil
}

func (d *kubernetesJobDispatcher) validate(execution *model.TaskExecution) error {
	if d == nil || d.kube == nil {
		return fmt.Errorf("kubernetes dispatcher is not configured")
	}
	if execution == nil || strings.TrimSpace(execution.ID) == "" {
		return fmt.Errorf("execution is required")
	}
	deterministicName := kubernetesJobName(execution.ID)
	if execution.JobName != "" && execution.JobName != deterministicName {
		return fmt.Errorf("execution Job identity mismatch: name is %q, want %q", execution.JobName, deterministicName)
	}
	if execution.Namespace != "" && execution.Namespace != d.config.Namespace {
		return fmt.Errorf("execution namespace identity mismatch: namespace is %q, want %q", execution.Namespace, d.config.Namespace)
	}
	return nil
}

func verifyPVC(existing, desired *corev1.PersistentVolumeClaim, kind, identity string) error {
	if err := verifyRequiredLabels(existing.Labels, desired.Labels); err != nil {
		return fmt.Errorf("%s PVC %q identity mismatch for %q: %w", kind, existing.Name, identity, err)
	}
	if existing.Spec.StorageClassName == nil || desired.Spec.StorageClassName == nil || *existing.Spec.StorageClassName != *desired.Spec.StorageClassName ||
		!containsAccessMode(existing.Spec.AccessModes, corev1.ReadWriteMany) {
		return fmt.Errorf("%s PVC %q configuration mismatch", kind, existing.Name)
	}
	if existing.Spec.Resources.Requests.Storage().Cmp(*desired.Spec.Resources.Requests.Storage()) < 0 {
		return fmt.Errorf("%s PVC %q is smaller than required; expand it to at least %s", kind, existing.Name, desired.Spec.Resources.Requests.Storage().String())
	}
	return nil
}

func verifyJob(existing, desired *batchv1.Job, execution *model.TaskExecution, task *model.Task) error {
	if err := verifyRequiredLabels(existing.Labels, desired.Labels); err != nil {
		return fmt.Errorf("Kubernetes Job %q identity mismatch: %w", existing.Name, err)
	}
	if err := verifyRequiredLabels(existing.Spec.Template.Labels, desired.Spec.Template.Labels); err != nil {
		return fmt.Errorf("Kubernetes Job %q configuration mismatch: template %w", existing.Name, err)
	}
	existingSpec := normalizedJobSpec(existing)
	desiredSpec := normalizedJobSpec(desired)
	if !apiequality.Semantic.DeepEqual(existingSpec, desiredSpec) {
		fields := jobSpecMismatchPaths(existingSpec, desiredSpec, 8)
		detail := ""
		if len(fields) > 0 {
			detail = ": differing fields " + strings.Join(fields, ", ")
		}
		return fmt.Errorf("Kubernetes Job %q configuration mismatch for execution %q task %q%s", existing.Name, execution.ID, task.ID, detail)
	}
	return nil
}

func normalizedJobSpec(job *batchv1.Job) batchv1.JobSpec {
	spec := job.Spec.DeepCopy()
	one := int32(1)
	if spec.Completions == nil && spec.Parallelism == nil {
		spec.Completions = &one
	}
	if spec.Parallelism == nil {
		spec.Parallelism = &one
	}
	if spec.BackoffLimit == nil {
		backoff := int32(6)
		if spec.BackoffLimitPerIndex != nil {
			backoff = int32(^uint32(0) >> 1)
		}
		spec.BackoffLimit = &backoff
	}
	if spec.CompletionMode == nil {
		mode := batchv1.NonIndexedCompletion
		spec.CompletionMode = &mode
	}
	if spec.Suspend == nil {
		spec.Suspend = pointerTo(false)
	}
	if spec.PodReplacementPolicy == nil {
		policy := batchv1.TerminatingOrFailed
		if spec.PodFailurePolicy != nil {
			policy = batchv1.Failed
		}
		spec.PodReplacementPolicy = &policy
	}
	if spec.ManualSelector == nil {
		spec.ManualSelector = pointerTo(false)
	}
	normalizeGeneratedJobSelector(job, spec)
	normalizeJobPodDefaults(&spec.Template.Spec)
	return *spec
}

func normalizeGeneratedJobSelector(job *batchv1.Job, spec *batchv1.JobSpec) {
	if spec.ManualSelector == nil || *spec.ManualSelector || job.UID == "" {
		return
	}
	wantSelector := &metav1.LabelSelector{MatchLabels: map[string]string{
		batchv1.ControllerUidLabel: string(job.UID),
	}}
	if apiequality.Semantic.DeepEqual(spec.Selector, wantSelector) {
		spec.Selector = nil
	}
	for key, want := range map[string]string{
		"controller-uid":           string(job.UID),
		batchv1.ControllerUidLabel: string(job.UID),
		"job-name":                 job.Name,
		batchv1.JobNameLabel:       job.Name,
	} {
		if spec.Template.Labels[key] == want {
			delete(spec.Template.Labels, key)
		}
	}
}

func normalizeJobPodDefaults(spec *corev1.PodSpec) {
	if spec.DeprecatedServiceAccount == spec.ServiceAccountName {
		spec.DeprecatedServiceAccount = ""
	}
	if spec.DNSPolicy == "" {
		spec.DNSPolicy = corev1.DNSClusterFirst
	}
	if spec.RestartPolicy == "" {
		spec.RestartPolicy = corev1.RestartPolicyAlways
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	if spec.TerminationGracePeriodSeconds == nil {
		spec.TerminationGracePeriodSeconds = int64Ptr(corev1.DefaultTerminationGracePeriodSeconds)
	}
	if spec.SchedulerName == "" {
		spec.SchedulerName = corev1.DefaultSchedulerName
	}
	if spec.EnableServiceLinks == nil {
		spec.EnableServiceLinks = pointerTo(corev1.DefaultEnableServiceLinks)
	}
	if spec.ShareProcessNamespace == nil {
		spec.ShareProcessNamespace = pointerTo(false)
	}
	if spec.HostUsers == nil {
		spec.HostUsers = pointerTo(true)
	}
	for i := range spec.InitContainers {
		normalizeContainerDefaults(&spec.InitContainers[i])
	}
	for i := range spec.Containers {
		normalizeContainerDefaults(&spec.Containers[i])
	}
	for i := range spec.Volumes {
		normalizeVolumeDefaults(&spec.Volumes[i])
	}
}

func normalizeContainerDefaults(container *corev1.Container) {
	if container.TerminationMessagePath == "" {
		container.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if container.TerminationMessagePolicy == "" {
		container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
}

func normalizeVolumeDefaults(volume *corev1.Volume) {
	if volume.Secret != nil && volume.Secret.DefaultMode == nil {
		volume.Secret.DefaultMode = int32Ptr(corev1.SecretVolumeSourceDefaultMode)
	}
	if volume.Projected != nil && volume.Projected.DefaultMode == nil {
		volume.Projected.DefaultMode = int32Ptr(corev1.ProjectedVolumeSourceDefaultMode)
	}
}

func jobSpecMismatchPaths(existing, desired batchv1.JobSpec, limit int) []string {
	existingJSON, existingErr := json.Marshal(existing)
	desiredJSON, desiredErr := json.Marshal(desired)
	if existingErr != nil || desiredErr != nil {
		return nil
	}
	var existingValue any
	var desiredValue any
	if json.Unmarshal(existingJSON, &existingValue) != nil || json.Unmarshal(desiredJSON, &desiredValue) != nil {
		return nil
	}
	paths := make([]string, 0, limit)
	collectJSONMismatchPaths(existingValue, desiredValue, "spec", limit, &paths)
	return paths
}

func collectJSONMismatchPaths(existing, desired any, path string, limit int, paths *[]string) {
	if len(*paths) >= limit || reflect.DeepEqual(existing, desired) {
		return
	}
	switch existingValue := existing.(type) {
	case map[string]any:
		desiredValue, ok := desired.(map[string]any)
		if !ok {
			*paths = append(*paths, path)
			return
		}
		keys := make(map[string]struct{}, len(existingValue)+len(desiredValue))
		for key := range existingValue {
			keys[key] = struct{}{}
		}
		for key := range desiredValue {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			collectJSONMismatchPaths(existingValue[key], desiredValue[key], path+"."+key, limit, paths)
		}
	case []any:
		desiredValue, ok := desired.([]any)
		if !ok || len(existingValue) != len(desiredValue) {
			*paths = append(*paths, path)
			return
		}
		for i := range existingValue {
			collectJSONMismatchPaths(existingValue[i], desiredValue[i], fmt.Sprintf("%s[%d]", path, i), limit, paths)
		}
	default:
		*paths = append(*paths, path)
	}
}

func pointerTo[T any](value T) *T { return &value }

func verifyRequiredLabels(existing, desired map[string]string) error {
	for key, want := range desired {
		if got := existing[key]; got != want {
			return fmt.Errorf("label %q is %q, want %q", key, got, want)
		}
	}
	return nil
}

func inspectJob(job *batchv1.Job) *KubernetesExecutionState {
	state := &KubernetesExecutionState{Phase: kubernetesPhasePending}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobFailed:
			state.Phase = kubernetesPhaseFailed
			state.Reason = condition.Reason
			state.Message = condition.Message
			completedAt := condition.LastTransitionTime.Time
			state.CompletedAt = &completedAt
			return state
		case batchv1.JobComplete:
			state.Phase = kubernetesPhaseSucceeded
			state.Reason = condition.Reason
			state.Message = condition.Message
			completedAt := condition.LastTransitionTime.Time
			state.CompletedAt = &completedAt
			return state
		}
	}
	if job.Status.Active > 0 {
		state.Phase = kubernetesPhaseRunning
	}
	return state
}

func applyPodDiagnostics(state *KubernetesExecutionState, pod *corev1.Pod) {
	jobTerminal := isTerminalKubernetesPhase(state.Phase)
	if !jobTerminal {
		switch pod.Status.Phase {
		case corev1.PodPending:
			state.Phase = kubernetesPhasePending
		case corev1.PodRunning:
			state.Phase = kubernetesPhaseRunning
		case corev1.PodSucceeded:
			state.Phase = kubernetesPhaseSucceeded
		case corev1.PodFailed:
			state.Phase = kubernetesPhaseFailed
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != kubernetesAgentContainerName {
			continue
		}
		if status.State.Waiting != nil && !jobTerminal {
			state.Reason = status.State.Waiting.Reason
			state.Message = status.State.Waiting.Message
			return
		}
		if status.State.Terminated == nil {
			continue
		}
		terminated := status.State.Terminated
		if !terminated.FinishedAt.IsZero() {
			completedAt := terminated.FinishedAt.Time
			state.CompletedAt = &completedAt
		}
		exitCode := terminated.ExitCode
		state.ExitCode = &exitCode
		if state.Phase == kubernetesPhasePending || state.Phase == kubernetesPhaseRunning {
			if exitCode == 0 {
				state.Phase = kubernetesPhaseSucceeded
			} else {
				state.Phase = kubernetesPhaseFailed
			}
		}
		if terminated.Reason != "" {
			state.Reason = terminated.Reason
		}
		if terminated.Message != "" {
			state.Message = terminated.Message
		}
		return
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Waiting != nil && !jobTerminal {
			state.Reason = status.State.Waiting.Reason
			state.Message = status.State.Waiting.Message
			return
		}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && !jobTerminal {
			state.Phase = kubernetesPhasePending
			state.Reason = condition.Reason
			state.Message = condition.Message
			return
		}
	}
	if pod.Status.Reason != "" && !jobTerminal {
		state.Reason = pod.Status.Reason
	}
	if pod.Status.Message != "" && !jobTerminal {
		state.Message = pod.Status.Message
	}
}

func isTerminalKubernetesPhase(phase string) bool {
	return phase == kubernetesPhaseSucceeded || phase == kubernetesPhaseFailed
}

func newestPod(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}
	newest := &pods[0]
	for i := 1; i < len(pods); i++ {
		if pods[i].CreationTimestamp.After(newest.CreationTimestamp.Time) ||
			(pods[i].CreationTimestamp.Equal(&newest.CreationTimestamp) &&
				(pods[i].Name > newest.Name || (pods[i].Name == newest.Name && string(pods[i].UID) > string(newest.UID)))) {
			newest = &pods[i]
		}
	}
	return newest
}

func podControlledByJob(pod *corev1.Pod, job *batchv1.Job) bool {
	if pod == nil || job == nil || job.UID == "" || pod.Labels[batchv1.JobNameLabel] != job.Name {
		return false
	}
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.APIVersion == "batch/v1" && owner.Kind == "Job" && owner.Name == job.Name && owner.UID == job.UID {
			return true
		}
	}
	return false
}

func containsAccessMode(modes []corev1.PersistentVolumeAccessMode, want corev1.PersistentVolumeAccessMode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}
