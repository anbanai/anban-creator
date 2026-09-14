package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func (d *kubernetesJobDispatcher) ResolveRuntime(taskType string) srvconfig.RuntimeImageSelection {
	if d == nil {
		return srvconfig.RuntimeImageSelection{}
	}
	return RuntimeImageForTask(d.config.RuntimeImages, taskType)
}

type kubernetesJobDispatcher struct {
	config kubernetesJobConfig
	kube   kubernetes.Interface
	memory ProjectMemoryStore
}

var _ RuntimeDispatcher = (*kubernetesJobDispatcher)(nil)

func (*kubernetesJobDispatcher) Scope() string { return "kubernetes" }

func NewKubernetesDispatcher(cfg srvconfig.KubernetesConfig, runtimeImages srvconfig.RuntimeImages, serverURL string, memory ProjectMemoryStore) (RuntimeDispatcher, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return NewKubernetesDispatcherWithClient(cfg, runtimeImages, serverURL, client, memory)
}

func NewKubernetesDispatcherWithClient(cfg srvconfig.KubernetesConfig, runtimeImages srvconfig.RuntimeImages, serverURL string, client kubernetes.Interface, memory ProjectMemoryStore) (RuntimeDispatcher, error) {
	if client == nil || memory == nil {
		return nil, fmt.Errorf("kubernetes client and project memory store are required")
	}
	jobCfg := kubernetesJobConfig{KubernetesConfig: cfg, RuntimeImages: runtimeImages, ServerURL: serverURL}
	return &kubernetesJobDispatcher{config: jobCfg, kube: client, memory: memory}, nil
}

func (d *kubernetesJobDispatcher) Prepare(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*model.RuntimeIdentity, error) {
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
	if err := d.validateRuntime(execution, task); err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	resume := execution.Attempt > 1 || strings.TrimSpace(execution.ParentExecutionID) != "" || strings.TrimSpace(execution.RuntimeInstanceID) != ""
	if err := prepareProjectMemory(ctx, d.memory, task.ProjectID, resume); err != nil {
		return nil, fmt.Errorf("prepare Kubernetes project memory: %w", err)
	}

	allowPVCCreation := execution.ParentExecutionID == ""
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
		if existingJob.Spec.Suspend != nil && !*existingJob.Spec.Suspend &&
			(execution.RuntimeScope != desiredJob.Namespace || execution.RuntimeWorkload != desiredJob.Name) {
			return nil, NewPermanentDispatchError(fmt.Errorf("Kubernetes Job %q was activated before its runtime identity was persisted", desiredJob.Name))
		}
	}
	return &model.RuntimeIdentity{Scope: desiredJob.Namespace, Workload: desiredJob.Name, InstanceID: string(existingJob.UID)}, nil
}

func (d *kubernetesJobDispatcher) ResolvePrepared(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*model.RuntimeIdentity, error) {
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
	if err := d.validateRuntime(execution, task); err != nil {
		return nil, NewPermanentDispatchError(err)
	}

	desiredJob := buildKubernetesJob(d.config, execution, task)
	existingJob, err := d.kube.BatchV1().Jobs(d.config.Namespace).Get(ctx, desiredJob.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("resolve prepared Kubernetes Job %q: %w", desiredJob.Name, ErrRuntimeWorkloadNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get prepared Kubernetes Job %q: %w", desiredJob.Name, classifyKubernetesDispatchAPIError(err, false))
	}
	if err := verifyJob(existingJob, desiredJob, execution, task); err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	if execution.RuntimeInstanceID != "" && string(existingJob.UID) != execution.RuntimeInstanceID {
		return nil, NewPermanentDispatchError(fmt.Errorf("Kubernetes Job %q UID mismatch: got %q, want persisted %q", existingJob.Name, existingJob.UID, execution.RuntimeInstanceID))
	}
	if existingJob.UID == "" {
		return nil, NewPermanentDispatchError(fmt.Errorf("Kubernetes Job %q has no UID", existingJob.Name))
	}
	return &model.RuntimeIdentity{Scope: existingJob.Namespace, Workload: existingJob.Name, InstanceID: string(existingJob.UID)}, nil
}

func (d *kubernetesJobDispatcher) Activate(ctx context.Context, execution *model.TaskExecution) error {
	if err := d.validatePersistedJobIdentity(execution); err != nil {
		return NewPermanentDispatchError(err)
	}
	name := kubernetesJobName(execution.ID)
	jobs := d.kube.BatchV1().Jobs(d.config.Namespace)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		job, err := jobs.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := verifyPersistedKubernetesJob(job, execution); err != nil {
			return NewPermanentDispatchError(err)
		}
		if job.Spec.Suspend != nil && !*job.Spec.Suspend {
			return nil
		}
		updated := job.DeepCopy()
		suspend := false
		updated.Spec.Suspend = &suspend
		_, err = jobs.Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})
	if err == nil {
		return nil
	}
	if IsPermanentDispatchError(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		return NewPermanentDispatchError(fmt.Errorf("persisted Kubernetes Job %q is missing: %w", name, err))
	}
	return fmt.Errorf("activate Kubernetes Job %q: %w", name, classifyKubernetesDispatchAPIError(err, false))
}

func verifyPersistedKubernetesJob(job *batchv1.Job, execution *model.TaskExecution) error {
	if job == nil || job.Namespace != execution.RuntimeScope || job.Name != execution.RuntimeWorkload {
		return fmt.Errorf("Kubernetes Job identity mismatch")
	}
	if string(job.UID) != execution.RuntimeInstanceID {
		return fmt.Errorf("Kubernetes Job %q UID mismatch: got %q, want persisted %q", job.Name, job.UID, execution.RuntimeInstanceID)
	}
	if err := verifyRequiredLabels(job.Labels, kubernetesExecutionLabels(execution, nil)); err != nil {
		return fmt.Errorf("Kubernetes Job %q identity mismatch: %w", job.Name, err)
	}
	if len(job.Spec.Template.Spec.Containers) != 1 || job.Spec.Template.Spec.Containers[0].Image != execution.RuntimeImage {
		return fmt.Errorf("Kubernetes Job %q runtime image mismatch", job.Name)
	}
	return nil
}

func (d *kubernetesJobDispatcher) validateRuntime(execution *model.TaskExecution, task *model.Task) error {
	profile := strings.TrimSpace(execution.RuntimeProfile)
	image := strings.TrimSpace(execution.RuntimeImage)
	if profile == "" || image == "" {
		return fmt.Errorf("execution runtime identity is required")
	}
	if execution.Attempt > 1 || strings.TrimSpace(execution.ParentExecutionID) != "" {
		return nil
	}
	configured := d.ResolveRuntime(task.Type)
	if profile != strings.TrimSpace(configured.Profile) || image != strings.TrimSpace(configured.Image) {
		return fmt.Errorf(
			"initial runtime identity mismatch: execution has %q %q, configured runtime is %q %q",
			profile, image, strings.TrimSpace(configured.Profile), strings.TrimSpace(configured.Image),
		)
	}
	return nil
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
	if err := d.validatePersistedJobIdentity(execution); err != nil {
		return NewPermanentDispatchError(err)
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
	if err := verifyPersistedKubernetesJob(job, execution); err != nil {
		return NewPermanentDispatchError(err)
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

func (d *kubernetesJobDispatcher) Inspect(ctx context.Context, execution *model.TaskExecution) (*RuntimeExecutionState, error) {
	if err := d.validatePersistedJobIdentity(execution); err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	name := kubernetesJobName(execution.ID)
	job, err := d.kube.BatchV1().Jobs(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get Kubernetes Job %q: %w", name, ErrRuntimeWorkloadNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get Kubernetes Job %q: %w", name, err)
	}
	if err := verifyPersistedKubernetesJob(job, execution); err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	state := inspectJob(job)
	state.InstanceID = string(job.UID)
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
	if execution.RuntimeWorkload != "" && execution.RuntimeWorkload != deterministicName {
		return fmt.Errorf("execution workload identity mismatch: name is %q, want %q", execution.RuntimeWorkload, deterministicName)
	}
	if execution.RuntimeScope != "" && execution.RuntimeScope != d.config.Namespace {
		return fmt.Errorf("execution runtime scope identity mismatch: scope is %q, want %q", execution.RuntimeScope, d.config.Namespace)
	}
	return nil
}

func (d *kubernetesJobDispatcher) validatePersistedJobIdentity(execution *model.TaskExecution) error {
	if err := d.validate(execution); err != nil {
		return err
	}
	if execution.RuntimeScope != d.config.Namespace ||
		execution.RuntimeWorkload != kubernetesJobName(execution.ID) ||
		strings.TrimSpace(execution.RuntimeInstanceID) == "" {
		return fmt.Errorf("persisted Kubernetes Job identity is incomplete or mismatched")
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
	// Suspension is protocol state: Prepare sets it and Activate clears it.
	// It is verified separately from immutable workload configuration.
	spec.Suspend = nil
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
	if container.TerminationMessagePolicy == "" || container.TerminationMessagePolicy == corev1.TerminationMessageReadFile {
		// Jobs created before stderr fallback was enabled remain compatible across
		// a Server rollout; newly created Jobs still carry the stronger policy.
		container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
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

func inspectJob(job *batchv1.Job) *RuntimeExecutionState {
	state := &RuntimeExecutionState{Phase: RuntimePhasePending}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobFailed:
			state.Phase = RuntimePhaseFailed
			state.Reason = condition.Reason
			state.Message = condition.Message
			completedAt := condition.LastTransitionTime.Time
			state.CompletedAt = &completedAt
			return state
		case batchv1.JobComplete:
			state.Phase = RuntimePhaseSucceeded
			state.Reason = condition.Reason
			state.Message = condition.Message
			completedAt := condition.LastTransitionTime.Time
			state.CompletedAt = &completedAt
			return state
		}
	}
	if job.Status.Active > 0 {
		state.Phase = RuntimePhaseRunning
	}
	return state
}

func applyPodDiagnostics(state *RuntimeExecutionState, pod *corev1.Pod) {
	jobTerminal := isTerminalKubernetesPhase(state.Phase)
	if !jobTerminal {
		switch pod.Status.Phase {
		case corev1.PodPending:
			state.Phase = RuntimePhasePending
		case corev1.PodRunning:
			state.Phase = RuntimePhaseRunning
		case corev1.PodSucceeded:
			state.Phase = RuntimePhaseSucceeded
		case corev1.PodFailed:
			state.Phase = RuntimePhaseFailed
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name != kubernetesWorkspaceInitContainerName {
			continue
		}
		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			terminated := status.State.Terminated
			state.Phase = RuntimePhaseFailed
			state.Container = kubernetesWorkspaceInitContainerName
			if terminated.Reason != "" {
				state.Reason = terminated.Reason
			}
			if terminated.Message != "" {
				state.Message = terminated.Message
			}
			exitCode := terminated.ExitCode
			state.ExitCode = &exitCode
			if !terminated.FinishedAt.IsZero() {
				completedAt := terminated.FinishedAt.Time
				state.CompletedAt = &completedAt
			}
			return
		}
		if status.State.Waiting != nil && !jobTerminal {
			state.Reason = status.State.Waiting.Reason
			state.Message = status.State.Waiting.Message
			return
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
		state.Container = kubernetesAgentContainerName
		if !terminated.FinishedAt.IsZero() {
			completedAt := terminated.FinishedAt.Time
			state.CompletedAt = &completedAt
		}
		exitCode := terminated.ExitCode
		state.ExitCode = &exitCode
		if state.Phase == RuntimePhasePending || state.Phase == RuntimePhaseRunning {
			if exitCode == 0 {
				state.Phase = RuntimePhaseSucceeded
			} else {
				state.Phase = RuntimePhaseFailed
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
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && !jobTerminal {
			state.Phase = RuntimePhasePending
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
	return phase == RuntimePhaseSucceeded || phase == RuntimePhaseFailed
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
