package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/anbanai/anban-creator/server/agentpack"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	kubernetesAgentUID                   int64 = 1000
	kubernetesAgentGID                   int64 = 1000
	kubernetesAgentContainerName               = "creator-agent"
	kubernetesWorkspaceInitContainerName       = "workspace-init"
	kubernetesMemoryMountName                  = "memory"
	kubernetesTokenVolumeName                  = "workload-token"
	kubernetesTmpVolumeName                    = "tmp"
	kubernetesServerCAVolumeName               = "server-ca"
	kubernetesServerCAMountPath                = "/var/run/secrets/anban-server-ca"
	kubernetesServerCAFile                     = kubernetesServerCAMountPath + "/ca.crt"
	kubernetesMemoryMountPath                  = "/workspace/.claude/memory"
	kubernetesRuntimeHomePath                  = "/workspace/" + RuntimeHomeDirName
	kubernetesTokenMountPath                   = "/var/run/secrets/anban"
	kubernetesTokenFile                        = kubernetesTokenMountPath + "/token"
	kubernetesTokenAudience                    = "anban-server"
	kubernetesObjectConfigHashLabel            = "anban.ai/config-hash"
)

type kubernetesJobConfig struct {
	srvconfig.KubernetesConfig
	RuntimeImages srvconfig.RuntimeImages
	ServerURL     string
}

func buildKubernetesJob(cfg kubernetesJobConfig, execution *model.TaskExecution, task *model.Task) *batchv1.Job {
	backoffLimit := int32(0)
	activeDeadline := cfg.ActiveDeadlineSeconds
	ttl := cfg.TTLSecondsAfterFinished
	suspend := true
	automountToken := false
	runAsNonRoot := true
	runAsRoot := false
	runAsUser := kubernetesAgentUID
	runAsGroup := kubernetesAgentGID
	rootUser := int64(0)
	rootGroup := int64(0)
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	readOnlyRoot := true
	allowPrivilegeEscalation := false
	tokenExpiration := projectedTokenExpiration(activeDeadline)
	resources := cfg.ResourcesForTask(taskType(task))
	runtimeImage := strings.TrimSpace(execution.RuntimeImage)

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
			Suspend:                 &suspend,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: copyLabels(labels)},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyNever,
					ServiceAccountName:            cfg.ServiceAccount,
					AutomountServiceAccountToken:  &automountToken,
					TerminationGracePeriodSeconds: int64Ptr(int64(cfg.CompletionGraceSeconds)),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:        &runAsNonRoot,
						RunAsUser:           &runAsUser,
						RunAsGroup:          &runAsGroup,
						FSGroup:             &runAsGroup,
						FSGroupChangePolicy: &fsGroupChangePolicy,
						SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					InitContainers: []corev1.Container{{
						Name:                     kubernetesWorkspaceInitContainerName,
						Image:                    runtimeImage,
						ImagePullPolicy:          corev1.PullAlways,
						TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						Command:                  []string{"/bin/sh", "-c"},
						Args:                     []string{kubernetesWorkspaceInitScript(execution.RuntimeAdapter)},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &runAsRoot,
							RunAsUser:                &rootUser,
							RunAsGroup:               &rootGroup,
							ReadOnlyRootFilesystem:   &readOnlyRoot,
							AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
								Add:  []corev1.Capability{"CHOWN", "FOWNER", "DAC_OVERRIDE"},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: kubernetesWorkspaceMountName, MountPath: "/workspace"},
							{Name: kubernetesMemoryMountName, MountPath: kubernetesMemoryMountPath},
						},
					}},
					Containers: []corev1.Container{{
						Name:                     kubernetesAgentContainerName,
						Image:                    runtimeImage,
						ImagePullPolicy:          corev1.PullAlways,
						TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: kubernetesRuntimeHomePath},
							{Name: "SSL_CERT_FILE", Value: kubernetesServerCAFile},
							{Name: "NODE_EXTRA_CA_CERTS", Value: kubernetesServerCAFile},
						},
						Command: []string{"anban"},
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
							Requests: kubernetesResourceList(resources.Requests),
							Limits:   kubernetesResourceList(resources.Limits),
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: kubernetesWorkspaceMountName, MountPath: "/workspace"},
							{Name: kubernetesMemoryMountName, MountPath: kubernetesMemoryMountPath},
							{Name: kubernetesTmpVolumeName, MountPath: "/tmp"},
							{Name: kubernetesTokenVolumeName, MountPath: kubernetesTokenMountPath, ReadOnly: true},
							{Name: kubernetesServerCAVolumeName, MountPath: kubernetesServerCAMountPath, ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: kubernetesWorkspaceMountName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: kubernetesTaskWorkspacePVCName(taskID(task))}}},
						{Name: kubernetesMemoryMountName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: kubernetesProjectMemoryPVCName(projectID(task))}}},
						{Name: kubernetesTmpVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: kubernetesTokenVolumeName, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
							DefaultMode: int32Ptr(0440),
							Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience:          kubernetesTokenAudience,
								ExpirationSeconds: &tokenExpiration,
								Path:              "token",
							}}},
						}}},
						{Name: kubernetesServerCAVolumeName, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
							SecretName:  cfg.ServerCASecret,
							DefaultMode: int32Ptr(corev1.SecretVolumeSourceDefaultMode),
							Items:       []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
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

func kubernetesWorkspaceInitScript(runtimeAdapter string) string {
	lines := []string{
		"set -eu",
		"chown 1000:1000 /workspace",
		"chmod 0770 /workspace",
		"output=/workspace/output",
		`if [ -L "$output" ] || { [ -e "$output" ] && [ ! -d "$output" ]; }; then echo "runtime output must be a real directory" >&2; exit 1; fi`,
		`install -d -m 0750 -o 1000 -g 1000 "$output"`,
		"install -d -m 0700 -o 1000 -g 1000 " + kubernetesRuntimeHomePath,
		"install -d -m 0770 -o 1000 -g 1000 " + kubernetesMemoryMountPath,
	}
	if strings.TrimSpace(runtimeAdapter) != agentpack.AdapterOpenMontage {
		return strings.Join(lines, "\n")
	}
	return strings.Join(append(lines, kubernetesMontageInitScript(
		ContainerMontageTemplatePath,
		"/workspace/"+MontageRuntimeDirName,
		"/workspace/.montage-init",
		"/workspace/output",
	)), "\n")
}

func kubernetesMontageInitScript(templatePath, runtimePath, stagingPath, outputPath string) string {
	return strings.Join([]string{
		"template=" + templatePath,
		"runtime=" + runtimePath,
		"staging=" + stagingPath,
		"output=" + outputPath,
		`if [ ! -e "$runtime" ]; then`,
		`  rm -rf "$staging"`,
		`  mkdir -p "$staging"`,
		`  cp -a "$template/." "$staging/"`,
		`  mv "$staging" "$runtime"`,
		"fi",
		`chown -R 1000:1000 "$runtime"`,
		`chmod -R u+rwX "$runtime"`,
		`if [ -L "$runtime/output" ]; then [ "$(readlink "$runtime/output")" = "$output" ] || { echo "Montage output must link to canonical output" >&2; exit 1; }; elif [ -e "$runtime/output" ]; then echo "Montage output must link to canonical output" >&2; exit 1; else ln -s "$output" "$runtime/output"; fi`,
	}, "\n")
}

func kubernetesResourceList(values map[string]string) corev1.ResourceList {
	if len(values) == 0 {
		return nil
	}
	out := corev1.ResourceList{}
	for name, raw := range values {
		quantity, err := resource.ParseQuantity(raw)
		if err == nil {
			out[corev1.ResourceName(name)] = quantity
		}
	}
	return out
}

func buildProjectMemoryPVC(cfg kubernetesJobConfig, projectID string) *corev1.PersistentVolumeClaim {
	storageClass := cfg.NASStorageClass
	quantity := resource.MustParse(cfg.ProjectMemorySize)
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

func buildTaskWorkspacePVC(cfg kubernetesJobConfig, task *model.Task) *corev1.PersistentVolumeClaim {
	storageClass := cfg.NASStorageClass
	quantity := resource.MustParse(cfg.TaskWorkspaceSize)
	labels := kubernetesAgentLabels(task)
	labels["app.kubernetes.io/component"] = "task-workspace"
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetesTaskWorkspacePVCName(taskID(task)),
			Namespace: cfg.Namespace,
			Labels:    labels,
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

func taskID(task *model.Task) string {
	if task == nil {
		return ""
	}
	return task.ID
}

func taskType(task *model.Task) string {
	if task == nil {
		return ""
	}
	return task.Type
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
