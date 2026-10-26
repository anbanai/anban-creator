package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
)

// KubernetesTaskTypes lists the persisted task types registered for managed Jobs.
// Shared content_generation kinds persist their channel; video_edit persists video-edit.
func KubernetesTaskTypes() []string {
	types := make(map[string]struct{})
	for _, pack := range agentpack.Default().Packs {
		if pack.Kind != agentpack.KindManaged || pack.Channel == "" || (pack.Interaction != nil && pack.Interaction.UserQuestions) {
			continue
		}
		for _, kind := range pack.Bindings.TaskKinds {
			taskType := kind
			if kind == model.TaskKindContentGeneration {
				taskType = pack.Channel
			} else if pack.ID == model.AgentIDVideoEdit && kind == model.TaskKindVideoEdit {
				taskType = model.PlatformVideoEdit
			} else {
				taskType = model.TaskTypeForTaskKind(kind)
			}
			types[taskType] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(types))
}

// ResourcesForTask requires an exact, complete profile and returns an independent copy.
func (c KubernetesConfig) ResourcesForTask(taskType string) (KubernetesResourceConfig, error) {
	if !slices.Contains(KubernetesTaskTypes(), taskType) {
		return KubernetesResourceConfig{}, fmt.Errorf("claude.kubernetes.resource_profiles contains unsupported task type %q", taskType)
	}
	path := "claude.kubernetes.resource_profiles." + taskType
	profile, ok := c.ResourceProfiles[taskType]
	if !ok {
		return KubernetesResourceConfig{}, fmt.Errorf("%s is required", path)
	}
	errs := validateKubernetesResourceConfig(path, profile)
	for _, group := range []struct {
		name   string
		values map[string]string
	}{{"requests", profile.Requests}, {"limits", profile.Limits}} {
		for _, name := range []string{"cpu", "memory"} {
			if strings.TrimSpace(group.values[name]) == "" {
				errs = append(errs, path+"."+group.name+"."+name+" is required")
			}
		}
	}
	if len(errs) > 0 {
		return KubernetesResourceConfig{}, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return KubernetesResourceConfig{Requests: maps.Clone(profile.Requests), Limits: maps.Clone(profile.Limits)}, nil
}

// ValidateResourceProfiles rejects missing, obsolete and incomplete task profiles.
func (c KubernetesConfig) ValidateResourceProfiles() error {
	var errs []string
	for _, taskType := range KubernetesTaskTypes() {
		if _, err := c.ResourcesForTask(taskType); err != nil {
			errs = append(errs, err.Error())
		}
	}
	for _, taskType := range slices.Sorted(maps.Keys(c.ResourceProfiles)) {
		if !slices.Contains(KubernetesTaskTypes(), taskType) {
			errs = append(errs, fmt.Sprintf("claude.kubernetes.resource_profiles contains unsupported task type %q", taskType))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
