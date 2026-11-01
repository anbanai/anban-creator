package config

import (
	"os"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gopkg.in/yaml.v3"
)

// Kubernetes task dispatch has no fallback resource profile, so every managed
// Pack task type must be present in the example that operators deploy from.
func TestConfigExampleIncludesTopicRadarKubernetesProfiles(t *testing.T) {
	raw, err := os.ReadFile("../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Claude struct {
			Kubernetes struct {
				ResourceProfiles map[string]KubernetesResourceConfig `yaml:"resource_profiles"`
			} `yaml:"kubernetes"`
		} `yaml:"claude"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	cfg := KubernetesConfig{ResourceProfiles: document.Claude.Kubernetes.ResourceProfiles}
	for _, taskType := range []string{model.TaskTypeTopicDigest, model.TaskTypeTopicScript} {
		profile, err := cfg.ResourcesForTask(taskType)
		if err != nil {
			t.Fatalf("config.example.yaml: %v", err)
		}
		if len(profile.Requests) == 0 || len(profile.Limits) == 0 {
			t.Fatalf("config.example.yaml: resource profile %q is empty", taskType)
		}
	}
}
