package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	KubernetesWorkloadAudience = "anban-server"
	KubernetesPodNameExtra     = "authentication.kubernetes.io/pod-name"
	KubernetesPodUIDExtra      = "authentication.kubernetes.io/pod-uid"
)

// KubernetesWorkloadIdentity is the verified Kubernetes object ownership chain.
type KubernetesWorkloadIdentity struct {
	Namespace      string
	ServiceAccount string
	PodName        string
	PodUID         string
	JobName        string
	JobUID         string
	ExecutionID    string
	TaskID         string
	ProjectID      string
	UserID         string
	JobDeadline    time.Time
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

func (v *KubernetesWorkloadVerifier) Verify(ctx context.Context, token, requestedExecutionID string) (*KubernetesWorkloadIdentity, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(requestedExecutionID) == "" {
		return nil, errors.New("workload token and execution ID are required")
	}
	review, err := v.kube.AuthenticationV1().TokenReviews().Create(ctx, &authv1.TokenReview{Spec: authv1.TokenReviewSpec{Token: token, Audiences: []string{KubernetesWorkloadAudience}}}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("review workload token: %w", err)
	}
	expectedUser := "system:serviceaccount:" + v.namespace + ":" + v.serviceAccount
	if !review.Status.Authenticated || review.Status.User.Username != expectedUser {
		return nil, errors.New("workload token is not authenticated as the configured service account")
	}
	if !slices.Contains(review.Status.Audiences, KubernetesWorkloadAudience) {
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
	if string(pod.UID) != podUID {
		return nil, errors.New("bound Pod UID mismatch")
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
	if string(job.UID) != ownerUID {
		return nil, errors.New("owning Job UID mismatch")
	}
	if job.Status.StartTime == nil || job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds <= 0 {
		return nil, errors.New("owning Job has no active deadline identity")
	}
	jobDeadline := job.Status.StartTime.Add(time.Duration(*job.Spec.ActiveDeadlineSeconds) * time.Second)
	if !jobDeadline.After(time.Now()) {
		return nil, errors.New("owning Job active deadline has expired")
	}
	labels := job.Labels
	if labels["app.kubernetes.io/name"] != kubernetesAgentAppName || labels["app.kubernetes.io/component"] != "execution" ||
		labels[kubernetesExecutionIDLabel] != kubernetesLabelValue(requestedExecutionID) || labels[kubernetesTaskIDLabel] == "" || labels[kubernetesProjectIDLabel] == "" || labels[kubernetesUserIDLabel] == "" {
		return nil, errors.New("owning Job runtime identity labels mismatch")
	}
	return &KubernetesWorkloadIdentity{Namespace: v.namespace, ServiceAccount: v.serviceAccount, PodName: podName, PodUID: podUID, JobName: job.Name, JobUID: string(job.UID), ExecutionID: requestedExecutionID, TaskID: labels[kubernetesTaskIDLabel], ProjectID: labels[kubernetesProjectIDLabel], UserID: labels[kubernetesUserIDLabel], JobDeadline: jobDeadline}, nil
}

func singleExtra(extra map[string]authv1.ExtraValue, key string) (string, error) {
	values := extra[key]
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", fmt.Errorf("workload token %s extra must contain exactly one value", key)
	}
	return values[0], nil
}
