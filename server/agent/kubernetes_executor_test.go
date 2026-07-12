package agent

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestBuildKubernetesJobIsOneShotAndHardened(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	spec := job.Spec.Template.Spec
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoff limit = %#v, want 0", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 900 {
		t.Fatalf("active deadline = %#v, want 900", job.Spec.ActiveDeadlineSeconds)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 120 {
		t.Fatalf("TTL = %#v, want 120", job.Spec.TTLSecondsAfterFinished)
	}
	if spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("restart policy = %q, want Never", spec.RestartPolicy)
	}
	if len(spec.Containers) != 1 || len(spec.InitContainers) != 0 {
		t.Fatalf("containers = %d, init containers = %d, want one main container and no init containers", len(spec.Containers), len(spec.InitContainers))
	}

	c := spec.Containers[0]
	if c.Image != "registry.example.com/anban-agent:v2" {
		t.Fatalf("image = %q, want configured image", c.Image)
	}
	if c.SecurityContext == nil || c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("security context = %#v, want read-only root filesystem", c.SecurityContext)
	}
	if c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("security context = %#v, want non-root and no privilege escalation", c.SecurityContext)
	}
	if c.SecurityContext.Capabilities == nil || !slices.Contains(c.SecurityContext.Capabilities.Drop, corev1.Capability("ALL")) {
		t.Fatalf("capabilities = %#v, want ALL dropped", c.SecurityContext.Capabilities)
	}
	assertMount(t, c, kubernetesWorkspaceMountName, "/workspace", false)
	assertMount(t, c, kubernetesMemoryMountName, "/workspace/.claude/memory", false)
	assertMount(t, c, kubernetesHomeVolumeName, "/home/node", false)
	assertMount(t, c, kubernetesTokenVolumeName, kubernetesTokenMountPath, true)
	assertProjectedAudience(t, spec.Volumes, kubernetesTokenAudience)
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatalf("automount token = %#v, want false", spec.AutomountServiceAccountToken)
	}
	if spec.SecurityContext == nil || spec.SecurityContext.SeccompProfile == nil || spec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod security context = %#v, want RuntimeDefault seccomp", spec.SecurityContext)
	}
	if len(spec.ImagePullSecrets) != 1 || spec.ImagePullSecrets[0].Name != "acr-secret" {
		t.Fatalf("image pull secrets = %#v, want acr-secret", spec.ImagePullSecrets)
	}
	if got := strings.Join(append(c.Command, c.Args...), " "); !strings.Contains(got, "anban job") || !strings.Contains(got, "--server-url http://creator-server:8080") || !strings.Contains(got, "--execution-id execution-1") || !strings.Contains(got, "--workload-token-file "+kubernetesTokenFile) {
		t.Fatalf("command = %q, want one-shot job bootstrap args", got)
	}
	if got := strings.Join(append(c.Command, c.Args...), " "); strings.Contains(got, testTask().Prompt) {
		t.Fatalf("command embeds task prompt: %q", got)
	}
	if len(c.Env) != 0 {
		t.Fatalf("environment = %#v, want no task secrets", c.Env)
	}
	if c.Resources.Requests.Cpu().String() != "500m" || c.Resources.Limits.Memory().String() != "2Gi" {
		t.Fatalf("resources = %#v, want configured requests and limits", c.Resources)
	}
}

func TestBuildProjectMemoryPVCUsesNASStorageClass(t *testing.T) {
	pvc := buildProjectMemoryPVC(testJobConfig(), "project-1")
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "alicloud-nas" {
		t.Fatalf("storage class = %#v, want alicloud-nas", pvc.Spec.StorageClassName)
	}
	if !slices.Contains(pvc.Spec.AccessModes, corev1.ReadWriteMany) {
		t.Fatalf("access modes = %#v, want RWX", pvc.Spec.AccessModes)
	}
	if pvc.Spec.Resources.Requests.Storage().String() != "1Gi" {
		t.Fatalf("storage request = %s, want 1Gi", pvc.Spec.Resources.Requests.Storage().String())
	}
}

func TestKubernetesJobAndPVCNamesAreDeterministicDNSSafeAndCollisionAware(t *testing.T) {
	for _, tc := range []struct {
		nameA string
		nameB string
		build func(string) string
	}{
		{nameA: "Execution_With.Mixed/Unsafe_Chars", nameB: "Execution-With.Mixed/Unsafe-Chars", build: kubernetesJobName},
		{nameA: "Project_With.Mixed/Unsafe_Chars", nameB: "Project-With.Mixed/Unsafe-Chars", build: kubernetesProjectMemoryPVCName},
	} {
		first := tc.build(tc.nameA)
		if first != tc.build(tc.nameA) {
			t.Fatalf("name for %q is not deterministic", tc.nameA)
		}
		if first == tc.build(tc.nameB) {
			t.Fatalf("names collide for distinct identities %q and %q: %q", tc.nameA, tc.nameB, first)
		}
		if len(first) > 63 || !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(first) {
			t.Fatalf("name %q is not DNS-1123 safe", first)
		}
	}
}

func TestKubernetesJobLabelsPreserveExecutionIdentity(t *testing.T) {
	execution := testExecution()
	job := buildKubernetesJob(testJobConfig(), execution, testTask())
	if job.Labels[kubernetesExecutionIDLabel] != execution.ID {
		t.Fatalf("execution label = %q, want %q", job.Labels[kubernetesExecutionIDLabel], execution.ID)
	}
	if job.Labels[kubernetesTaskIDLabel] != execution.TaskID || job.Labels[kubernetesProjectIDLabel] != "project-1" {
		t.Fatalf("identity labels = %#v", job.Labels)
	}
}

func TestKubernetesLabelValuePreservesValidIdentity(t *testing.T) {
	const identity = "Execution_ID.With-Case"
	if got := kubernetesLabelValue(identity); got != identity {
		t.Fatalf("label value = %q, want valid identity preserved as %q", got, identity)
	}
}

func TestKubernetesDispatcherDispatchCreatesAndReusesObjects(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	d := testDispatcher(client)
	if err := d.Dispatch(ctx, testExecution(), testTask()); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if err := d.Dispatch(ctx, testExecution(), testTask()); err != nil {
		t.Fatalf("idempotent Dispatch: %v", err)
	}
	jobs, err := client.BatchV1().Jobs("anban").List(ctx, metav1.ListOptions{})
	if err != nil || len(jobs.Items) != 1 {
		t.Fatalf("jobs = %#v, err = %v, want one", jobs.Items, err)
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims("anban").List(ctx, metav1.ListOptions{})
	if err != nil || len(pvcs.Items) != 1 {
		t.Fatalf("PVCs = %#v, err = %v, want one", pvcs.Items, err)
	}
}

func TestKubernetesDispatcherRejectsMissingOrMismatchedRequiredLabels(t *testing.T) {
	ctx := context.Background()
	desiredJob := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	desiredPVC := buildProjectMemoryPVC(testJobConfig(), "project-1")
	for _, object := range []struct {
		name   string
		labels map[string]string
		client func(*batchv1.Job, *corev1.PersistentVolumeClaim) kubeclient.Interface
	}{
		{name: "job", labels: desiredJob.Labels, client: func(job *batchv1.Job, pvc *corev1.PersistentVolumeClaim) kubeclient.Interface {
			return fake.NewSimpleClientset(job, pvc)
		}},
		{name: "PVC", labels: desiredPVC.Labels, client: func(job *batchv1.Job, pvc *corev1.PersistentVolumeClaim) kubeclient.Interface {
			return fake.NewSimpleClientset(pvc, job)
		}},
	} {
		for label := range object.labels {
			for _, mutation := range []string{"missing", "conflicting"} {
				t.Run(object.name+"/"+label+"/"+mutation, func(t *testing.T) {
					job := desiredJob.DeepCopy()
					pvc := desiredPVC.DeepCopy()
					labels := job.Labels
					if object.name == "PVC" {
						labels = pvc.Labels
					}
					if mutation == "missing" {
						delete(labels, label)
					} else {
						labels[label] = "conflicting-value"
					}
					err := testDispatcher(object.client(job, pvc)).Dispatch(ctx, testExecution(), testTask())
					if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
						t.Fatalf("Dispatch error = %v, want identity mismatch", err)
					}
				})
			}
		}
	}
}

func TestKubernetesDispatcherDeleteUsesForegroundPropagation(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(buildKubernetesJob(testJobConfig(), testExecution(), testTask()))
	d := testDispatcher(client)
	if err := d.Delete(ctx, testExecution()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	actions := client.Actions()
	deleteAction, ok := actions[len(actions)-1].(ktesting.DeleteAction)
	if !ok {
		t.Fatalf("last action = %T, want DeleteAction", actions[len(actions)-1])
	}
	if got := deleteAction.GetDeleteOptions().PropagationPolicy; got == nil || *got != metav1.DeletePropagationForeground {
		t.Fatalf("propagation = %#v, want foreground", got)
	}
	if err := d.Delete(ctx, testExecution()); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestKubernetesDispatcherDeleteProjectMemoryIsGuardedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	pvc := buildProjectMemoryPVC(testJobConfig(), "project-1")
	client := fake.NewSimpleClientset(pvc)
	d := testDispatcher(client)
	if err := d.DeleteProjectMemory(ctx, "project-1"); err != nil {
		t.Fatalf("DeleteProjectMemory: %v", err)
	}
	if err := d.DeleteProjectMemory(ctx, "project-1"); err != nil {
		t.Fatalf("idempotent DeleteProjectMemory: %v", err)
	}

	mismatch := buildProjectMemoryPVC(testJobConfig(), "project-1")
	mismatch.Labels[kubernetesProjectIDLabel] = "project-2"
	client = fake.NewSimpleClientset(mismatch)
	err := testDispatcher(client).DeleteProjectMemory(ctx, "project-1")
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("mismatched DeleteProjectMemory error = %v, want identity mismatch", err)
	}
	if _, getErr := client.CoreV1().PersistentVolumeClaims("anban").Get(ctx, mismatch.Name, metav1.GetOptions{}); getErr != nil {
		t.Fatalf("mismatched PVC was deleted: %v", getErr)
	}

	missingOwnership := buildProjectMemoryPVC(testJobConfig(), "project-1")
	delete(missingOwnership.Labels, "app.kubernetes.io/component")
	client = fake.NewSimpleClientset(missingOwnership)
	err = testDispatcher(client).DeleteProjectMemory(ctx, "project-1")
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("unowned DeleteProjectMemory error = %v, want identity mismatch", err)
	}
	if _, getErr := client.CoreV1().PersistentVolumeClaims("anban").Get(ctx, missingOwnership.Name, metav1.GetOptions{}); getErr != nil {
		t.Fatalf("unowned PVC was deleted: %v", getErr)
	}
}

func TestKubernetesDispatcherInspectMapsJobAndPodTermination(t *testing.T) {
	execution := testExecution()
	job := buildKubernetesJob(testJobConfig(), execution, testTask())
	job.Status.Conditions = []batchv1.JobCondition{{
		Type:    batchv1.JobFailed,
		Status:  corev1.ConditionTrue,
		Reason:  "BackoffLimitExceeded",
		Message: "container failed",
	}}
	exitCode := int32(137)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-pod",
			Namespace: "anban",
			UID:       types.UID("pod-uid-1"),
			Labels:    map[string]string{kubernetesExecutionIDLabel: execution.ID},
		},
		Status: corev1.PodStatus{
			Reason:  "Evicted",
			Message: "node pressure",
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: kubernetesAgentContainerName,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: exitCode,
					Reason:   "OOMKilled",
					Message:  "memory limit exceeded",
				}},
			}},
		},
	}
	d := testDispatcher(fake.NewSimpleClientset(job, pod))
	state, err := d.Inspect(context.Background(), execution)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhaseFailed || state.PodUID != "pod-uid-1" || state.ExitCode == nil || *state.ExitCode != exitCode {
		t.Fatalf("state = %#v, want failed pod with exit 137", state)
	}
	if state.Reason != "OOMKilled" || state.Message != "memory limit exceeded" {
		t.Fatalf("diagnostics = %q/%q, want termination diagnostics", state.Reason, state.Message)
	}
}

func TestKubernetesDispatcherInspectMapsPendingAndCompleteJobs(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		condition *batchv1.JobCondition
		active    int32
		want      string
	}{
		{name: "pending", want: kubernetesPhasePending},
		{name: "running", active: 1, want: kubernetesPhaseRunning},
		{name: "complete", condition: &batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Reason: "Completed"}, want: kubernetesPhaseSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
			job.Status.Active = tc.active
			if tc.condition != nil {
				job.Status.Conditions = []batchv1.JobCondition{*tc.condition}
			}
			state, err := testDispatcher(fake.NewSimpleClientset(job)).Inspect(ctx, testExecution())
			if err != nil || state.Phase != tc.want {
				t.Fatalf("Inspect state = %#v, err = %v, want %q", state, err, tc.want)
			}
		})
	}
}

func TestKubernetesDispatcherInspectMapsPodTerminationBeforeJobCondition(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-pod",
			Namespace: "anban",
			Labels:    map[string]string{kubernetesExecutionIDLabel: testExecution().ID},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: kubernetesAgentContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 2,
				Reason:   "Error",
			}},
		}}},
	}
	state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhaseFailed {
		t.Fatalf("phase = %q, want failed from terminated container before Job condition", state.Phase)
	}
}

func TestKubernetesDispatcherInspectPreservesSchedulingFailureDiagnostics(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	job.Status.Active = 1
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unscheduled-job-pod",
			Namespace: "anban",
			Labels:    map[string]string{kubernetesExecutionIDLabel: testExecution().ID},
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: insufficient memory",
		}}},
	}
	state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhasePending {
		t.Fatalf("phase = %q, want pending for pre-start scheduling failure", state.Phase)
	}
	if state.Reason != "Unschedulable" || state.Message != "0/3 nodes are available: insufficient memory" {
		t.Fatalf("diagnostics = %q/%q, want scheduler reason and message", state.Reason, state.Message)
	}
}

func TestKubernetesDispatcherInspectKeepsRunningPodRunning(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	job.Status.Active = 1
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-job-pod",
			Namespace: "anban",
			Labels:    map[string]string{kubernetesExecutionIDLabel: testExecution().ID},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhaseRunning {
		t.Fatalf("phase = %q, want running for a running Pod", state.Phase)
	}
}

func TestKubernetesDispatcherInspectPropagatesNotFound(t *testing.T) {
	_, err := testDispatcher(fake.NewSimpleClientset()).Inspect(context.Background(), testExecution())
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Inspect error = %v, want NotFound", err)
	}
}

func TestKubernetesDispatcherStopsWhenPVCProvisioningFails(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "memory", nil)
	})
	err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask())
	if err == nil || !strings.Contains(err.Error(), "create project memory PVC") {
		t.Fatalf("Dispatch error = %v, want PVC creation error", err)
	}
	jobs, listErr := client.BatchV1().Jobs("anban").List(context.Background(), metav1.ListOptions{})
	if listErr != nil || len(jobs.Items) != 0 {
		t.Fatalf("jobs = %#v, err = %v, want none after PVC failure", jobs.Items, listErr)
	}
}

func testJobConfig() kubernetesJobConfig {
	return kubernetesJobConfig{
		KubernetesConfig: srvconfig.KubernetesConfig{
			Namespace:               "anban",
			AgentImage:              "registry.example.com/anban-agent:v2",
			ServiceAccount:          "creator-agent-runner",
			ImagePullSecret:         "acr-secret",
			MemoryStorageClass:      "alicloud-nas",
			MemorySize:              "1Gi",
			ActiveDeadlineSeconds:   900,
			TTLSecondsAfterFinished: 120,
			Resources: srvconfig.KubernetesResourceConfig{
				Requests: map[string]string{"cpu": "500m", "memory": "1Gi"},
				Limits:   map[string]string{"cpu": "2", "memory": "2Gi"},
			},
		},
		ServerURL: "http://creator-server:8080",
	}
}

func testExecution() *model.TaskExecution {
	return &model.TaskExecution{ID: "execution-1", TaskID: "task-1", Namespace: "anban", JobName: kubernetesJobName("execution-1")}
}

func testTask() *model.Task {
	return &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Prompt: "never put this prompt in a Job"}
}

func testDispatcher(client kubeclient.Interface) *kubernetesJobDispatcher {
	return &kubernetesJobDispatcher{config: testJobConfig(), kube: client}
}

func assertMount(t *testing.T, container corev1.Container, name, mountPath string, readOnly bool) {
	t.Helper()
	for _, mount := range container.VolumeMounts {
		if mount.Name == name && mount.MountPath == mountPath && mount.ReadOnly == readOnly {
			return
		}
	}
	t.Fatalf("mount %q at %q readOnly=%t not found in %#v", name, mountPath, readOnly, container.VolumeMounts)
}

func assertProjectedAudience(t *testing.T, volumes []corev1.Volume, audience string) {
	t.Helper()
	for _, volume := range volumes {
		if volume.Projected == nil {
			continue
		}
		for _, source := range volume.Projected.Sources {
			if source.ServiceAccountToken != nil && source.ServiceAccountToken.Audience == audience {
				return
			}
		}
	}
	t.Fatalf("projected service-account token audience %q not found in %#v", audience, volumes)
}
