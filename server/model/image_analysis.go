package model

import "time"

const (
	ImageAnalysisKindProjectVisualStyle = "project_visual_style"
	ImageAnalysisKindTemplatePrompt     = "template_prompt"

	ImageAnalysisSubjectProject  = "project"
	ImageAnalysisSubjectTemplate = "template"

	ImageAnalysisStatusQueued     = "queued"
	ImageAnalysisStatusRunning    = "running"
	ImageAnalysisStatusSucceeded  = "succeeded"
	ImageAnalysisStatusFailed     = "failed"
	ImageAnalysisStatusCancelled  = "cancelled"
	ImageAnalysisStatusSuperseded = "superseded"

	ImageAnalysisSourceManual   = "manual"
	ImageAnalysisSourceAnalysis = "analysis"

	TemplateReadinessAnalyzing = "analyzing"
	TemplateReadinessReady     = "ready"
	TemplateReadinessFailed    = "failed"
)

// ImageAnalysisJob is the durable, current analysis state for one subject and kind.
// Generation invalidates workers created before a retry, cancellation, or source change.
type ImageAnalysisJob struct {
	ID             string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID         string     `gorm:"type:char(36);index;not null" json:"-"`
	Kind           string     `gorm:"type:varchar(40);uniqueIndex:idx_image_analysis_subject,priority:3;not null" json:"kind"`
	SubjectType    string     `gorm:"type:varchar(20);uniqueIndex:idx_image_analysis_subject,priority:1;not null" json:"-"`
	SubjectID      string     `gorm:"type:char(36);uniqueIndex:idx_image_analysis_subject,priority:2;index;not null" json:"-"`
	SourceAssetID  string     `gorm:"type:char(36);index;not null" json:"-"`
	Generation     int64      `gorm:"not null;default:1" json:"-"`
	Status         string     `gorm:"type:varchar(20);index;not null" json:"status"`
	AttemptCount   int        `gorm:"not null;default:0" json:"attempt_count"`
	Result         string     `gorm:"type:text" json:"-"`
	PreviousResult string     `gorm:"type:text" json:"-"`
	ErrorCode      string     `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	ErrorMessage   string     `gorm:"type:varchar(500)" json:"error_message,omitempty"`
	LeaseExpiresAt *time.Time `gorm:"index" json:"-"`
	EnqueuedAt     *time.Time `gorm:"index" json:"-"`
	StartedAt      *time.Time `json:"-"`
	CompletedAt    *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"-"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (ImageAnalysisJob) TableName() string { return "image_analysis_jobs" }

type ImageAnalysisView struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Status       string    `json:"status"`
	AttemptCount int       `json:"attempt_count"`
	ErrorCode    string    `json:"error_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CanRetry     bool      `json:"can_retry"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (j *ImageAnalysisJob) View() *ImageAnalysisView {
	if j == nil {
		return nil
	}
	return &ImageAnalysisView{
		ID: j.ID, Kind: j.Kind, Status: j.Status, AttemptCount: j.AttemptCount,
		ErrorCode: j.ErrorCode, ErrorMessage: j.ErrorMessage,
		CanRetry:  j.Status == ImageAnalysisStatusFailed || j.Status == ImageAnalysisStatusCancelled,
		UpdatedAt: j.UpdatedAt,
	}
}
