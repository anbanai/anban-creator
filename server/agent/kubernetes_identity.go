package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/anbanai/anban-creator/server/model"
)

const (
	KubernetesWorkloadAudience = "anban-server"
	KubernetesPodNameExtra     = "authentication.kubernetes.io/pod-name"
	KubernetesPodUIDExtra      = "authentication.kubernetes.io/pod-uid"
)

// WorkloadIdentity is the provider-neutral, verified identity of one runtime
// instance and the execution ownership it is allowed to bootstrap. Target is
// the provider kind; RuntimeIdentity fields remain provider-owned coordinates.
type WorkloadIdentity struct {
	model.RuntimeIdentity
	Target      string
	ExecutionID string
	TaskID      string
	ProjectID   string
	UserID      string
	Deadline    time.Time
}

// WorkloadVerifier authenticates a provider credential and resolves it to the
// common runtime and execution ownership identity.
type WorkloadVerifier interface {
	Verify(context.Context, string) (*WorkloadIdentity, error)
}

type KubernetesWorkloadVerifier struct {
	kube           kubernetes.Interface
	namespace      string
	serviceAccount string
}

func NewKubernetesWorkloadVerifier(kube kubernetes.Interface, namespace, serviceAccount string) (*KubernetesWorkloadVerifier, error) {
	if kube == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(serviceAccount) == "" {
		return nil, errors.New("Kubernetes workload verifier requires client, namespace, and service account")
	}
	return &KubernetesWorkloadVerifier{kube: kube, namespace: namespace, serviceAccount: serviceAccount}, nil
}

func (v *KubernetesWorkloadVerifier) Verify(ctx context.Context, token string) (*WorkloadIdentity, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("workload token is required")
	}
	review, err := v.kube.AuthenticationV1().TokenReviews().Create(ctx, &authv1.TokenReview{Spec: authv1.TokenReviewSpec{Token: token, Audiences: []string{KubernetesWorkloadAudience}}}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("review workload token: %w", err)
	}
	expectedUser := "system:serviceaccount:" + v.namespace + ":" + v.serviceAccount
	if strings.TrimSpace(review.Status.Error) != "" || !review.Status.Authenticated || review.Status.User.Username != expectedUser {
		return nil, errors.New("workload token is not authenticated as the configured service account")
	}
	if len(review.Status.Audiences) != 1 || !slices.Contains(review.Status.Audiences, KubernetesWorkloadAudience) {
		return nil, errors.New("workload token audience mismatch")
	}
	podName, err := singleExtra(review.Status.User.Extra, KubernetesPodNameExtra)
	if err != nil {
		return nil, err
	}
	podUID, err := singleExtra(review.Status.User.Extra, KubernetesPodUIDExtra)
	if err != nil {
		return nil, err
	}
	pod, err := v.kube.CoreV1().Pods(v.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get bound Pod: %w", err)
	}
	if pod.Namespace != v.namespace || pod.Spec.ServiceAccountName != v.serviceAccount {
		return nil, errors.New("bound Pod service account identity mismatch")
	}
	if string(pod.UID) != podUID {
		return nil, errors.New("bound Pod UID mismatch")
	}
	if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return nil, errors.New("bound Pod is terminating or terminal")
	}
	var ownerName, ownerUID string
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "Job" && owner.APIVersion == "batch/v1" {
			if ownerName != "" {
				return nil, errors.New("bound Pod has ambiguous Job controllers")
			}
			ownerName, ownerUID = owner.Name, string(owner.UID)
		}
	}
	if ownerName == "" || ownerUID == "" {
		return nil, errors.New("bound Pod is not controlled by a Job")
	}
	job, err := v.kube.BatchV1().Jobs(v.namespace).Get(ctx, ownerName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get owning Job: %w", err)
	}
	if job.Namespace != v.namespace || job.Spec.Template.Spec.ServiceAccountName != v.serviceAccount {
		return nil, errors.New("owning Job service account identity mismatch")
	}
	if string(job.UID) != ownerUID {
		return nil, errors.New("owning Job UID mismatch")
	}
	if job.DeletionTimestamp != nil || (job.Spec.Suspend != nil && *job.Spec.Suspend) || terminalJobCondition(job.Status.Conditions) {
		return nil, errors.New("owning Job is terminating, suspended, or terminal")
	}
	if job.Status.StartTime == nil || job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds <= 0 {
		return nil, errors.New("owning Job has no active deadline identity")
	}
	jobDeadline := job.Status.StartTime.Add(time.Duration(*job.Spec.ActiveDeadlineSeconds) * time.Second)
	if !jobDeadline.After(time.Now()) {
		return nil, errors.New("owning Job active deadline has expired")
	}
	labels := job.Labels
	executionID := strings.TrimSpace(labels[kubernetesExecutionIDLabel])
	taskID := strings.TrimSpace(labels[kubernetesTaskIDLabel])
	projectID := strings.TrimSpace(labels[kubernetesProjectIDLabel])
	userID := strings.TrimSpace(labels[kubernetesUserIDLabel])
	if labels["app.kubernetes.io/name"] != kubernetesAgentAppName || labels["app.kubernetes.io/component"] != "execution" ||
		executionID == "" || taskID == "" || projectID == "" || userID == "" {
		return nil, errors.New("owning Job runtime identity labels mismatch")
	}
	for _, key := range []string{"app.kubernetes.io/name", "app.kubernetes.io/component", kubernetesExecutionIDLabel, kubernetesTaskIDLabel, kubernetesProjectIDLabel, kubernetesUserIDLabel} {
		if pod.Labels[key] == "" || pod.Labels[key] != labels[key] {
			return nil, errors.New("bound Pod and owning Job runtime identity labels conflict")
		}
	}
	if !validRuntimeIdentityLabel(executionID) || !validRuntimeIdentityLabel(taskID) || !validRuntimeIdentityLabel(projectID) || !validRuntimeIdentityLabel(userID) {
		return nil, errors.New("owning Job runtime identity labels are malformed")
	}
	return &WorkloadIdentity{
		RuntimeIdentity: model.RuntimeIdentity{Scope: v.namespace, Workload: job.Name, InstanceID: string(job.UID)},
		Target:          "kubernetes",
		ExecutionID:     executionID,
		TaskID:          taskID,
		ProjectID:       projectID,
		UserID:          userID,
		Deadline:        jobDeadline,
	}, nil
}

func validRuntimeIdentityLabel(value string) bool {
	return value != "" && value == kubernetesLabelValue(value) && len(value) <= 63
}

func terminalJobCondition(conditions []batchv1.JobCondition) bool {
	for _, condition := range conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed || condition.Type == batchv1.JobFailureTarget || condition.Type == batchv1.JobSuccessCriteriaMet) {
			return true
		}
	}
	return false
}

func singleExtra(extra map[string]authv1.ExtraValue, key string) (string, error) {
	values := extra[key]
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", fmt.Errorf("workload token %s extra must contain exactly one value", key)
	}
	return values[0], nil
}
