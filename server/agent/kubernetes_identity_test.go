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
		{"wrong audience", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) { r.Status.Audiences = []string{"other"} }},
		{"wrong service account", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Username = "system:serviceaccount:anban:other"
		}},
		{"ambiguous pod uid", func(r *authv1.TokenReview, _ *corev1.Pod, _ *batchv1.Job) {
			r.Status.User.Extra[KubernetesPodUIDExtra] = []string{"a", "b"}
		}},
		{"stale pod uid", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.UID = "new-uid" }},
		{"standalone pod", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.OwnerReferences = nil }},
		{"wrong owner uid", func(_ *authv1.TokenReview, p *corev1.Pod, _ *batchv1.Job) { p.OwnerReferences[0].UID = "foreign" }},
		{"wrong execution", func(_ *authv1.TokenReview, _ *corev1.Pod, j *batchv1.Job) {
			j.Labels[kubernetesExecutionIDLabel] = "other"
		}},
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

func workloadTestJob() *batchv1.Job {
	deadline := int64(300)
	started := metav1.Now()
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "anban", UID: types.UID("job-uid-1"), Labels: map[string]string{
		"app.kubernetes.io/name": kubernetesAgentAppName, "app.kubernetes.io/component": "execution", kubernetesExecutionIDLabel: "execution-1", kubernetesTaskIDLabel: "task-1", kubernetesProjectIDLabel: "project-1", kubernetesUserIDLabel: "user-1",
	}}, Spec: batchv1.JobSpec{ActiveDeadlineSeconds: &deadline}, Status: batchv1.JobStatus{StartTime: &started}}
}
func workloadTestPod(job *batchv1.Job) *corev1.Pod {
	controller := true
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "anban", UID: types.UID("pod-uid-1"), OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}}}
}
