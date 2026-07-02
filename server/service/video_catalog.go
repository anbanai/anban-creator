package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type VideoModelCatalog map[string]VideoModelSpec

type VideoModelSpec struct {
	Key                   string             `json:"key" yaml:"key"`
	DisplayName           string             `json:"display_name" yaml:"display_name"`
	ModelID               string             `json:"model_id" yaml:"model_id"`
	SupportedResolutions  []string           `json:"supported_resolutions" yaml:"supported_resolutions"`
	SupportedRatios       []string           `json:"supported_ratios" yaml:"supported_ratios"`
	MinDuration           int64              `json:"min_duration" yaml:"min_duration"`
	MaxDuration           int64              `json:"max_duration" yaml:"max_duration"`
	SupportsVideoInput    bool               `json:"supports_video_input" yaml:"supports_video_input"`
	Supports4K            bool               `json:"supports_4k" yaml:"supports_4k"`
	NoInputPricePerSecond map[string]float64 `json:"no_input_price_per_second" yaml:"no_input_price_per_second"`
	VideoInput5sMinPrice  map[string]float64 `json:"video_input_5s_min_price" yaml:"video_input_5s_min_price"`
	VideoInput5sMaxPrice  map[string]float64 `json:"video_input_5s_max_price" yaml:"video_input_5s_max_price"`
}

func DefaultVideoModelCatalog() VideoModelCatalog {
	commonRatios := []string{"16:9", "9:16", "1:1", "4:3", "3:4"}
	return VideoModelCatalog{
		"seedance-2.0": {
			Key:                  "seedance-2.0",
			DisplayName:          "Doubao Seedance 2.0",
			ModelID:              "doubao-seedance-2-0-260128",
			SupportedResolutions: []string{"480p", "720p", "1080p", "4k"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			Supports4K:           true,
			NoInputPricePerSecond: map[string]float64{
				"480p":  2.31 / 5,
				"720p":  4.97 / 5,
				"1080p": 12.39 / 5,
				"4k":    25.27 / 5,
			},
			VideoInput5sMinPrice: map[string]float64{"480p": 2.53, "720p": 5.44, "1080p": 13.56, "4k": 27.99},
			VideoInput5sMaxPrice: map[string]float64{"480p": 5.62, "720p": 12.10, "1080p": 30.13, "4k": 62.21},
		},
		"seedance-2.0-fast": {
			Key:                  "seedance-2.0-fast",
			DisplayName:          "Doubao Seedance 2.0 Fast",
			ModelID:              "doubao-seedance-2-0-fast-260128",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			NoInputPricePerSecond: map[string]float64{
				"480p": 1.86 / 5,
				"720p": 4.00 / 5,
			},
			VideoInput5sMinPrice: map[string]float64{"480p": 1.99, "720p": 4.28},
			VideoInput5sMaxPrice: map[string]float64{"480p": 4.42, "720p": 9.50},
		},
		"seedance-2.0-mini": {
			Key:                  "seedance-2.0-mini",
			DisplayName:          "Doubao Seedance 2.0 Mini",
			ModelID:              "doubao-seedance-2-0-mini-260615",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			NoInputPricePerSecond: map[string]float64{
				"480p": 1.16 / 5,
				"720p": 2.48 / 5,
			},
			VideoInput5sMinPrice: map[string]float64{"480p": 1.27, "720p": 2.72},
			VideoInput5sMaxPrice: map[string]float64{"480p": 2.81, "720p": 6.05},
		},
	}
}

func VideoModelCatalogFromConfig(entries []config.VideoModelCatalogEntry) VideoModelCatalog {
	catalog := DefaultVideoModelCatalog()
	for _, entry := range entries {
		if strings.TrimSpace(entry.Key) == "" {
			continue
		}
		spec, existed := catalog[entry.Key]
		spec.Key = entry.Key
		if entry.DisplayName != "" {
			spec.DisplayName = entry.DisplayName
		}
		if entry.ModelID != "" {
			spec.ModelID = entry.ModelID
		}
		if len(entry.SupportedResolutions) > 0 {
			spec.SupportedResolutions = entry.SupportedResolutions
		}
		if len(entry.SupportedRatios) > 0 {
			spec.SupportedRatios = entry.SupportedRatios
		}
		if entry.MinDuration > 0 {
			spec.MinDuration = entry.MinDuration
		}
		if entry.MaxDuration > 0 {
			spec.MaxDuration = entry.MaxDuration
		}
		if !existed || entry.SupportsVideoInput {
			spec.SupportsVideoInput = entry.SupportsVideoInput
		}
		if !existed || entry.Supports4K {
			spec.Supports4K = entry.Supports4K
		}
		if len(entry.NoInputPricePerSecond) > 0 {
			spec.NoInputPricePerSecond = entry.NoInputPricePerSecond
		}
		if len(entry.VideoInput5sMinPrice) > 0 {
			spec.VideoInput5sMinPrice = entry.VideoInput5sMinPrice
		}
		if len(entry.VideoInput5sMaxPrice) > 0 {
			spec.VideoInput5sMaxPrice = entry.VideoInput5sMaxPrice
		}
		catalog[entry.Key] = spec
	}
	return catalog
}

func ResolveVideoGenerationPlan(req VideoGenerationRequest, defaults model.VideoDefaults, policy model.VideoModelPolicy, catalog VideoModelCatalog, creditMultiplier int) (VideoGenerationPlan, error) {
	if defaults == (model.VideoDefaults{}) || policy.DefaultModel == "" || len(policy.AllowedModels) == 0 {
		return VideoGenerationPlan{}, fmt.Errorf("project video profile is not configured")
	}
	if catalog == nil {
		catalog = DefaultVideoModelCatalog()
	}
	if creditMultiplier <= 0 {
		creditMultiplier = 1000
	}
	resolved := req
	if resolved.Purpose == "" {
		resolved.Purpose = defaults.Purpose
	}
	if resolved.Purpose == "" {
		resolved.Purpose = VideoPurposePlanting
	}
	modelKey := strings.TrimSpace(resolved.Model)
	if modelKey == "" {
		modelKey = defaults.ModelKey
	}
	if modelKey == "" {
		modelKey = policy.DefaultModel
	}
	if !stringIn(modelKey, policy.AllowedModels) {
		return VideoGenerationPlan{}, fmt.Errorf("model %s is not allowed by project video policy", modelKey)
	}
	spec, ok := catalog[modelKey]
	if !ok {
		return VideoGenerationPlan{}, fmt.Errorf("unknown video model: %s", modelKey)
	}
	resolved.Model = spec.ModelID
	if resolved.Resolution == "" {
		resolved.Resolution = defaults.Resolution
	}
	if resolved.Ratio == "" {
		resolved.Ratio = defaults.Ratio
	}
	if resolved.Duration == 0 {
		resolved.Duration = defaults.Duration
	}
	if resolved.Watermark == nil {
		resolved.Watermark = defaults.Watermark
	}
	preflight := defaults.Preflight
	if resolved.Preflight != nil {
		preflight = *resolved.Preflight
	}
	if err := validateVideoGenerationRequest(resolved); err != nil {
		return VideoGenerationPlan{}, err
	}
	if policy.MaxDuration > 0 && resolved.Duration > policy.MaxDuration {
		return VideoGenerationPlan{}, fmt.Errorf("duration %d exceeds project max duration %d", resolved.Duration, policy.MaxDuration)
	}
	if policy.MaxResolution != "" && resolutionRank(resolved.Resolution) > resolutionRank(policy.MaxResolution) {
		return VideoGenerationPlan{}, fmt.Errorf("resolution %s exceeds project max resolution %s", resolved.Resolution, policy.MaxResolution)
	}
	if !stringInFold(resolved.Resolution, spec.SupportedResolutions) {
		suggested := highestResolutionAtOrBelow(spec.SupportedResolutions, resolved.Resolution)
		if suggested != "" {
			if policy.AllowAutoDowngrade {
				resolved.Resolution = suggested
			} else {
				return VideoGenerationPlan{}, fmt.Errorf("model %s does not support resolution %s; suggested resolution: %s", modelKey, resolved.Resolution, suggested)
			}
		} else {
			return VideoGenerationPlan{}, fmt.Errorf("model %s does not support resolution %s", modelKey, resolved.Resolution)
		}
	}
	if !stringInFold(resolved.Ratio, spec.SupportedRatios) {
		return VideoGenerationPlan{}, fmt.Errorf("model %s does not support ratio %s", modelKey, resolved.Ratio)
	}
	if resolved.Duration < spec.MinDuration || resolved.Duration > spec.MaxDuration {
		return VideoGenerationPlan{}, fmt.Errorf("model %s duration must be between %d and %d seconds", modelKey, spec.MinDuration, spec.MaxDuration)
	}
	hasInputVideo, inputSeconds := videoInputStats(resolved.ReferenceSet)
	if hasInputVideo && !spec.SupportsVideoInput {
		return VideoGenerationPlan{}, fmt.Errorf("model %s does not support video input", modelKey)
	}
	cny, err := estimateVideoCNY(spec, resolved.Resolution, resolved.Duration, hasInputVideo, inputSeconds)
	if err != nil {
		return VideoGenerationPlan{}, err
	}
	credits := int(math.Ceil(cny * float64(creditMultiplier)))
	breakdown := &model.VideoPricingBreakdown{
		CNY:              round2(cny),
		CreditMultiplier: creditMultiplier,
		InputVideo:       hasInputVideo,
		InputSeconds:     inputSeconds,
		OutputSeconds:    resolved.Duration,
		Resolution:       resolved.Resolution,
		Ratio:            resolved.Ratio,
		ModelKey:         modelKey,
	}
	plan := VideoGenerationPlan{
		Purpose:          resolved.Purpose,
		Prompt:           resolved.Prompt,
		ModelKey:         modelKey,
		Model:            resolved.Model,
		Resolution:       resolved.Resolution,
		Ratio:            resolved.Ratio,
		Duration:         resolved.Duration,
		Seed:             resolved.Seed,
		CameraFixed:      resolved.CameraFixed,
		Watermark:        resolved.Watermark,
		Preflight:        preflight,
		ServiceTier:      resolved.ServiceTier,
		References:       resolved.ReferenceSet,
		EstimatedCredits: credits,
		PricingBreakdown: breakdown,
	}
	return plan, nil
}

func estimateVideoCNY(spec VideoModelSpec, resolution string, duration int64, hasInputVideo bool, inputSeconds float64) (float64, error) {
	resolution = strings.ToLower(resolution)
	perSecond, ok := spec.NoInputPricePerSecond[resolution]
	if !ok {
		return 0, fmt.Errorf("pricing is not configured for model %s resolution %s", spec.Key, resolution)
	}
	if !hasInputVideo {
		return round2(perSecond * float64(duration)), nil
	}
	minPrice, minOK := spec.VideoInput5sMinPrice[resolution]
	maxPrice, maxOK := spec.VideoInput5sMaxPrice[resolution]
	if !minOK || !maxOK {
		return 0, fmt.Errorf("video-input pricing is not configured for model %s resolution %s", spec.Key, resolution)
	}
	if inputSeconds <= 0 {
		inputSeconds = 4
	}
	if inputSeconds < 2 {
		inputSeconds = 2
	}
	if inputSeconds > 15 {
		inputSeconds = 15
	}
	// Official table gives a 5s-output range: minimum for 2-4s input, maximum
	// for 15s input. Interpolate between 4s and 15s, then scale by output length.
	base := minPrice
	if inputSeconds > 4 {
		base = minPrice + (maxPrice-minPrice)*((inputSeconds-4)/(15-4))
	}
	return round2(base * float64(duration) / 5), nil
}

func videoInputStats(refs []VideoReferenceInput) (bool, float64) {
	maxSeconds := 0.0
	found := false
	for _, ref := range refs {
		if ref.Type != VideoReferenceVideo {
			continue
		}
		found = true
		if ref.InputDurationSeconds > maxSeconds {
			maxSeconds = ref.InputDurationSeconds
		}
	}
	return found, maxSeconds
}

func stringIn(v string, items []string) bool {
	for _, item := range items {
		if v == item {
			return true
		}
	}
	return false
}

func stringInFold(v string, items []string) bool {
	for _, item := range items {
		if strings.EqualFold(v, item) {
			return true
		}
	}
	return false
}

func resolutionRank(v string) int {
	switch strings.ToLower(v) {
	case "480p":
		return 1
	case "720p":
		return 2
	case "1080p":
		return 3
	case "2k":
		return 4
	case "4k":
		return 5
	default:
		return 0
	}
}

func highestResolutionAtOrBelow(supported []string, requested string) string {
	reqRank := resolutionRank(requested)
	ordered := append([]string(nil), supported...)
	sort.Slice(ordered, func(i, j int) bool { return resolutionRank(ordered[i]) > resolutionRank(ordered[j]) })
	for _, item := range ordered {
		if resolutionRank(item) <= reqRank {
			return strings.ToLower(item)
		}
	}
	return ""
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
