package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProfileStatusDraft     = "draft"
	ProfileStatusConfirmed = "confirmed"
)

// ProfileDimension is intentionally map-shaped: Easel's six files are semantic
// documents whose fields evolve independently. Sources/evidence remain attached
// to each dimension so inferred values cannot be mistaken for user facts.
type ProfileDimension struct {
	Content       map[string]any `json:"content"`
	Sources       []string       `json:"sources"`
	Evidence      []string       `json:"evidence"`
	MissingFields []string       `json:"missing_fields"`
}

type ProjectProfileDimensions struct {
	Identity    ProfileDimension `json:"identity"`
	Style       ProfileDimension `json:"style"`
	Audience    ProfileDimension `json:"audience"`
	Platforms   ProfileDimension `json:"platforms"`
	Preferences ProfileDimension `json:"preferences"`
	Memory      ProfileDimension `json:"memory"`
}

type ProjectProfile struct {
	SchemaVersion     int                      `json:"schema_version"`
	Status            string                   `json:"status"`
	Version           int64                    `json:"version"`
	AnalysisTaskID    string                   `json:"analysis_task_id,omitempty"`
	Dimensions        ProjectProfileDimensions `json:"dimensions"`
	AnalysisLimits    []string                 `json:"analysis_limits"`
	FollowUpQuestions []string                 `json:"follow_up_questions"`
}

func ProfileDimensions() []string {
	return []string{"identity", "style", "audience", "platforms", "preferences", "memory"}
}

func emptyProfileDimension() ProfileDimension {
	return ProfileDimension{Content: map[string]any{}, Sources: []string{}, Evidence: []string{}, MissingFields: []string{}}
}

func NewProjectProfile() ProjectProfile {
	return ProjectProfile{
		SchemaVersion: 1,
		Status:        ProfileStatusDraft,
		Dimensions: ProjectProfileDimensions{
			Identity: emptyProfileDimension(), Style: emptyProfileDimension(), Audience: emptyProfileDimension(),
			Platforms: emptyProfileDimension(), Preferences: emptyProfileDimension(), Memory: emptyProfileDimension(),
		},
		AnalysisLimits: []string{}, FollowUpQuestions: []string{},
	}
}

func (p ProjectProfile) IsConfirmed() bool {
	return p.Status == ProfileStatusConfirmed && p.Version > 0
}

var profileSourceTags = map[string]struct{}{
	"[用户确认]":  {},
	"[链接分析]":  {},
	"[推断待确认]": {},
	"[待补充]":   {},
}

// Validate enforces the stable wire contract shared by the Agent draft and
// the project-owned confirmed profile. Content stays map-shaped so each Easel
// dimension can evolve independently without changing the transport schema.
func (p ProjectProfile) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported profile schema version")
	}
	if p.Status != ProfileStatusDraft && p.Status != ProfileStatusConfirmed {
		return fmt.Errorf("invalid profile status")
	}
	for name, dimension := range map[string]ProfileDimension{
		"identity": p.Dimensions.Identity, "style": p.Dimensions.Style,
		"audience": p.Dimensions.Audience, "platforms": p.Dimensions.Platforms,
		"preferences": p.Dimensions.Preferences, "memory": p.Dimensions.Memory,
	} {
		if dimension.Content == nil || dimension.Sources == nil || dimension.Evidence == nil || dimension.MissingFields == nil {
			return fmt.Errorf("profile dimension %s is incomplete", name)
		}
		for _, source := range dimension.Sources {
			if _, ok := profileSourceTags[strings.TrimSpace(source)]; !ok {
				return fmt.Errorf("profile dimension %s has invalid source", name)
			}
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	if len(encoded) > 2<<20 {
		return fmt.Errorf("profile is too large")
	}
	return nil
}
