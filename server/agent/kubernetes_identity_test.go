package agent

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestWorkloadVerifierBindsTokenPodJobAndExecution(t *testing.T) {
	job := workloadTestJob()
	pod := workloadTestPod(job)
	kube := fake.NewClientset(job, pod)
	kube.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		review := action.(ktesting.CreateAction).GetObject().(*authv1.TokenReview)
		if len(review.Spec.Audiences) != 1 || review.Spec.Audiences[0] != KubernetesWorkloadAudience {
			t.Fatalf("audiences = %v", review.Spec.Audiences)
		}
		return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, Audiences: []string{KubernetesWorkloadAudience}, User: authv1.UserInfo{
			Username: "system:serviceaccount:anban:runner", Extra: map[string]authv1.ExtraValue{
				KubernetesPodNameExtra: {"pod-1"}, KubernetesPodUIDExtra: {"pod-uid-1"},
			},
		}}}, nil
	})
	verifier, err := NewKubernetesWorkloadVerifier(kube, "anban", "runner")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.Verify(context.Background(), "bound-token", "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.PodName != "pod-1" || identity.PodUID != "pod-uid-1" || identity.JobName != "job-1" || identity.ExecutionID != "execution-1" || identity.TaskID != "task-1" || identity.ProjectID != "project-1" || identity.JobDeadline.IsZero() {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestWorkloadVerifierRejectsSpoofedIdentity(t *testing.T) {
	job := workloadTestJob()
	pod := workloadTestPod(job)
	for _, tc := range []struct {
		name   string
		mutate func(*authv1.TokenReview, *corev1.Pod, *batchv1.Job)
	}{
		{"unauthenticated", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) { r.Status.Authenticated = false }},
		{"review error", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) { r.Status.Error = "token rejected" }},
		{"missing audience", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) { r.Status.Audiences = nil }},
		{"wrong audience", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) { r.Status.Audiences = []string{"other"} }},
		{"ambiguous audience", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.Audiences = []string{KubernetesWorkloadAudience, "other"}
		}},
		{"wrong service account", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Username = "system:serviceaccount:anban:other"
		}},
		{"wrong namespace", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Username = "system:serviceaccount:other:runner"
		}},
		{"missing pod name", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			delete(r.Status.User.Extra, KubernetesPodNameExtra)
		}},
		{"ambiguous pod name", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Extra[KubernetesPodNameExtra] = []string{"a", "b"}
		}},
		{"missing pod uid", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			delete(r.Status.User.Extra, KubernetesPodUIDExtra)
		}},
		{"ambiguous pod uid", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Extra[KubernetesPodUIDExtra] = []string{"a", "b"}
		}},
		{"stale pod uid", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.UID = "new-uid" }},
		{"pod wrong namespace", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.Namespace = "other" }},
		{"pod wrong service account", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.Spec.ServiceAccountName = "other"
		}},
		{"standalone pod", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.OwnerReferences = nil }},
		{"wrong controller kind", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.OwnerReferences[0].Kind = "ReplicaSet" }},
		{"wrong controller api", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.OwnerReferences[0].APIVersion = "batch/v2"
		}},
		{"non-controller owner", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			controller := false
			p.OwnerReferences[0].Controller = &controller
		}},
		{"wrong owner uid", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.OwnerReferences[0].UID = "foreign" }},
		{"job uid mismatch", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.UID = "replacement" }},
		{"job wrong namespace", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.Namespace = "other" }},
		{"job wrong service account", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Spec.Template.Spec.ServiceAccountName = "other"
		}},
		{"missing job deadline", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.Spec.ActiveDeadlineSeconds = nil }},
		{"missing job start", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.Status.StartTime = nil }},
		{"missing app name", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { delete(j.Labels, "app.kubernetes.io/name") }},
		{"wrong app name", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Labels["app.kubernetes.io/name"] = "other"
		}},
		{"missing component", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			delete(j.Labels, "app.kubernetes.io/component")
		}},
		{"wrong component", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Labels["app.kubernetes.io/component"] = "other"
		}},
		{"missing execution", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			delete(j.Labels, kubernetesExecutionIDLabel)
		}},
		{"wrong execution", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Labels[kubernetesExecutionIDLabel] = "other"
		}},
		{"missing task", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { delete(j.Labels, kubernetesTaskIDLabel) }},
		{"conflicting task", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.Labels[kubernetesTaskIDLabel] = "other" }},
		{"missing project", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { delete(j.Labels, kubernetesProjectIDLabel) }},
		{"conflicting project", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Labels[kubernetesProjectIDLabel] = "other"
		}},
		{"missing user", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { delete(j.Labels, kubernetesUserIDLabel) }},
		{"conflicting user", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) { j.Labels[kubernetesUserIDLabel] = "other" }},
		{"pod missing app name", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { delete(p.Labels, "app.kubernetes.io/name") }},
		{"pod conflicting app name", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.Labels["app.kubernetes.io/name"] = "other"
		}},
		{"pod missing component", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			delete(p.Labels, "app.kubernetes.io/component")
		}},
		{"pod conflicting component", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.Labels["app.kubernetes.io/component"] = "other"
		}},
		{"pod missing execution", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			delete(p.Labels, kubernetesExecutionIDLabel)
		}},
		{"pod conflicting execution", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.Labels[kubernetesExecutionIDLabel] = "other"
		}},
		{"pod missing task", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { delete(p.Labels, kubernetesTaskIDLabel) }},
		{"pod conflicting task", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.Labels[kubernetesTaskIDLabel] = "other" }},
		{"pod missing project", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { delete(p.Labels, kubernetesProjectIDLabel) }},
		{"pod conflicting project", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) {
			p.Labels[kubernetesProjectIDLabel] = "other"
		}},
		{"pod missing user", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { delete(p.Labels, kubernetesUserIDLabel) }},
		{"pod conflicting user", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.Labels[kubernetesUserIDLabel] = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := job.DeepCopy()
			p := pod.DeepCopy()
			r := &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, Audiences: []string{KubernetesWorkloadAudience}, User: authv1.UserInfo{Username: "system:serviceaccount:anban:runner", Extra: map[string]authv1.ExtraValue{KubernetesPodNameExtra: {"pod-1"}, KubernetesPodUIDExtra: {"pod-uid-1"}}}}}
			tc.mutate(r, p, j)
			kube := fake.NewClientset(j, p)
			kube.PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) { return true, r, nil })
			v, _ := NewKubernetesWorkloadVerifier(kube, "anban", "runner")
			if _, err := v.Verify(context.Background(), "token", "execution-1"); err == nil {
				t.Fatal("spoofed workload accepted")
			}
		})
	}
}

func TestWorkloadVerifierRejectsMissingBoundObjects(t *testing.T) {
	for _, missing := range []string{"pod", "job"} {
		t.Run(missing, func(t *testing.T) {
			job := workloadTestJob()
			pod := workloadTestPod(job)
			objects := []runtime.Object{}
			if missing != "job" {
				objects = append(objects, job)
			}
			if missing != "pod" {
				objects = append(objects, pod)
			}
			kube := fake.NewClientset(objects...)
			kube.PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, Audiences: []string{KubernetesWorkloadAudience}, User: authv1.UserInfo{Username: "system:serviceaccount:anban:runner", Extra: map[string]authv1.ExtraValue{KubernetesPodNameExtra: {"pod-1"}, KubernetesPodUIDExtra: {"pod-uid-1"}}}}}, nil
			})
			v, _ := NewKubernetesWorkloadVerifier(kube, "anban", "runner")
			if _, err := v.Verify(context.Background(), "token", "execution-1"); err == nil {
				t.Fatalf("missing %s accepted", missing)
			}
		})
	}
}

func workloadTestJob() *batchv1.Job {
	deadline := int64(300)
	started := metav1.Now()
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "anban", UID: types.UID("job-uid-1"), Labels: map[string]string{
		"app.kubernetes.io/name": kubernetesAgentAppName, "app.kubernetes.io/component": "execution", kubernetesExecutionIDLabel: "execution-1", kubernetesTaskIDLabel: "task-1", kubernetesProjectIDLabel: "project-1", kubernetesUserIDLabel: "user-1",
	}}, Spec: batchv1.JobSpec{ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{ServiceAccountName: "runner"}}}, Status: batchv1.JobStatus{StartTime: &started}}
}
func workloadTestPod(job *batchv1.Job) *corev1.Pod {
	controller := true
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "anban", UID: types.UID("pod-uid-1"), Labels: copyLabels(job.Labels), OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}}, Spec: corev1.PodSpec{ServiceAccountName: "runner"}}
}
