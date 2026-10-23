package agent

import (
	"github.com/anbanai/anban-creator/server/model"
	"testing"
	"time"
)

func TestHypitKubernetesPlacementMatchesRuntimeArchitecture(t *testing.T) {
	for _, kind := range []string{model.PlatformHypit, model.PlatformWechat, model.PlatformMontage} {
		t.Run(kind, func(t *testing.T) {
			task := testTask()
			task.Type = kind
			job := buildKubernetesJob(testJobConfig(), testExecution(), task)
			selector := job.Spec.Template.Spec.NodeSelector
			if kind == model.PlatformHypit {
				if len(selector) != 2 || selector["kubernetes.io/os"] != "linux" || selector["kubernetes.io/arch"] != "amd64" {
					t.Fatalf("Hypit placement=%v", selector)
				}
			} else if len(selector) != 0 {
				t.Fatalf("unrelated runtime placement changed: %v", selector)
			}
		})
	}
}

func TestHypitWorkloadHonorsFrozenTimeout(t *testing.T) {
	task := testTask()
	task.Type = model.PlatformHypit
	task.HypitRuntimeSnapshot = []byte(`{"limits":{"timeout_minutes":7}}`)
	job := buildKubernetesJob(testJobConfig(), testExecution(), task)
	if *job.Spec.ActiveDeadlineSeconds != 420 {
		t.Fatalf("deadline=%d", *job.Spec.ActiveDeadlineSeconds)
	}
	cfg := dockerRuntimeConfig{DockerConfig: dockerDispatcherTestConfig()}
	runtime := buildDockerRuntimeSpec(cfg, testExecution(), task)
	if runtime.Timeout != 7*time.Minute {
		t.Fatalf("Docker timeout=%v", runtime.Timeout)
	}
}

func TestVideoEditWorkloadsHonorExecutionDeadline(t *testing.T) {
	task := testTask()
	task.Type = model.PlatformVideoEdit
	execution := testExecution()
	execution.ActiveDeadlineSeconds = int64((90 * time.Minute).Seconds())

	job := buildKubernetesJob(testJobConfig(), execution, task)
	if got := *job.Spec.ActiveDeadlineSeconds; got != 90*60 {
		t.Fatalf("Kubernetes deadline = %d seconds, want %d", got, 90*60)
	}

	cfg := dockerRuntimeConfig{DockerConfig: dockerDispatcherTestConfig()}
	runtime := buildDockerRuntimeSpec(cfg, execution, task)
	if runtime.Timeout != 90*time.Minute {
		t.Fatalf("Docker timeout = %v, want 90m", runtime.Timeout)
	}
}
