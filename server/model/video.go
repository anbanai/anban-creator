package model

const (
	VideoWorkflowCreator = "creator"
	VideoWorkflowEditor  = "editor"
)

// VideoDefaults stores reusable video generation defaults on a project. Plans
// and tasks copy these values into their own snapshots when they override them.
type VideoDefaults struct {
	Purpose        string `json:"purpose,omitempty"`
	CreativeType   string `json:"creative_type,omitempty"`
	SubjectProfile string `json:"subject_profile,omitempty"`
	Audience       string `json:"audience,omitempty"`
	SingleMessage  string `json:"single_message,omitempty"`
	ModelKey       string `json:"model_key,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	Ratio          string `json:"ratio,omitempty"`
	Duration       int64  `json:"duration,omitempty"`
	Watermark      *bool  `json:"watermark,omitempty"`
	Preflight      bool   `json:"preflight,omitempty"`
}

// VideoModelPolicy constrains which video model/parameter combinations a
// project is allowed to use.
type VideoModelPolicy struct {
	AllowedModels      []string `json:"allowed_models,omitempty"`
	DefaultModel       string   `json:"default_model,omitempty"`
	AllowAutoDowngrade bool     `json:"allow_auto_downgrade,omitempty"`
	MaxResolution      string   `json:"max_resolution,omitempty"`
	MaxDuration        int64    `json:"max_duration,omitempty"`
}

// VideoTaskConfig is the immutable plan/task snapshot used by agents and Studio.
type VideoTaskConfig struct {
	Workflow                  string                   `json:"workflow,omitempty"`
	Purpose                   string                   `json:"purpose,omitempty"`
	CreativeType              string                   `json:"creative_type,omitempty"`
	SubjectProfile            string                   `json:"subject_profile,omitempty"`
	Audience                  string                   `json:"audience,omitempty"`
	SingleMessage             string                   `json:"single_message,omitempty"`
	ModelKey                  string                   `json:"model_key,omitempty"`
	Model                     string                   `json:"model,omitempty"`
	Resolution                string                   `json:"resolution,omitempty"`
	Ratio                     string                   `json:"ratio,omitempty"`
	Duration                  int64                    `json:"duration,omitempty"`
	TargetDurationSeconds     int64                    `json:"target_duration_seconds,omitempty"`
	TargetDurationSource      string                   `json:"target_duration_source,omitempty"`
	TargetDurationReason      string                   `json:"target_duration_reason,omitempty"`
	SegmentMaxDurationSeconds int64                    `json:"segment_max_duration_seconds,omitempty"`
	SegmentMinDurationSeconds int64                    `json:"segment_min_duration_seconds,omitempty"`
	Segments                  []VideoTaskSegmentConfig `json:"segments,omitempty"`
	Watermark                 *bool                    `json:"watermark,omitempty"`
	Preflight                 bool                     `json:"preflight,omitempty"`
	References                []VideoReferenceAsset    `json:"references,omitempty"`
	EstimatedCredits          int                      `json:"estimated_credits,omitempty"`
	PricingBreakdown          *VideoPricingBreakdown   `json:"pricing_breakdown,omitempty"`
}

type VideoTaskSegmentConfig struct {
	Index            int    `json:"index"`
	StartSecond      int64  `json:"start_second"`
	EndSecond        int64  `json:"end_second"`
	Duration         int64  `json:"duration"`
	Prompt           string `json:"prompt,omitempty"`
	ModelKey         string `json:"model_key,omitempty"`
	Model            string `json:"model,omitempty"`
	Resolution       string `json:"resolution,omitempty"`
	Ratio            string `json:"ratio,omitempty"`
	EstimatedCredits int    `json:"estimated_credits,omitempty"`
}

func NormalizeVideoWorkflow(workflow string) string {
	switch workflow {
	case VideoWorkflowEditor:
		return VideoWorkflowEditor
	default:
		return VideoWorkflowCreator
	}
}

// VideoReferenceAsset is a Studio/API-facing reference saved on video tasks and
// plans. Agents map it into provider-facing references via MCP so only
// server-owned or public assets reach the video model.
type VideoReferenceAsset struct {
	Type                 string  `json:"type"`
	URL                  string  `json:"url,omitempty"`
	Text                 string  `json:"text,omitempty"`
	ReferenceRole        string  `json:"reference_role,omitempty"`
	FileName             string  `json:"file_name,omitempty"`
	MimeType             string  `json:"mime_type,omitempty"`
	FileSize             int64   `json:"file_size,omitempty"`
	InputDurationSeconds float64 `json:"input_duration_seconds,omitempty"`
}

// VideoPricingBreakdown records the resolved official-price estimate used for
// task billing.
type VideoPricingBreakdown struct {
	CNY              float64                        `json:"cny"`
	CreditMultiplier int                            `json:"credit_multiplier"`
	CreditsPerCNY    int                            `json:"credits_per_cny"`
	InputVideo       bool                           `json:"input_video"`
	InputSeconds     float64                        `json:"input_seconds,omitempty"`
	OutputSeconds    int64                          `json:"output_seconds"`
	SegmentCount     int                            `json:"segment_count,omitempty"`
	Resolution       string                         `json:"resolution"`
	Ratio            string                         `json:"ratio"`
	ModelKey         string                         `json:"model_key"`
	Segments         []VideoPricingSegmentBreakdown `json:"segments,omitempty"`
}

type VideoPricingSegmentBreakdown struct {
	Index   int     `json:"index"`
	Seconds int64   `json:"seconds"`
	CNY     float64 `json:"cny"`
	Credits int     `json:"credits"`
}
