package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"

	"github.com/anbanai/anban-creator/server/config"
)

const (
	VideoPurposePlanting  = "planting"
	VideoPurposeEcommerce = "ecommerce"
	VideoPurposeLeadGen   = "lead_gen"
	VideoPurposePromotion = "promotion"

	VideoCreativeTypePersonalIP         = "personal_ip"
	VideoCreativeTypeHighEfficiencyJoke = "high_efficiency_joke"
	VideoCreativeTypeProductDemo        = "product_demo"
	VideoCreativeTypeBrandPromo         = "brand_promo"
	VideoCreativeTypeCustom             = "custom"

	VideoReferenceText  = "text"
	VideoReferenceImage = "image_url"
	VideoReferenceAudio = "audio_url"
	VideoReferenceVideo = "video_url"
)

var validVideoPurposes = map[string]bool{
	"":                    true,
	VideoPurposePlanting:  true,
	VideoPurposeEcommerce: true,
	VideoPurposeLeadGen:   true,
	VideoPurposePromotion: true,
}

var validVideoCreativeTypes = map[string]bool{
	"":                                  true,
	VideoCreativeTypePersonalIP:         true,
	VideoCreativeTypeHighEfficiencyJoke: true,
	VideoCreativeTypeProductDemo:        true,
	VideoCreativeTypeBrandPromo:         true,
	VideoCreativeTypeCustom:             true,
}

var validVideoReferenceTypes = map[string]bool{
	VideoReferenceText:  true,
	VideoReferenceImage: true,
	VideoReferenceAudio: true,
	VideoReferenceVideo: true,
}

var validVideoProductionModes = map[string]bool{
	"":                          true,
	VideoProductionModeFastLane: true,
	VideoProductionModeGuided:   true,
	VideoProductionModeSequence: true,
	VideoProductionModeRemake:   true,
}

// VideoGenerationRequest is the service-facing request for Ark content generation.
type VideoGenerationRequest struct {
	Prompt                 string                `json:"prompt"`
	ScenarioKey            string                `json:"scenario_key,omitempty"`
	ProductionMode         string                `json:"production_mode,omitempty"`
	Purpose                string                `json:"purpose,omitempty"`
	CreativeType           string                `json:"creative_type,omitempty"`
	SubjectProfile         string                `json:"subject_profile,omitempty"`
	Audience               string                `json:"audience,omitempty"`
	SingleMessage          string                `json:"single_message,omitempty"`
	Model                  string                `json:"model,omitempty"`
	Resolution             string                `json:"resolution,omitempty"`
	Ratio                  string                `json:"ratio,omitempty"`
	Duration               int64                 `json:"duration,omitempty"`
	PlannedDurationSeconds int64                 `json:"planned_duration_seconds,omitempty"`
	TargetDurationReason   string                `json:"target_duration_reason,omitempty"`
	Seed                   *int64                `json:"seed,omitempty"`
	CameraFixed            *bool                 `json:"camera_fixed,omitempty"`
	Watermark              *bool                 `json:"watermark,omitempty"`
	Preflight              *bool                 `json:"preflight,omitempty"`
	ServiceTier            string                `json:"service_tier,omitempty"`
	SafetyID               string                `json:"safety_identifier,omitempty"`
	TaskID                 string                `json:"task_id,omitempty"`
	RetakeBudget           int                   `json:"retake_budget,omitempty"`
	DeliveryTargets        []string              `json:"delivery_targets,omitempty"`
	ReferenceSet           []VideoReferenceInput `json:"references,omitempty"`
}

// VideoReferenceInput describes one text/image/audio/video reference.
type VideoReferenceInput struct {
	Type                 string   `json:"type"`
	URL                  string   `json:"url,omitempty"`
	Text                 string   `json:"text,omitempty"`
	TaskFileID           string   `json:"task_file_id,omitempty"`
	ReferenceRole        string   `json:"reference_role,omitempty"`
	MustKeep             []string `json:"must_keep,omitempty"`
	CanChange            []string `json:"can_change,omitempty"`
	MustNotTransfer      []string `json:"must_not_transfer,omitempty"`
	InputDurationSeconds float64  `json:"input_duration_seconds,omitempty"`
}

// VideoGenerationCreateResult is returned immediately after task submission.
type VideoGenerationCreateResult struct {
	VideoTaskID string `json:"video_task_id"`
	Model       string `json:"model"`
	Resolution  string `json:"resolution,omitempty"`
	Ratio       string `json:"ratio,omitempty"`
	Duration    int64  `json:"duration,omitempty"`
	Seed        *int64 `json:"seed,omitempty"`
}

// VideoGenerationTaskResult is returned for task query/list responses.
type VideoGenerationTaskResult struct {
	VideoTaskID   string                `json:"video_task_id"`
	Status        string                `json:"status"`
	Model         string                `json:"model,omitempty"`
	VideoURL      string                `json:"video_url,omitempty"`
	LastFrameURL  string                `json:"last_frame_url,omitempty"`
	FileURL       string                `json:"file_url,omitempty"`
	Resolution    string                `json:"resolution,omitempty"`
	Ratio         string                `json:"ratio,omitempty"`
	Duration      int64                 `json:"duration,omitempty"`
	Seed          *int64                `json:"seed,omitempty"`
	RevisedPrompt string                `json:"revised_prompt,omitempty"`
	Error         *VideoGenerationError `json:"error,omitempty"`
}

type VideoGenerationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	VideoDurationSourceUser           = "user"
	VideoDurationSourceReferenceVideo = "reference_video"
	VideoDurationSourceAIPlanned      = "ai_planned"
	VideoDurationSourceProjectDefault = "project_default"
)

type VideoGenerationSegmentPlan struct {
	Index       int    `json:"index"`
	StartSecond int64  `json:"start_second"`
	EndSecond   int64  `json:"end_second"`
	Duration    int64  `json:"duration"`
	Prompt      string `json:"prompt,omitempty"`
	ModelKey    string `json:"model_key,omitempty"`
	Model       string `json:"model,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Ratio       string `json:"ratio,omitempty"`
}

// VideoGenerationPlan is a deterministic MCP planning artifact.
type VideoGenerationPlan struct {
	ProjectID                 string                       `json:"project_id"`
	ScenarioKey               string                       `json:"scenario_key,omitempty"`
	ProductionMode            string                       `json:"production_mode,omitempty"`
	Purpose                   string                       `json:"purpose"`
	CreativeType              string                       `json:"creative_type,omitempty"`
	SubjectProfile            string                       `json:"subject_profile,omitempty"`
	Audience                  string                       `json:"audience,omitempty"`
	SingleMessage             string                       `json:"single_message,omitempty"`
	Prompt                    string                       `json:"prompt"`
	ModelKey                  string                       `json:"model_key,omitempty"`
	Model                     string                       `json:"model,omitempty"`
	Resolution                string                       `json:"resolution"`
	Ratio                     string                       `json:"ratio"`
	Duration                  int64                        `json:"duration"`
	TargetDurationSeconds     int64                        `json:"target_duration_seconds"`
	TargetDurationSource      string                       `json:"target_duration_source"`
	TargetDurationReason      string                       `json:"target_duration_reason,omitempty"`
	SegmentMaxDurationSeconds int64                        `json:"segment_max_duration_seconds,omitempty"`
	SegmentMinDurationSeconds int64                        `json:"segment_min_duration_seconds,omitempty"`
	Segments                  []VideoGenerationSegmentPlan `json:"segments,omitempty"`
	Seed                      *int64                       `json:"seed,omitempty"`
	CameraFixed               *bool                        `json:"camera_fixed,omitempty"`
	Watermark                 *bool                        `json:"watermark,omitempty"`
	Preflight                 bool                         `json:"preflight,omitempty"`
	ServiceTier               string                       `json:"service_tier,omitempty"`
	References                []VideoReferenceInput        `json:"references,omitempty"`
	RetakeBudget              int                          `json:"retake_budget,omitempty"`
	DeliveryTargets           []string                     `json:"delivery_targets,omitempty"`
	RequiredArtifacts         []string                     `json:"required_artifacts"`
	SDKPayloadPreview         map[string]any               `json:"sdk_payload_preview"`
}

// VideoService wraps Ark runtime content generation for short videos.
type VideoService struct {
	cfg    *config.VideoAPIConfig
	client *arkruntime.Client
}

func NewVideoService(cfg *config.VideoAPIConfig) *VideoService {
	if cfg == nil {
		cfg = &config.VideoAPIConfig{}
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	opts := []arkruntime.ConfigOption{arkruntime.WithTimeout(timeout)}
	if cfg.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(cfg.BaseURL))
	}
	return &VideoService{
		cfg:    cfg,
		client: arkruntime.NewClientWithApiKey(cfg.Key, opts...),
	}
}

func (s *VideoService) CreateTask(ctx context.Context, req VideoGenerationRequest) (*VideoGenerationCreateResult, error) {
	arkReq, resolved, err := s.buildArkCreateRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.CreateContentGenerationTask(ctx, arkReq)
	if err != nil {
		return nil, convertVideoSDKError(err)
	}
	return &VideoGenerationCreateResult{
		VideoTaskID: resp.ID,
		Model:       resolved.Model,
		Resolution:  resolved.Resolution,
		Ratio:       resolved.Ratio,
		Duration:    resolved.Duration,
		Seed:        resolved.Seed,
	}, nil
}

func (s *VideoService) QueryTask(ctx context.Context, videoTaskID string) (*VideoGenerationTaskResult, error) {
	videoTaskID = strings.TrimSpace(videoTaskID)
	if videoTaskID == "" {
		return nil, fmt.Errorf("video_task_id is required")
	}
	resp, err := s.client.GetContentGenerationTask(ctx, arkmodel.GetContentGenerationTaskRequest{ID: videoTaskID})
	if err != nil {
		return nil, convertVideoSDKError(err)
	}
	return mapVideoTaskResponse(resp), nil
}

func (s *VideoService) BuildPlan(req VideoGenerationRequest, projectID string) (*VideoGenerationPlan, error) {
	arkReq, resolved, err := s.buildArkCreateRequest(req)
	if err != nil {
		return nil, err
	}
	contentPreview := make([]map[string]any, 0, len(arkReq.Content))
	for _, item := range arkReq.Content {
		entry := map[string]any{"type": item.Type}
		switch item.Type {
		case arkmodel.ContentGenerationContentItemTypeText:
			entry["text"] = item.Text
		case arkmodel.ContentGenerationContentItemTypeImage:
			entry["image_url"] = item.ImageURL
		case arkmodel.ContentGenerationContentItemTypeAudio:
			entry["audio_url"] = item.AudioURL
		case arkmodel.ContentGenerationContentItemTypeVideo:
			entry["video_url"] = item.VideoURL
		}
		contentPreview = append(contentPreview, entry)
	}
	return &VideoGenerationPlan{
		ProjectID:       projectID,
		ScenarioKey:     resolved.ScenarioKey,
		ProductionMode:  resolved.ProductionMode,
		Purpose:         resolved.Purpose,
		CreativeType:    resolved.CreativeType,
		SubjectProfile:  resolved.SubjectProfile,
		Audience:        resolved.Audience,
		SingleMessage:   resolved.SingleMessage,
		Prompt:          resolved.Prompt,
		Model:           resolved.Model,
		Resolution:      resolved.Resolution,
		Ratio:           resolved.Ratio,
		Duration:        resolved.Duration,
		Seed:            resolved.Seed,
		CameraFixed:     resolved.CameraFixed,
		Watermark:       resolved.Watermark,
		Preflight:       resolved.Preflight != nil && *resolved.Preflight,
		ServiceTier:     resolved.ServiceTier,
		References:      resolved.ReferenceSet,
		RetakeBudget:    resolved.RetakeBudget,
		DeliveryTargets: resolved.DeliveryTargets,
		RequiredArtifacts: []string{
			"reference-anchors.md",
			"creative-brief.md",
			"video-understanding.json",
			"script.md",
			"shot-plan.md",
			"generation-plan.json",
			"quality-review.md",
		},
		SDKPayloadPreview: map[string]any{
			"model":        arkReq.Model,
			"content":      contentPreview,
			"resolution":   arkReq.Resolution,
			"ratio":        arkReq.Ratio,
			"duration":     arkReq.Duration,
			"seed":         arkReq.Seed,
			"camera_fixed": arkReq.CameraFixed,
			"watermark":    arkReq.Watermark,
			"service_tier": arkReq.ServiceTier,
		},
	}, nil
}

func (s *VideoService) ValidateRequest(req VideoGenerationRequest) error {
	_, _, err := s.buildArkCreateRequest(req)
	return err
}

func (s *VideoService) buildArkCreateRequest(req VideoGenerationRequest) (arkmodel.CreateContentGenerationTaskRequest, VideoGenerationRequest, error) {
	resolved := s.applyDefaults(req)
	if err := validateVideoGenerationRequest(resolved); err != nil {
		return arkmodel.CreateContentGenerationTaskRequest{}, resolved, err
	}
	if strings.TrimSpace(resolved.Prompt) == "" {
		return arkmodel.CreateContentGenerationTaskRequest{}, resolved, fmt.Errorf("prompt is required")
	}
	content := make([]*arkmodel.CreateContentGenerationContentItem, 0, len(resolved.ReferenceSet)+1)
	content = append(content, &arkmodel.CreateContentGenerationContentItem{
		Type: arkmodel.ContentGenerationContentItemTypeText,
		Text: &resolved.Prompt,
	})
	for _, ref := range resolved.ReferenceSet {
		item, err := mapVideoReferenceToArkContent(ref)
		if err != nil {
			return arkmodel.CreateContentGenerationTaskRequest{}, resolved, err
		}
		content = append(content, item)
	}

	arkReq := arkmodel.CreateContentGenerationTaskRequest{
		Model:       resolved.Model,
		Content:     content,
		Seed:        resolved.Seed,
		CameraFixed: resolved.CameraFixed,
		Watermark:   resolved.Watermark,
	}
	if resolved.Resolution != "" {
		arkReq.Resolution = &resolved.Resolution
	}
	if resolved.Ratio != "" {
		arkReq.Ratio = &resolved.Ratio
	}
	if resolved.Duration > 0 {
		arkReq.Duration = &resolved.Duration
	}
	if resolved.ServiceTier != "" {
		arkReq.ServiceTier = &resolved.ServiceTier
	}
	if resolved.SafetyID != "" {
		arkReq.SafetyIdentifier = &resolved.SafetyID
	}
	return arkReq, resolved, nil
}

func (s *VideoService) applyDefaults(req VideoGenerationRequest) VideoGenerationRequest {
	if req.CreativeType == "" {
		req.CreativeType = VideoCreativeTypePersonalIP
	}
	if req.Purpose == "" {
		req.Purpose = DefaultVideoPurposeForCreativeType(req.CreativeType)
	}
	return req
}

func validateVideoGenerationRequest(req VideoGenerationRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("video model is required")
	}
	if !validVideoCreativeTypes[req.CreativeType] {
		return fmt.Errorf("creative_type must be one of personal_ip, high_efficiency_joke, product_demo, brand_promo, custom")
	}
	if !validVideoPurposes[req.Purpose] {
		return fmt.Errorf("purpose must be one of planting, ecommerce, lead_gen, promotion")
	}
	if !validVideoProductionModes[req.ProductionMode] {
		return fmt.Errorf("production_mode must be one of fast_lane, guided, sequence, remake")
	}
	if req.RetakeBudget < 0 || req.RetakeBudget > 20 {
		return fmt.Errorf("retake_budget must be between 0 and 20")
	}
	if req.Duration <= 0 || req.Duration > 600 {
		return fmt.Errorf("duration must be between 1 and 600 seconds")
	}
	if req.Ratio != "" && !isAllowedVideoRatio(req.Ratio) {
		return fmt.Errorf("ratio must be one of 9:16, 16:9, 1:1, 4:3, 3:4")
	}
	if req.Resolution != "" && !isAllowedVideoResolution(req.Resolution) {
		return fmt.Errorf("resolution must be one of 480p, 720p, 1080p, 2k, 4k")
	}
	for _, ref := range req.ReferenceSet {
		if !validVideoReferenceTypes[ref.Type] {
			return fmt.Errorf("reference type must be one of text, image_url, audio_url, video_url")
		}
		if ref.Type == VideoReferenceText {
			if strings.TrimSpace(ref.Text) == "" {
				return fmt.Errorf("text reference requires text")
			}
			continue
		}
		if err := validatePublicHTTPSURL(ref.URL); err != nil {
			return err
		}
	}
	return nil
}

func DefaultVideoPurposeForCreativeType(creativeType string) string {
	switch creativeType {
	case VideoCreativeTypeHighEfficiencyJoke:
		return VideoPurposePromotion
	default:
		return VideoPurposePlanting
	}
}

func isAllowedVideoRatio(v string) bool {
	switch strings.ToLower(v) {
	case "9:16", "16:9", "1:1", "4:3", "3:4":
		return true
	default:
		return false
	}
}

func isAllowedVideoResolution(v string) bool {
	switch strings.ToLower(v) {
	case "480p", "720p", "1080p", "2k", "4k":
		return true
	default:
		return false
	}
}

func validatePublicHTTPSURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("reference URL must be a publicly accessible HTTPS URL; use OSS/CDN or another Ark-accessible URL")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
			return fmt.Errorf("reference URL must be a publicly accessible HTTPS URL; local/private addresses are not accessible by Ark")
		}
	}
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".local") {
		return fmt.Errorf("reference URL must be a publicly accessible HTTPS URL; local/private addresses are not accessible by Ark")
	}
	return nil
}

func ValidatePublicHTTPSURLForVideoReference(raw string) error {
	return validatePublicHTTPSURL(raw)
}

func mapVideoReferenceToArkContent(ref VideoReferenceInput) (*arkmodel.CreateContentGenerationContentItem, error) {
	switch ref.Type {
	case VideoReferenceText:
		text := ref.Text
		return &arkmodel.CreateContentGenerationContentItem{Type: arkmodel.ContentGenerationContentItemTypeText, Text: &text}, nil
	case VideoReferenceImage:
		return &arkmodel.CreateContentGenerationContentItem{Type: arkmodel.ContentGenerationContentItemTypeImage, ImageURL: &arkmodel.ImageURL{URL: ref.URL}}, nil
	case VideoReferenceAudio:
		return &arkmodel.CreateContentGenerationContentItem{Type: arkmodel.ContentGenerationContentItemTypeAudio, AudioURL: &arkmodel.AudioUrl{Url: ref.URL}}, nil
	case VideoReferenceVideo:
		return &arkmodel.CreateContentGenerationContentItem{Type: arkmodel.ContentGenerationContentItemTypeVideo, VideoURL: &arkmodel.VideoUrl{Url: ref.URL}}, nil
	default:
		return nil, fmt.Errorf("reference type must be one of text, image_url, audio_url, video_url")
	}
}

func mapVideoTaskResponse(resp arkmodel.GetContentGenerationTaskResponse) *VideoGenerationTaskResult {
	result := &VideoGenerationTaskResult{
		VideoTaskID:   resp.ID,
		Status:        resp.Status,
		Model:         resp.Model,
		VideoURL:      resp.Content.VideoURL,
		LastFrameURL:  resp.Content.LastFrameURL,
		FileURL:       resp.Content.FileURL,
		Seed:          resp.Seed,
		RevisedPrompt: derefString(resp.RevisedPrompt),
	}
	if resp.Resolution != nil {
		result.Resolution = *resp.Resolution
	}
	if resp.Ratio != nil {
		result.Ratio = *resp.Ratio
	}
	if resp.Duration != nil {
		result.Duration = *resp.Duration
	}
	if resp.Error != nil {
		result.Error = &VideoGenerationError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return result
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func convertVideoSDKError(err error) error {
	var apiErr *arkmodel.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case 401:
			return fmt.Errorf("video generation unauthorized: API Key is invalid or expired")
		case 402, 403:
			return fmt.Errorf("video generation access denied or account balance insufficient")
		case 429:
			return fmt.Errorf("video generation rate limited: retry later")
		case 400:
			return fmt.Errorf("video generation request rejected: %s", apiErr.Message)
		default:
			if apiErr.Message != "" {
				return fmt.Errorf("video generation failed: %s", apiErr.Message)
			}
		}
	}
	return err
}
