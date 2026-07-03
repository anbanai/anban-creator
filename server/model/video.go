package model

// VideoDefaults stores reusable video generation defaults on a project. Plans
// and tasks copy these values into their own snapshots when they override them.
type VideoDefaults struct {
	Purpose    string `json:"purpose,omitempty"`
	ModelKey   string `json:"model_key,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Ratio      string `json:"ratio,omitempty"`
	Duration   int64  `json:"duration,omitempty"`
	Watermark  *bool  `json:"watermark,omitempty"`
	Preflight  bool   `json:"preflight,omitempty"`
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
	Purpose          string                 `json:"purpose,omitempty"`
	ModelKey         string                 `json:"model_key,omitempty"`
	Model            string                 `json:"model,omitempty"`
	Resolution       string                 `json:"resolution,omitempty"`
	Ratio            string                 `json:"ratio,omitempty"`
	Duration         int64                  `json:"duration,omitempty"`
	Watermark        *bool                  `json:"watermark,omitempty"`
	Preflight        bool                   `json:"preflight,omitempty"`
	References       []VideoReferenceAsset  `json:"references,omitempty"`
	EstimatedCredits int                    `json:"estimated_credits,omitempty"`
	PricingBreakdown *VideoPricingBreakdown `json:"pricing_breakdown,omitempty"`
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
	CNY              float64 `json:"cny"`
	CreditMultiplier int     `json:"credit_multiplier"`
	InputVideo       bool    `json:"input_video"`
	InputSeconds     float64 `json:"input_seconds,omitempty"`
	OutputSeconds    int64   `json:"output_seconds"`
	Resolution       string  `json:"resolution"`
	Ratio            string  `json:"ratio"`
	ModelKey         string  `json:"model_key"`
}
