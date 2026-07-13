package agent

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != kubernetesAgentUID || c.SecurityContext.RunAsGroup == nil || *c.SecurityContext.RunAsGroup != kubernetesAgentGID {
		t.Fatalf("container identity = %#v, want %d:%d", c.SecurityContext, kubernetesAgentUID, kubernetesAgentGID)
	}
	if c.SecurityContext.Capabilities == nil || !slices.Contains(c.SecurityContext.Capabilities.Drop, corev1.Capability("ALL")) {
		t.Fatalf("capabilities = %#v, want ALL dropped", c.SecurityContext.Capabilities)
	}
	assertMount(t, c, kubernetesWorkspaceMountName, "/workspace", false)
	assertMount(t, c, kubernetesMemoryMountName, "/workspace/.claude/memory", false)
	assertMount(t, c, kubernetesHomeVolumeName, "/home/node", false)
	if home := requireTestVolume(t, job, kubernetesHomeVolumeName); home.EmptyDir == nil {
		t.Fatalf("home volume = %#v, want writable emptyDir", home)
	}
	assertMount(t, c, kubernetesTokenVolumeName, kubernetesTokenMountPath, true)
	assertProjectedAudience(t, spec.Volumes, kubernetesTokenAudience)
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatalf("automount token = %#v, want false", spec.AutomountServiceAccountToken)
	}
	if spec.SecurityContext == nil || spec.SecurityContext.SeccompProfile == nil || spec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod security context = %#v, want RuntimeDefault seccomp", spec.SecurityContext)
	}
	if spec.SecurityContext.RunAsUser == nil || *spec.SecurityContext.RunAsUser != kubernetesAgentUID || spec.SecurityContext.RunAsGroup == nil || *spec.SecurityContext.RunAsGroup != kubernetesAgentGID {
		t.Fatalf("pod identity = %#v, want %d:%d", spec.SecurityContext, kubernetesAgentUID, kubernetesAgentGID)
	}
	if len(spec.ImagePullSecrets) != 1 || spec.ImagePullSecrets[0].Name != "acr-secret" {
		t.Fatalf("image pull secrets = %#v, want acr-secret", spec.ImagePullSecrets)
	}
	if got := strings.Join(append(c.Command, c.Args...), " "); !strings.Contains(got, "anban job") || !strings.Contains(got, "--server-url https://creator-server:8443") || !strings.Contains(got, "--execution-id execution-1") || !strings.Contains(got, "--workload-token-file "+kubernetesTokenFile) {
		t.Fatalf("command = %q, want one-shot job bootstrap args", got)
	}
	if got := strings.Join(append(c.Command, c.Args...), " "); strings.Contains(got, testTask().Prompt) {
		t.Fatalf("command embeds task prompt: %q", got)
	}
	if len(c.Env) != 2 || c.Env[0].Name != "HOME" || c.Env[0].Value != "/home/node" || c.Env[1].Name != "ANBAN_JOB_FINALIZATION_TIMEOUT" || c.Env[1].Value != "25s" {
		t.Fatalf("environment = %#v, want HOME and grace-aligned finalization timeout", c.Env)
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

func TestKubernetesFinalizationTimeoutIsBoundedByGraceAndDeadline(t *testing.T) {
	tests := []struct {
		grace    int
		deadline int64
		want     int64
	}{
		{grace: 30, deadline: 900, want: 25},
		{grace: 4, deadline: 900, want: 4},
		{grace: 600, deadline: 900, want: 300},
		{grace: 600, deadline: 20, want: 15},
		{grace: 0, deadline: 20, want: 15},
	}
	for _, tc := range tests {
		if got := kubernetesFinalizationTimeoutSeconds(tc.grace, tc.deadline); got != tc.want {
			t.Fatalf("timeout(%d,%d)=%d want %d", tc.grace, tc.deadline, got, tc.want)
		}
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

func TestVerifyKubernetesJobRejectsSecurityAndRuntimeSpecMutation(t *testing.T) {
	desired := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	for _, tc := range []struct {
		name   string
		mutate func(*batchv1.Job)
	}{
		{name: "backoff", mutate: func(job *batchv1.Job) { *job.Spec.BackoffLimit = 1 }},
		{name: "parallelism", mutate: func(job *batchv1.Job) { job.Spec.Parallelism = int32Ptr(2) }},
		{name: "completions", mutate: func(job *batchv1.Job) { job.Spec.Completions = int32Ptr(2) }},
		{name: "deadline", mutate: func(job *batchv1.Job) { *job.Spec.ActiveDeadlineSeconds = 901 }},
		{name: "TTL", mutate: func(job *batchv1.Job) { *job.Spec.TTLSecondsAfterFinished = 121 }},
		{name: "suspend", mutate: func(job *batchv1.Job) { job.Spec.Suspend = boolPtr(true) }},
		{name: "completion mode", mutate: func(job *batchv1.Job) {
			mode := batchv1.IndexedCompletion
			job.Spec.CompletionMode = &mode
		}},
		{name: "managed by", mutate: func(job *batchv1.Job) { job.Spec.ManagedBy = stringPtr("foreign.example/controller") }},
		{name: "pod failure policy", mutate: func(job *batchv1.Job) { job.Spec.PodFailurePolicy = &batchv1.PodFailurePolicy{} }},
		{name: "pod replacement policy", mutate: func(job *batchv1.Job) {
			policy := batchv1.Failed
			job.Spec.PodReplacementPolicy = &policy
		}},
		{name: "success policy", mutate: func(job *batchv1.Job) { job.Spec.SuccessPolicy = &batchv1.SuccessPolicy{} }},
		{name: "backoff per index", mutate: func(job *batchv1.Job) { job.Spec.BackoffLimitPerIndex = int32Ptr(1) }},
		{name: "max failed indexes", mutate: func(job *batchv1.Job) { job.Spec.MaxFailedIndexes = int32Ptr(1) }},
		{name: "manual selector", mutate: func(job *batchv1.Job) { job.Spec.ManualSelector = boolPtr(true) }},
		{name: "foreign selector", mutate: func(job *batchv1.Job) {
			job.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"foreign": "selector"}}
		}},
		{name: "restart", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure }},
		{name: "service account", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.ServiceAccountName = "foreign" }},
		{name: "automount", mutate: func(job *batchv1.Job) { *job.Spec.Template.Spec.AutomountServiceAccountToken = true }},
		{name: "grace", mutate: func(job *batchv1.Job) { *job.Spec.Template.Spec.TerminationGracePeriodSeconds++ }},
		{name: "host network", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.HostNetwork = true }},
		{name: "host PID", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.HostPID = true }},
		{name: "host IPC", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.HostIPC = true }},
		{name: "shared process namespace", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.ShareProcessNamespace = boolPtr(true) }},
		{name: "host users", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.HostUsers = boolPtr(false) }},
		{name: "runtime class", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.RuntimeClassName = stringPtr("foreign") }},
		{name: "node selector", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.NodeSelector = map[string]string{"dedicated": "foreign"}
		}},
		{name: "affinity", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Affinity = &corev1.Affinity{} }},
		{name: "tolerations", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Tolerations = []corev1.Toleration{{Key: "foreign"}} }},
		{name: "pod security", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.SecurityContext = nil }},
		{name: "init container", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "init"}} }},
		{name: "extra container", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers = append(job.Spec.Template.Spec.Containers, corev1.Container{Name: "sidecar"})
		}},
		{name: "container name", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].Name = "foreign" }},
		{name: "image", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].Image = "foreign/image" }},
		{name: "pull policy", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent }},
		{name: "command", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].Command = []string{"sleep"} }},
		{name: "args", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].Args[0] = "run" }},
		{name: "working directory", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].WorkingDir = "/foreign" }},
		{name: "environment", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "FOREIGN", Value: "1"}}
		}},
		{name: "home environment", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "HOME", Value: "/foreign"}}
		}},
		{name: "environment source", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{Prefix: "FOREIGN_"}}
		}},
		{name: "ports", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8080}}
		}},
		{name: "liveness probe", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{} }},
		{name: "lifecycle", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"sh", "-c", "echo foreign"}},
			}}
		}},
		{name: "container security", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].SecurityContext = nil }},
		{name: "resources", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("3")
		}},
		{name: "mounts", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath = "/foreign" }},
		{name: "memory claim", mutate: func(job *batchv1.Job) {
			requireTestVolume(t, job, kubernetesMemoryMountName).PersistentVolumeClaim.ClaimName = "foreign"
		}},
		{name: "token audience", mutate: func(job *batchv1.Job) { requireTestTokenProjection(t, job).Audience = "foreign" }},
		{name: "token path", mutate: func(job *batchv1.Job) { requireTestTokenProjection(t, job).Path = "foreign" }},
		{name: "token expiration", mutate: func(job *batchv1.Job) { *requireTestTokenProjection(t, job).ExpirationSeconds++ }},
		{name: "token mode", mutate: func(job *batchv1.Job) {
			*requireTestVolume(t, job, kubernetesTokenVolumeName).Projected.DefaultMode = 0777
		}},
		{name: "volumes", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.Volumes = job.Spec.Template.Spec.Volumes[1:] }},
		{name: "home volume", mutate: func(job *batchv1.Job) {
			requireTestVolume(t, job, kubernetesHomeVolumeName).EmptyDir = nil
		}},
		{name: "pull secret", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.ImagePullSecrets[0].Name = "foreign" }},
		{name: "template label", mutate: func(job *batchv1.Job) { delete(job.Spec.Template.Labels, kubernetesExecutionIDLabel) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			if err := verifyJob(existing, desired, testExecution(), testTask()); err == nil || !strings.Contains(err.Error(), "configuration mismatch") {
				t.Fatalf("verifyJob error = %v, want configuration mismatch", err)
			}
		})
	}
}

func TestVerifyKubernetesJobIgnoresSafeAPIServerDefaults(t *testing.T) {
	desired := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	existing := desired.DeepCopy()
	existing.Annotations[kubernetesObjectConfigHashLabel] = "stale-diagnostic-hash"
	existing.UID = types.UID("job-uid-1")
	existing.Spec.Parallelism = int32Ptr(1)
	existing.Spec.Completions = int32Ptr(1)
	existing.Spec.ManualSelector = boolPtr(false)
	mode := batchv1.NonIndexedCompletion
	existing.Spec.CompletionMode = &mode
	existing.Spec.Suspend = boolPtr(false)
	replacement := batchv1.TerminatingOrFailed
	existing.Spec.PodReplacementPolicy = &replacement
	existing.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{
		batchv1.ControllerUidLabel: string(existing.UID),
	}}
	for key, value := range map[string]string{
		"controller-uid":           string(existing.UID),
		batchv1.ControllerUidLabel: string(existing.UID),
		"job-name":                 existing.Name,
		batchv1.JobNameLabel:       existing.Name,
	} {
		existing.Spec.Template.Labels[key] = value
	}
	enableServiceLinks := true
	existing.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	existing.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
	existing.Spec.Template.Spec.EnableServiceLinks = &enableServiceLinks
	existing.Spec.Template.Spec.HostUsers = boolPtr(true)
	existing.Spec.Template.Spec.ShareProcessNamespace = boolPtr(false)
	existing.Spec.Template.Spec.Containers[0].TerminationMessagePath = corev1.TerminationMessagePathDefault
	existing.Spec.Template.Spec.Containers[0].TerminationMessagePolicy = corev1.TerminationMessageReadFile
	if err := verifyJob(existing, desired, testExecution(), testTask()); err != nil {
		t.Fatalf("verifyJob with API defaults: %v", err)
	}
}

func TestVerifyProjectMemoryPVCAcceptsEqualOrLargerAndRejectsSmaller(t *testing.T) {
	desired := buildProjectMemoryPVC(testJobConfig(), "project-1")
	for _, tc := range []struct {
		name    string
		size    string
		wantErr bool
	}{
		{name: "equal", size: "1Gi"},
		{name: "larger", size: "2Gi"},
		{name: "smaller", size: "512Mi", wantErr: true},
		{name: "missing", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			if tc.size == "" {
				delete(existing.Spec.Resources.Requests, corev1.ResourceStorage)
			} else {
				existing.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(tc.size)
			}
			err := verifyPVC(existing, desired, "project-1")
			if tc.wantErr && (err == nil || !strings.Contains(err.Error(), "expand")) {
				t.Fatalf("verifyPVC error = %v, want actionable expansion error", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("verifyPVC: %v", err)
			}
		})
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
					if !IsPermanentDispatchError(err) {
						t.Fatalf("identity mismatch error = %T, want permanent dispatch error", err)
					}
				})
			}
		}
	}
}

func TestKubernetesDispatcherLeavesCreateTimeoutAmbiguous(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask())
	if err == nil {
		t.Fatal("expected create timeout")
	}
	if IsPermanentDispatchError(err) {
		t.Fatalf("create timeout was classified permanent: %v", err)
	}
}

func TestKubernetesDispatcherClassifiesAPIErrors(t *testing.T) {
	resource := schema.GroupResource{Group: "batch", Resource: "jobs"}
	statusCases := []struct {
		name      string
		err       error
		permanent bool
	}{
		{name: "forbidden", err: apierrors.NewForbidden(resource, "object-1", errors.New("denied")), permanent: true},
		{name: "unauthorized", err: apierrors.NewUnauthorized("identity rejected"), permanent: true},
		{name: "invalid", err: apierrors.NewInvalid(schema.GroupKind{Group: "batch", Kind: "Job"}, "object-1", field.ErrorList{field.Invalid(field.NewPath("spec"), "bad", "invalid")}), permanent: true},
		{name: "bad request", err: apierrors.NewBadRequest("malformed"), permanent: true},
		{name: "timeout", err: apierrors.NewTimeoutError("timeout", 1)},
		{name: "server timeout", err: apierrors.NewServerTimeout(resource, "create", 1)},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("down")},
		{name: "internal error", err: apierrors.NewInternalError(errors.New("broken"))},
		{name: "too many requests", err: apierrors.NewTooManyRequests("slow down", 1)},
		{name: "conflict", err: apierrors.NewConflict(resource, "object-1", errors.New("changed"))},
		{name: "transport reset", err: errors.New("connection reset by peer")},
	}
	operations := []struct {
		name     string
		resource string
		verb     string
	}{
		{name: "PVC GET", resource: "persistentvolumeclaims", verb: "get"},
		{name: "PVC Create", resource: "persistentvolumeclaims", verb: "create"},
		{name: "Job GET", resource: "jobs", verb: "get"},
		{name: "Job Create", resource: "jobs", verb: "create"},
	}
	for _, operation := range operations {
		for _, testCase := range statusCases {
			t.Run(operation.name+"/"+testCase.name, func(t *testing.T) {
				client := fake.NewSimpleClientset()
				if operation.resource == "jobs" {
					if _, err := client.CoreV1().PersistentVolumeClaims("anban").Create(context.Background(),
						buildProjectMemoryPVC(testJobConfig(), "project-1"), metav1.CreateOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				client.PrependReactor(operation.verb, operation.resource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.err
				})
				err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask())
				if err == nil {
					t.Fatal("expected API error")
				}
				if got := IsPermanentDispatchError(err); got != testCase.permanent {
					t.Fatalf("permanent = %v, want %v: %v", got, testCase.permanent, err)
				}
			})
		}
	}

	for _, resourceName := range []string{"persistentvolumeclaims", "jobs"} {
		t.Run(resourceName+" create parent not found", func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if resourceName == "jobs" {
				_, _ = client.CoreV1().PersistentVolumeClaims("anban").Create(context.Background(),
					buildProjectMemoryPVC(testJobConfig(), "project-1"), metav1.CreateOptions{})
			}
			client.PrependReactor("create", resourceName, func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: resourceName}, "anban")
			})
			err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask())
			if err == nil || !IsPermanentDispatchError(err) {
				t.Fatalf("create NotFound error = %v, want permanent", err)
			}
		})
	}
}

func TestKubernetesDispatcherLeavesPostAlreadyExistsNotFoundRetryable(t *testing.T) {
	for _, resourceName := range []string{"persistentvolumeclaims", "jobs"} {
		t.Run(resourceName, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if resourceName == "jobs" {
				_, _ = client.CoreV1().PersistentVolumeClaims("anban").Create(context.Background(),
					buildProjectMemoryPVC(testJobConfig(), "project-1"), metav1.CreateOptions{})
			}
			client.PrependReactor("get", resourceName, func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: resourceName}, "object-1")
			})
			client.PrependReactor("create", resourceName, func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: resourceName}, "object-1")
			})
			err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask())
			if err == nil {
				t.Fatal("expected post-AlreadyExists disappearance error")
			}
			if IsPermanentDispatchError(err) {
				t.Fatalf("disappearance race was classified permanent: %v", err)
			}
		})
	}
}

func TestKubernetesDispatcherDeleteUsesForegroundPropagation(t *testing.T) {
	ctx := context.Background()
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	job.UID = types.UID("job-uid-1")
	client := fake.NewSimpleClientset(job)
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
	if got := deleteAction.GetDeleteOptions().Preconditions; got == nil || got.UID == nil || *got.UID != job.UID {
		t.Fatalf("preconditions = %#v, want UID %q", got, job.UID)
	}
	if err := d.Delete(ctx, testExecution()); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestKubernetesDispatcherDeleteRejectsForeignJob(t *testing.T) {
	desired := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	for _, label := range []string{
		"app.kubernetes.io/name",
		"app.kubernetes.io/component",
		kubernetesExecutionIDLabel,
		kubernetesTaskIDLabel,
	} {
		t.Run(label, func(t *testing.T) {
			job := desired.DeepCopy()
			delete(job.Labels, label)
			client := fake.NewSimpleClientset(job)
			err := testDispatcher(client).Delete(context.Background(), testExecution())
			if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
				t.Fatalf("Delete error = %v, want identity mismatch", err)
			}
			if _, getErr := client.BatchV1().Jobs("anban").Get(context.Background(), job.Name, metav1.GetOptions{}); getErr != nil {
				t.Fatalf("foreign Job was deleted: %v", getErr)
			}
		})
	}
}

func TestKubernetesDispatcherDeleteProjectMemoryIsGuardedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	pvc := buildProjectMemoryPVC(testJobConfig(), "project-1")
	pvc.UID = types.UID("pvc-uid-1")
	client := fake.NewSimpleClientset(pvc)
	d := testDispatcher(client)
	if err := d.DeleteProjectMemory(ctx, "project-1"); err != nil {
		t.Fatalf("DeleteProjectMemory: %v", err)
	}
	deleteAction, ok := client.Actions()[len(client.Actions())-1].(ktesting.DeleteAction)
	if !ok {
		t.Fatalf("last action = %T, want DeleteAction", client.Actions()[len(client.Actions())-1])
	}
	if got := deleteAction.GetDeleteOptions().Preconditions; got == nil || got.UID == nil || *got.UID != pvc.UID {
		t.Fatalf("preconditions = %#v, want UID %q", got, pvc.UID)
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
	job.UID = types.UID("job-uid-1")
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
	ownTestPod(job, pod)
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
		{name: "complete active", condition: &batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Reason: "Completed"}, active: 1, want: kubernetesPhaseSucceeded},
		{name: "failed active", condition: &batchv1.JobCondition{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "Failed"}, active: 1, want: kubernetesPhaseFailed},
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
	job.UID = types.UID("job-uid-1")
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
	ownTestPod(job, pod)
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
	job.UID = types.UID("job-uid-1")
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
	ownTestPod(job, pod)
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
	job.UID = types.UID("job-uid-1")
	job.Status.Active = 1
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-job-pod",
			Namespace: "anban",
			Labels:    map[string]string{kubernetesExecutionIDLabel: testExecution().ID},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name:  kubernetesAgentContainerName,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}}},
	}
	ownTestPod(job, pod)
	state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhaseRunning {
		t.Fatalf("phase = %q, want running for a running Pod", state.Phase)
	}
}

func TestKubernetesDispatcherInspectMapsContainerWaitingDiagnostics(t *testing.T) {
	for _, reason := range []string{"ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError"} {
		t.Run(reason, func(t *testing.T) {
			job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
			job.UID = types.UID("job-uid-1")
			job.Status.Active = 1
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "waiting", Namespace: "anban", Labels: map[string]string{kubernetesExecutionIDLabel: testExecution().ID}},
				Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
					Name:  kubernetesAgentContainerName,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: "waiting message"}},
				}}},
			}
			ownTestPod(job, pod)
			state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if state.Phase != kubernetesPhasePending || state.Reason != reason || state.Message != "waiting message" {
				t.Fatalf("state = %#v, want pending waiting diagnostics", state)
			}
		})
	}
}

func TestKubernetesDispatcherInspectPreservesTerminalJobDiagnosticsFromWaitingPod(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	job.UID = types.UID("job-uid-1")
	job.Status.Active = 1
	job.Status.Conditions = []batchv1.JobCondition{{
		Type:    batchv1.JobComplete,
		Status:  corev1.ConditionTrue,
		Reason:  "Completed",
		Message: "job completed",
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "waiting", Namespace: "anban", Labels: map[string]string{kubernetesExecutionIDLabel: testExecution().ID}},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
			Name: kubernetesAgentContainerName,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason:  "ImagePullBackOff",
				Message: "stale waiting diagnostic",
			}},
		}}},
	}
	ownTestPod(job, pod)
	state, err := testDispatcher(fake.NewSimpleClientset(job, pod)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Phase != kubernetesPhaseSucceeded || state.Reason != "Completed" || state.Message != "job completed" {
		t.Fatalf("state = %#v, want authoritative terminal Job diagnostics", state)
	}
}

func TestKubernetesDispatcherInspectIgnoresSpoofedOrStalePods(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	job.UID = types.UID("job-uid-current")
	owned := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "owned", Namespace: "anban", UID: types.UID("owned-uid"),
		CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
		Labels:            map[string]string{kubernetesExecutionIDLabel: testExecution().ID},
	}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	ownTestPod(job, owned)
	spoof := owned.DeepCopy()
	spoof.Name = "spoof"
	spoof.UID = types.UID("spoof-uid")
	spoof.CreationTimestamp = metav1.NewTime(time.Unix(200, 0))
	spoof.OwnerReferences = nil
	spoof.Status.Phase = corev1.PodFailed
	stale := spoof.DeepCopy()
	stale.Name = "stale"
	stale.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: types.UID("old-job-uid"), Controller: boolPtr(true)}}
	state, err := testDispatcher(fake.NewSimpleClientset(job, owned, spoof, stale)).Inspect(context.Background(), testExecution())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.PodUID != "owned-uid" || state.Phase != kubernetesPhaseRunning {
		t.Fatalf("state = %#v, want only current Job-owned Pod", state)
	}
}

func TestNewestPodUsesStableNameAndUIDTieBreak(t *testing.T) {
	timestamp := metav1.NewTime(time.Unix(100, 0))
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", UID: types.UID("uid-z"), CreationTimestamp: timestamp}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", UID: types.UID("uid-a"), CreationTimestamp: timestamp}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", UID: types.UID("uid-z"), CreationTimestamp: timestamp}},
	}
	if got := newestPod(pods); got == nil || got.Name != "pod-b" || got.UID != types.UID("uid-z") {
		t.Fatalf("newestPod = %#v, want stable pod-b/uid-z tie break", got)
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

func TestKubernetesDispatcherRecoversFromPVCCreateAlreadyExistsRace(t *testing.T) {
	desiredPVC := buildProjectMemoryPVC(testJobConfig(), "project-1")
	desiredJob := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	client := fake.NewSimpleClientset(desiredJob)
	gets := 0
	client.PrependReactor("get", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		gets++
		if gets == 1 {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "persistentvolumeclaims"}, desiredPVC.Name)
		}
		return true, desiredPVC.DeepCopy(), nil
	})
	client.PrependReactor("create", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "persistentvolumeclaims"}, desiredPVC.Name)
	})
	if err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask()); err != nil {
		t.Fatalf("Dispatch after PVC create race: %v", err)
	}
	if gets != 2 {
		t.Fatalf("PVC GET calls = %d, want initial lookup plus race recovery lookup", gets)
	}
}

func TestKubernetesDispatcherRecoversFromJobCreateAlreadyExistsRace(t *testing.T) {
	desiredPVC := buildProjectMemoryPVC(testJobConfig(), "project-1")
	desiredJob := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	client := fake.NewSimpleClientset(desiredPVC)
	gets := 0
	client.PrependReactor("get", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
		gets++
		if gets == 1 {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, desiredJob.Name)
		}
		return true, desiredJob.DeepCopy(), nil
	})
	client.PrependReactor("create", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "jobs"}, desiredJob.Name)
	})
	if err := testDispatcher(client).Dispatch(context.Background(), testExecution(), testTask()); err != nil {
		t.Fatalf("Dispatch after Job create race: %v", err)
	}
	if gets != 2 {
		t.Fatalf("Job GET calls = %d, want initial lookup plus race recovery lookup", gets)
	}
}

func TestLegacyKubernetesWorkspaceInitUsesNumericOwnership(t *testing.T) {
	got := kubernetesWorkspaceInitScript("/workspace/project")
	if !strings.Contains(got, "chown -R 1000:1000") || strings.Contains(got, "node:node") {
		t.Fatalf("workspace init script = %q, want numeric runtime ownership", got)
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
			CompletionGraceSeconds:  30,
			Resources: srvconfig.KubernetesResourceConfig{
				Requests: map[string]string{"cpu": "500m", "memory": "1Gi"},
				Limits:   map[string]string{"cpu": "2", "memory": "2Gi"},
			},
		},
		ServerURL: "https://creator-server:8443",
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

func requireTestVolume(t *testing.T, job *batchv1.Job, name string) *corev1.VolumeSource {
	t.Helper()
	for i := range job.Spec.Template.Spec.Volumes {
		if job.Spec.Template.Spec.Volumes[i].Name == name {
			return &job.Spec.Template.Spec.Volumes[i].VolumeSource
		}
	}
	t.Fatalf("volume %q not found", name)
	return nil
}

func requireTestTokenProjection(t *testing.T, job *batchv1.Job) *corev1.ServiceAccountTokenProjection {
	t.Helper()
	projected := requireTestVolume(t, job, kubernetesTokenVolumeName).Projected
	if projected == nil {
		t.Fatal("token projected volume missing")
	}
	for i := range projected.Sources {
		if projected.Sources[i].ServiceAccountToken != nil {
			return projected.Sources[i].ServiceAccountToken
		}
	}
	t.Fatal("service account token projection missing")
	return nil
}

func ownTestPod(job *batchv1.Job, pod *corev1.Pod) {
	pod.Labels[batchv1.JobNameLabel] = job.Name
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       job.Name,
		UID:        job.UID,
		Controller: boolPtr(true),
	}}
}

func boolPtr(value bool) *bool       { return &value }
func stringPtr(value string) *string { return &value }
