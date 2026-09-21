package agent

import (
	"github.com/anbanai/anban-creator/server/model"
	"testing"
	"time"
)

func TestHypitKubernetesPlacementMatchesRuntimeArchitecture(t *testing.T) {
	for _, kind := range []string{model.PlatformHypit, model.PlatformArticle, model.PlatformMontage} {
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
