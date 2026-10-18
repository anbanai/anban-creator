package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	ProfileStatusDraft     = "draft"
	ProfileStatusConfirmed = "confirmed"

	ProfileApplicationSourceSystemAnalysis   = "system_analysis"
	ProfileApplicationSourceUserConfirmation = "user_confirmation"
	ProfileApplicationSourceUserEdit         = "user_edit"
	ProfileApplicationSourceUserRestore      = "user_restore"

	ProfileInitializationNotStarted = "not_started"
	ProfileInitializationQueued     = "queued"
	ProfileInitializationRunning    = "running"
	ProfileInitializationReady      = "ready"
	ProfileInitializationFailed     = "failed"
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
	SchemaVersion        int                      `json:"schema_version"`
	Status               string                   `json:"status"`
	InitializationStatus string                   `json:"initialization_status"`
	LastError            string                   `json:"last_error,omitempty"`
	Version              int64                    `json:"version"`
	ApplicationSource    string                   `json:"application_source,omitempty"`
	AnalysisTaskID       string                   `json:"analysis_task_id,omitempty"`
	Dimensions           ProjectProfileDimensions `json:"dimensions"`
	AnalysisLimits       []string                 `json:"analysis_limits"`
	FollowUpQuestions    []string                 `json:"follow_up_questions"`
}

// ProjectProfileRevision is the immutable database history for a project
// profile. The current Project.Profile column remains a fast read model; this
// table is the audit trail and source for restore/query operations.
type ProjectProfileRevision struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID       string         `gorm:"type:char(36);index:idx_profile_revision,priority:1;not null" json:"project_id"`
	Revision        int64          `gorm:"index:idx_profile_revision,priority:2;not null" json:"revision"`
	SixDimensions   datatypes.JSON `gorm:"type:json;not null" json:"six_dimensions"`
	ProfileSnapshot datatypes.JSON `gorm:"type:json" json:"profile_snapshot,omitempty"`
	SourceTaskID    string         `gorm:"type:char(36);index" json:"source_task_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ProjectProfileState is the queryable lifecycle read model for the current
// profile. Project.Profile remains the compatibility snapshot of dimensions.
type ProjectProfileState struct {
	ProjectID    string    `gorm:"type:char(36);primaryKey" json:"project_id"`
	Status       string    `gorm:"type:varchar(20);not null" json:"status"`
	Revision     int64     `gorm:"not null;default:0" json:"revision"`
	ActiveTaskID string    `gorm:"type:char(36);index" json:"active_task_id,omitempty"`
	LastError    string    `gorm:"type:text" json:"last_error,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func ProfileDimensions() []string {
	return []string{"identity", "style", "audience", "platforms", "preferences", "memory"}
}

func emptyProfileDimension() ProfileDimension {
	return ProfileDimension{Content: map[string]any{}, Sources: []string{}, Evidence: []string{}, MissingFields: []string{}}
}

func NewProjectProfile() ProjectProfile {
	return ProjectProfile{
		SchemaVersion:        1,
		Status:               ProfileStatusDraft,
		InitializationStatus: ProfileInitializationNotStarted,
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
	"[用户编辑]":  {},
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
	switch p.ApplicationSource {
	case "", ProfileApplicationSourceSystemAnalysis, ProfileApplicationSourceUserConfirmation,
		ProfileApplicationSourceUserEdit, ProfileApplicationSourceUserRestore:
	default:
		return fmt.Errorf("invalid profile application source")
	}
	switch p.InitializationStatus {
	case ProfileInitializationNotStarted, ProfileInitializationQueued, ProfileInitializationRunning,
		ProfileInitializationReady, ProfileInitializationFailed:
	default:
		return fmt.Errorf("invalid profile initialization status")
	}
	dimensions := []struct {
		name  string
		value ProfileDimension
	}{
		{"identity", p.Dimensions.Identity}, {"style", p.Dimensions.Style},
		{"audience", p.Dimensions.Audience}, {"platforms", p.Dimensions.Platforms},
		{"preferences", p.Dimensions.Preferences}, {"memory", p.Dimensions.Memory},
	}
	var validationErrors []error
	for _, item := range dimensions {
		missing := []string{}
		if item.value.Content == nil {
			missing = append(missing, "content")
		}
		if item.value.Sources == nil {
			missing = append(missing, "sources")
		}
		if item.value.Evidence == nil {
			missing = append(missing, "evidence")
		}
		if item.value.MissingFields == nil {
			missing = append(missing, "missing_fields")
		}
		if len(missing) > 0 {
			validationErrors = append(validationErrors, fmt.Errorf("profile dimension %s is incomplete: missing %s", item.name, strings.Join(missing, ", ")))
		}
		for _, source := range item.value.Sources {
			if _, ok := profileSourceTags[strings.TrimSpace(source)]; !ok {
				validationErrors = append(validationErrors, fmt.Errorf("profile dimension %s has invalid source %q", item.name, source))
			}
		}
	}
	if len(validationErrors) > 0 {
		return errors.Join(validationErrors...)
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
