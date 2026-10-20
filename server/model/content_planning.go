package model

import "time"

// PlanningArtifactRef names one immutable delivered object. No path-only lookup
// or current-execution fallback is permitted when consuming an accepted stage.
type PlanningArtifactRef struct {
	TaskID        string `json:"task_id"`
	ExecutionID   string `json:"execution_id"`
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
}
type PlanningCandidate struct {
	AssetID    string   `json:"asset_id,omitempty"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	SourceRefs []string `json:"source_refs,omitempty"`
}
type PlanningTargetRequirements struct {
	Platform        string  `json:"platform"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	AspectRatio     string  `json:"aspect_ratio,omitempty"`
}
type ContentPlanningInput struct {
	InternalSources    []PlanningAnalyticsRef      `json:"internal_sources,omitempty"`
	InternalEvidence   []PlanningInternalEvidence  `json:"internal_evidence,omitempty"`
	SchemaVersion      string                      `json:"schema_version"`
	TaskKind           string                      `json:"task_kind"`
	Candidates         []PlanningCandidate         `json:"candidates"`
	AcceptedArtifacts  []PlanningArtifactRef       `json:"accepted_artifacts"`
	TargetRequirements *PlanningTargetRequirements `json:"target_requirements,omitempty"`
}
type PlanningIdentity struct {
	SchemaVersion string   `json:"schema_version"`
	TaskID        string   `json:"task_id"`
	ExecutionID   string   `json:"execution_id"`
	TaskKind      string   `json:"task_kind"`
	Status        string   `json:"status"`
	Missing       []string `json:"missing"`
}
type PlanningSource struct {
	InternalReference *PlanningAnalyticsRef `json:"internal_reference,omitempty"`
	ID                string                `json:"id"`
	OperationID       string                `json:"operation_id"`
	URL               string                `json:"url"`
	PublishedAt       string                `json:"published_at"`
	FetchedAt         string                `json:"fetched_at"`
	Facts             []string              `json:"facts"`
}
type PlanningResearchEvidence struct {
	PlanningIdentity
	Sources []PlanningSource `json:"sources"`
}
type PlanningAssessment struct {
	Score      float64  `json:"score"`
	Rationale  string   `json:"rationale"`
	SourceRefs []string `json:"source_refs"`
}
type PlanningProposal struct {
	ID              string             `json:"id"`
	Title           string             `json:"title"`
	SemanticKernel  string             `json:"semantic_kernel"`
	SourceRefs      []string           `json:"source_refs"`
	MergeAssetIDs   []string           `json:"merge_asset_ids"`
	TargetFit       PlanningAssessment `json:"target_fit"`
	Transferability PlanningAssessment `json:"transferability"`
	Angles          []string           `json:"angles"`
	ProductionCost  string             `json:"production_cost"`
	MaterialRisk    string             `json:"material_risk"`
	ComplianceRisk  string             `json:"compliance_risk"`
}
type PlanningResearchResult struct {
	PlanningIdentity
	Candidates []PlanningProposal `json:"candidates"`
}
type PlanningCheck struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Rationale  string   `json:"rationale"`
	SourceRefs []string `json:"source_refs"`
}
type PlanningReview struct {
	PlanningIdentity
	ValidationKind string          `json:"validation_kind"`
	ContentSHA256  string          `json:"content_sha256"`
	Checks         []PlanningCheck `json:"checks"`
	SourceRefs     []string        `json:"source_refs"`
}
type PlanningScene struct {
	Index                int      `json:"index"`
	StartSeconds         float64  `json:"start_seconds"`
	EndSeconds           float64  `json:"end_seconds"`
	SpokenText           string   `json:"spoken_text"`
	Visuals              string   `json:"visuals"`
	MaterialRequirements []string `json:"material_requirements"`
	SourceRefs           []string `json:"source_refs"`
}
type PlanningVideoScript struct {
	PlanningIdentity
	Platform   string          `json:"platform"`
	SpokenText string          `json:"spoken_text"`
	Scenes     []PlanningScene `json:"scenes"`
	SourceRefs []string        `json:"source_refs"`
}

// PlanningAnalyticsRef freezes a reference to one independently collected
// analytics revision. It does not convert semantic judgments into metrics.
type PlanningAnalyticsRef struct {
	ProjectID         string `json:"project_id"`
	ContentID         string `json:"content_id"`
	AnalyticsRevision int64  `json:"analytics_revision"`
}
type PlanningInternalEvidence struct {
	Reference PlanningAnalyticsRef `json:"reference"`
	Content   AnalyticsContent     `json:"content"`
	DataAt    time.Time            `json:"data_at"`
}
