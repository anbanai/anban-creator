package agent

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/anbanai/anban-creator/server/model"
)

func TestLegacyAgentPodLabelSelector(t *testing.T) {
	if got, want := LegacyAgentPodLabelSelector(), "app.kubernetes.io/name=anban-agent"; got != want {
		t.Fatalf("LegacyAgentPodLabelSelector() = %q, want %q", got, want)
	}
}

func TestIsValidatedLegacyAgentPod(t *testing.T) {
	valid := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubernetesLegacyAgentPodName(&modelTaskForLegacyValidation),
			UID:  types.UID("legacy-uid"),
			Labels: map[string]string{
				"app.kubernetes.io/name": "anban-agent",
				kubernetesUserIDLabel:    modelTaskForLegacyValidation.UserID,
				kubernetesProjectIDLabel: modelTaskForLegacyValidation.ProjectID,
			},
			Annotations: map[string]string{kubernetesPodConfigHashAnnotation: "config-hash"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent"}}},
	}

	tests := []struct {
		name   string
		mutate func(*corev1.Pod)
		want   bool
	}{
		{name: "valid legacy pod", want: true},
		{name: "current app label", mutate: func(p *corev1.Pod) { p.Labels["app.kubernetes.io/name"] = "creator-agent" }},
		{name: "spoofed name", mutate: func(p *corev1.Pod) { p.Name = "anban-agent-spoofed" }},
		{name: "missing user label", mutate: func(p *corev1.Pod) { delete(p.Labels, kubernetesUserIDLabel) }},
		{name: "missing project label", mutate: func(p *corev1.Pod) { delete(p.Labels, kubernetesProjectIDLabel) }},
		{name: "missing config hash", mutate: func(p *corev1.Pod) { delete(p.Annotations, kubernetesPodConfigHashAnnotation) }},
		{name: "wrong first container", mutate: func(p *corev1.Pod) { p.Spec.Containers[0].Name = "creator-agent" }},
		{name: "missing containers", mutate: func(p *corev1.Pod) { p.Spec.Containers = nil }},
		{name: "missing UID", mutate: func(p *corev1.Pod) { p.UID = "" }},
		{name: "normalized unsafe IDs rejected", mutate: func(p *corev1.Pod) {
			p.Labels[kubernetesUserIDLabel] = "user-one"
			p.Name = kubernetesLegacyAgentPodName(&model.Task{UserID: "User_One", ProjectID: modelTaskForLegacyValidation.ProjectID})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := valid.DeepCopy()
			if tt.mutate != nil {
				tt.mutate(pod)
			}
			if got := IsValidatedLegacyAgentPod(pod); got != tt.want {
				t.Fatalf("IsValidatedLegacyAgentPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

var modelTaskForLegacyValidation = model.Task{UserID: "user-1", ProjectID: "project-1"}
