package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	kubernetesAgentUID              int64 = 1000
	kubernetesAgentGID              int64 = 1000
	kubernetesMemoryMountName             = "memory"
	kubernetesTokenVolumeName             = "workload-token"
	kubernetesTmpVolumeName               = "tmp"
	kubernetesHomeVolumeName              = "home"
	kubernetesMemoryMountPath             = "/workspace/.claude/memory"
	kubernetesTokenMountPath              = "/var/run/secrets/anban"
	kubernetesTokenFile                   = kubernetesTokenMountPath + "/token"
	kubernetesTokenAudience               = "anban-server"
	kubernetesObjectConfigHashLabel       = "anban.ai/config-hash"
)

type kubernetesJobConfig struct {
	srvconfig.KubernetesConfig
	ServerURL string
}

func buildKubernetesJob(cfg kubernetesJobConfig, execution *model.TaskExecution, task *model.Task) *batchv1.Job {
	backoffLimit := int32(0)
	activeDeadline := cfg.ActiveDeadlineSeconds
	ttl := cfg.TTLSecondsAfterFinished
	automountToken := false
	runAsNonRoot := true
	runAsUser := kubernetesAgentUID
	runAsGroup := kubernetesAgentGID
	readOnlyRoot := true
	allowPrivilegeEscalation := false
	tokenExpiration := projectedTokenExpiration(activeDeadline)

	labels := kubernetesExecutionLabels(execution, task)
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetesJobName(executionID(execution)),
			Namespace: cfg.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: copyLabels(labels)},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyNever,
					ServiceAccountName:            cfg.ServiceAccount,
					AutomountServiceAccountToken:  &automountToken,
					TerminationGracePeriodSeconds: int64Ptr(int64(cfg.CompletionGraceSeconds)),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   &runAsNonRoot,
						RunAsUser:      &runAsUser,
						RunAsGroup:     &runAsGroup,
						FSGroup:        &runAsGroup,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            kubernetesAgentContainerName,
						Image:           cfg.AgentImage,
						ImagePullPolicy: corev1.PullAlways,
						Env:             []corev1.EnvVar{{Name: "HOME", Value: ContainerHomePath}},
						Command:         []string{"anban"},
						Args: []string{
							"job",
							"--server-url", strings.TrimRight(cfg.ServerURL, "/"),
							"--execution-id", executionID(execution),
							"--workspace", "/workspace",
							"--workload-token-file", kubernetesTokenFile,
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &runAsNonRoot,
							RunAsUser:                &runAsUser,
							RunAsGroup:               &runAsGroup,
							ReadOnlyRootFilesystem:   &readOnlyRoot,
							AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						Resources: corev1.ResourceRequirements{
							Requests: kubernetesResourceList(cfg.Resources.Requests),
							Limits:   kubernetesResourceList(cfg.Resources.Limits),
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: kubernetesWorkspaceMountName, MountPath: "/workspace"},
							{Name: kubernetesMemoryMountName, MountPath: kubernetesMemoryMountPath},
							{Name: kubernetesTmpVolumeName, MountPath: "/tmp"},
							{Name: kubernetesHomeVolumeName, MountPath: "/home/node"},
							{Name: kubernetesTokenVolumeName, MountPath: kubernetesTokenMountPath, ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: kubernetesWorkspaceMountName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: kubernetesMemoryMountName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: kubernetesProjectMemoryPVCName(projectID(task))}}},
						{Name: kubernetesTmpVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: kubernetesHomeVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: kubernetesTokenVolumeName, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
							DefaultMode: int32Ptr(0440),
							Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience:          kubernetesTokenAudience,
								ExpirationSeconds: &tokenExpiration,
								Path:              "token",
							}}},
						}}},
					},
				},
			},
		},
	}
	if strings.TrimSpace(cfg.ImagePullSecret) != "" {
		job.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: cfg.ImagePullSecret}}
	}
	job.Annotations = map[string]string{kubernetesObjectConfigHashLabel: kubernetesObjectHash(job.Spec)}
	return job
}

func buildProjectMemoryPVC(cfg kubernetesJobConfig, projectID string) *corev1.PersistentVolumeClaim {
	storageClass := cfg.MemoryStorageClass
	quantity := resource.MustParse(cfg.MemorySize)
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetesProjectMemoryPVCName(projectID),
			Namespace: cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      kubernetesAgentAppName,
				"app.kubernetes.io/component": "project-memory",
				kubernetesProjectIDLabel:      kubernetesLabelValue(projectID),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: quantity,
			}},
		},
	}
}

func kubernetesExecutionLabels(execution *model.TaskExecution, task *model.Task) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":      kubernetesAgentAppName,
		"app.kubernetes.io/component": "execution",
		kubernetesExecutionIDLabel:    kubernetesLabelValue(executionID(execution)),
	}
	if task != nil {
		labels[kubernetesTaskIDLabel] = kubernetesLabelValue(task.ID)
		labels[kubernetesProjectIDLabel] = kubernetesLabelValue(task.ProjectID)
		labels[kubernetesUserIDLabel] = kubernetesLabelValue(task.UserID)
	} else if execution != nil {
		labels[kubernetesTaskIDLabel] = kubernetesLabelValue(execution.TaskID)
	}
	return labels
}

func projectedTokenExpiration(deadline int64) int64 {
	if deadline < 600 {
		return 600
	}
	if deadline > 3600 {
		return 3600
	}
	return deadline
}

func executionID(execution *model.TaskExecution) string {
	if execution == nil {
		return ""
	}
	return execution.ID
}

func projectID(task *model.Task) string {
	if task == nil {
		return ""
	}
	return task.ProjectID
}

func kubernetesObjectHash(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal Kubernetes object hash: %v", err))
	}
	return kubernetesHashSuffix(string(raw))
}

func copyLabels(labels map[string]string) map[string]string {
	copy := make(map[string]string, len(labels))
	for key, value := range labels {
		copy[key] = value
	}
	return copy
}

func int32Ptr(value int32) *int32 { return &value }
func int64Ptr(value int64) *int64 { return &value }
