package agent

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/anbanai/anban-creator/server/model"
)

const legacyAgentPodLabelSelector = "app.kubernetes.io/name=anban-agent"

// LegacyAgentPodLabelSelector returns the exact selector for the retired pod identity.
func LegacyAgentPodLabelSelector() string {
	return legacyAgentPodLabelSelector
}

// IsValidatedLegacyAgentPod rejects pods that cannot be tied to the old
// deterministic identity using their immutable Kubernetes UID and legacy metadata.
func IsValidatedLegacyAgentPod(pod *corev1.Pod) bool {
	if pod == nil || pod.UID == "" {
		return false
	}
	if pod.Labels["app.kubernetes.io/name"] != kubernetesLegacyAgentPrefix {
		return false
	}
	userID := strings.TrimSpace(pod.Labels[kubernetesUserIDLabel])
	projectID := strings.TrimSpace(pod.Labels[kubernetesProjectIDLabel])
	if userID == "" || projectID == "" {
		return false
	}
	if pod.Name != kubernetesLegacyAgentPodName(&model.Task{UserID: userID, ProjectID: projectID}) {
		return false
	}
	if strings.TrimSpace(pod.Annotations[kubernetesPodConfigHashAnnotation]) == "" {
		return false
	}
	return len(pod.Spec.Containers) > 0 && pod.Spec.Containers[0].Name == "agent"
}
