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
	EstimatedCredits int                    `json:"estimated_credits,omitempty"`
	PricingBreakdown *VideoPricingBreakdown `json:"pricing_breakdown,omitempty"`
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
