package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"slices"
	"strings"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type MontageSourceRequirement string

const (
	MontageSourceRequirementOptional     MontageSourceRequirement = "optional"
	MontageSourceRequirementVideo        MontageSourceRequirement = "video"
	MontageSourceRequirementVideoOrAudio MontageSourceRequirement = "video_or_audio"
	agentBootstrapFileMaxBytes                                    = 64 << 20
	agentBootstrapTotalMaxBytes                                   = 512 << 20
	montageSourceMaxBytes                                         = agentBootstrapFileMaxBytes
	montageBootstrapAttachmentMaxCount                            = 5
	montageBootstrapAttachmentMaxBytes                            = 50 << 20
	montageBootstrapReferenceMaxBytes                             = 10 << 20
	montageBootstrapInlineReserveBytes                            = 2 << 20
	montageBootstrapRuntimeReserveBytes                           = 2 << 20
	// Leave room for five input attachments, two reference images, and inline
	// bootstrap metadata before admitting task-file source media.
	montageBootstrapNonSourceReserve = montageBootstrapAttachmentMaxCount*montageBootstrapAttachmentMaxBytes + 2*montageBootstrapReferenceMaxBytes + montageBootstrapInlineReserveBytes + montageBootstrapRuntimeReserveBytes
	montageSourceTotalMaxBytes       = agentBootstrapTotalMaxBytes - montageBootstrapNonSourceReserve
)

// ValidateMontageInlineBootstrapBudget bounds the serialized inline files
// emitted for a Montage task before paid admission. Bootstrap performs the
// same final aggregate check after adding execution-specific settings and
// attachment metadata; this check prevents unbounded user-controlled fields
// from consuming the source-media reserve first.
func ValidateMontageInlineBootstrapBudget(input *model.MontageInput, project *model.Project, toolPolicy map[string]serverconfig.MontageToolCapabilityPolicy, pipelineDefaults map[string]map[string]any, attachments ...[]model.EntryAttachment) error {
	if input == nil {
		return fmt.Errorf("%w: montage_input is required", ErrMontageInput)
	}
	values := []any{
		input,
		toolPolicy,
		pipelineDefaults,
	}
	if len(attachments) > 0 && len(attachments[0]) > 0 {
		// buildAttachmentFiles emits inline text and an index for these same
		// attachments. Serialize the full descriptor as a conservative upper
		// bound so user-controlled metadata cannot consume the source reserve
		// after paid admission.
		values = append(values, attachments[0])
	}
	instructions := ""
	if project != nil {
		instructions = project.Instructions
		keywords := make([]string, 0)
		for _, keyword := range strings.Split(project.Keywords, ",") {
			if keyword = strings.TrimSpace(keyword); keyword != "" {
				keywords = append(keywords, keyword)
			}
		}
		// BuildAppConfig emits these project fields into settings.json for every
		// task, including Montage tasks. Count the same user-controlled values
		// here so admission matches the runtime payload.
		values = append(values, &appconfig.Config{
			Name: project.Name, Keywords: keywords, Positioning: instructions,
		})
	}
	totalBytes := int64(0)
	for _, value := range values {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Errorf("%w: montage bootstrap metadata cannot be serialized: %v", ErrMontageInput, err)
		}
		totalBytes += int64(len(raw))
	}
	if trimmed := strings.TrimSpace(instructions); trimmed != "" {
		totalBytes += int64(len([]byte("# CLAUDE.md\n\n## 项目定位\n\n" + trimmed)))
	}
	if totalBytes > montageBootstrapInlineReserveBytes {
		return fmt.Errorf("%w: inline Montage bootstrap metadata exceeds %d bytes", ErrMontageInput, montageBootstrapInlineReserveBytes)
	}
	return nil
}

type MontageOutputMode string

const (
	MontageOutputModeSingle   MontageOutputMode = "single"
	MontageOutputModeMultiple MontageOutputMode = "multiple"
)

type MontagePipelineCapability struct {
	Key                        string                   `json:"key"`
	DisplayName                string                   `json:"display_name"`
	Description                string                   `json:"description"`
	BestFor                    []string                 `json:"best_for"`
	SourceHint                 string                   `json:"source_hint"`
	OutputHint                 string                   `json:"output_hint"`
	SourceRequirement          MontageSourceRequirement `json:"source_requirement"`
	OutputMode                 MontageOutputMode        `json:"output_mode"`
	RecommendedDurationSeconds int64                    `json:"recommended_duration_seconds"`
}

type MontageCapabilityCatalog struct {
	Enabled            bool                        `json:"enabled"`
	DefaultPipeline    string                      `json:"default_pipeline"`
	MaxDurationSeconds int64                       `json:"max_duration_seconds"`
	MaxAssets          int                         `json:"max_assets"`
	Items              []MontagePipelineCapability `json:"items"`
}

type MontageCapabilityService struct {
	config serverconfig.MontageConfig
}

func NewMontageCapabilityService(cfg serverconfig.MontageConfig) *MontageCapabilityService {
	return &MontageCapabilityService{config: cfg}
}

var montagePipelineCapabilities = map[string]MontagePipelineCapability{
	"cinematic": {
		Key: "cinematic", DisplayName: "电影感制作", Description: "品牌片、预告片与情绪叙事",
		BestFor:    []string{"品牌发布", "概念预告", "氛围短片"},
		SourceHint: "可使用视频、图片，也可仅根据创意说明生成", OutputHint: "一条完整成片",
		SourceRequirement: MontageSourceRequirementOptional, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 30,
	},
	"talking-head": {
		Key: "talking-head", DisplayName: "口播精剪", Description: "人物讲解、访谈与课程内容",
		BestFor:    []string{"人物口播", "课程讲解", "采访精剪"},
		SourceHint: "需要一段包含人物讲话的原始视频", OutputHint: "一条带字幕的精剪视频",
		SourceRequirement: MontageSourceRequirementVideo, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 60,
	},
	"screen-demo": {
		Key: "screen-demo", DisplayName: "屏幕演示", Description: "产品教程、软件操作与终端流程",
		BestFor:    []string{"产品演示", "使用教程", "终端操作"},
		SourceHint: "可上传屏幕录制，或在说明中给出可复现的操作步骤", OutputHint: "一条清晰的演示视频",
		SourceRequirement: MontageSourceRequirementOptional, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 60,
	},
	"clip-factory": {
		Key: "clip-factory", DisplayName: "长视频拆条", Description: "从直播、播客或访谈中提炼短视频",
		BestFor:    []string{"直播切片", "播客拆条", "访谈精选"},
		SourceHint: "需要一段长视频或音频作为拆条来源", OutputHint: "多条可独立发布的短视频",
		SourceRequirement: MontageSourceRequirementVideoOrAudio, OutputMode: MontageOutputModeMultiple, RecommendedDurationSeconds: 45,
	},
}

func customMontagePipelineCapability(key string) MontagePipelineCapability {
	return MontagePipelineCapability{
		Key: key, DisplayName: key, Description: "自定义视频工作流",
		BestFor: []string{"团队自定义制作流程"}, SourceHint: "根据工作流要求添加图片、视频或音频素材",
		OutputHint: "按自定义工作流生成视频", SourceRequirement: MontageSourceRequirementOptional,
		OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 30,
	}
}

func (s *MontageCapabilityService) Catalog() MontageCapabilityCatalog {
	catalog := MontageCapabilityCatalog{
		Enabled: s.config.Enabled, DefaultPipeline: strings.TrimSpace(s.config.DefaultPipeline),
		MaxDurationSeconds: s.config.MaxDurationSeconds, MaxAssets: s.config.MaxAssets,
		Items: []MontagePipelineCapability{},
	}
	if !s.config.Enabled {
		return catalog
	}
	for _, rawKey := range s.config.AllowedPipelines {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		item, ok := montagePipelineCapabilities[key]
		if !ok {
			item = customMontagePipelineCapability(key)
		}
		item.BestFor = slices.Clone(item.BestFor)
		catalog.Items = append(catalog.Items, item)
	}
	return catalog
}

func (s *MontageCapabilityService) NormalizeAndValidateInput(input *model.MontageInput, defaults model.MontageDefaults) error {
	if input == nil {
		return fmt.Errorf("%w: montage_input is required", ErrMontageInput)
	}
	input.Brief = strings.TrimSpace(input.Brief)
	input.PipelineKey = strings.TrimSpace(input.PipelineKey)
	if input.PipelineKey == "" {
		input.PipelineKey = strings.TrimSpace(defaults.DefaultPipeline)
	}
	if input.PipelineKey == "" {
		input.PipelineKey = strings.TrimSpace(s.config.DefaultPipeline)
	}
	if input.Preferences.DurationSeconds == 0 {
		input.Preferences.DurationSeconds = defaults.Preferences.DurationSeconds
	}
	if input.Preferences.Style == "" {
		input.Preferences.Style = defaults.Preferences.Style
	}
	if input.Preferences.MusicPrompt == "" {
		input.Preferences.MusicPrompt = defaults.Preferences.MusicPrompt
	}
	if input.Preferences.SubtitleMode == "" {
		input.Preferences.SubtitleMode = defaults.Preferences.SubtitleMode
	}
	if input.Preferences.VoiceoverMode == "" {
		input.Preferences.VoiceoverMode = defaults.Preferences.VoiceoverMode
	}
	if input.DeliveryTargets == nil {
		input.DeliveryTargets = slices.Clone(defaults.DeliveryTargets)
		if input.DeliveryTargets == nil {
			input.DeliveryTargets = []string{}
		}
	}

	capability, allowed := s.pipeline(input.PipelineKey)
	if input.Preferences.DurationSeconds == 0 {
		input.Preferences.DurationSeconds = capability.RecommendedDurationSeconds
	}
	if !s.config.Enabled {
		return fmt.Errorf("%w: montage is disabled", ErrMontageInput)
	}
	if !allowed {
		return fmt.Errorf("%w: pipeline_key %q is not allowed", ErrMontageInput, input.PipelineKey)
	}
	if input.Preferences.DurationSeconds < 1 || input.Preferences.DurationSeconds > s.config.MaxDurationSeconds {
		return fmt.Errorf("%w: duration_seconds must be between 1 and %d", ErrMontageInput, s.config.MaxDurationSeconds)
	}
	if len(input.SourceAssets) > s.config.MaxAssets {
		return fmt.Errorf("%w: source_assets cannot exceed %d items", ErrMontageInput, s.config.MaxAssets)
	}
	if err := validateMontageRequiredSources(capability.SourceRequirement, input.SourceAssets); err != nil {
		return fmt.Errorf("%w: %v", ErrMontageInput, err)
	}
	return nil
}

func (s *MontageCapabilityService) ValidateProjectDefaults(defaults model.MontageDefaults) error {
	if !s.config.Enabled {
		return fmt.Errorf("%w: montage is disabled", ErrProjectMontageDefaults)
	}
	key := strings.TrimSpace(defaults.DefaultPipeline)
	if key != "" {
		if _, allowed := s.pipeline(key); !allowed {
			return fmt.Errorf("%w: default_pipeline %q is not allowed", ErrProjectMontageDefaults, key)
		}
	}
	duration := defaults.Preferences.DurationSeconds
	if duration < 0 || duration > s.config.MaxDurationSeconds {
		return fmt.Errorf("%w: montage duration_seconds must be between 0 and %d", ErrProjectMontageDefaults, s.config.MaxDurationSeconds)
	}
	return nil
}

type montageSourceTaskTrust struct {
	taskID         string
	projectID      string
	includeLineage bool
}

// ValidateMontageSourceTaskFiles verifies task-file locators before they are
// persisted into a new Montage task or plan. A task file is reusable within its
// owning project, from an exact clone source, or from the persisted root lineage.
func ValidateMontageSourceTaskFiles(ctx context.Context, repo repository.Repository, userID, projectID string, assets []model.MontageAsset, trustedSources ...montageSourceTaskTrust) error {
	for _, asset := range assets {
		fileID := strings.TrimSpace(asset.TaskFileID)
		if fileID == "" {
			continue
		}
		if repo == nil {
			return fmt.Errorf("%w: task_file_id %q cannot be verified", ErrMontageInput, fileID)
		}
		file, err := repo.TaskFiles().FindByID(ctx, fileID)
		if err != nil || file == nil {
			return fmt.Errorf("%w: source_assets.task_file_id %q was not found", ErrMontageInput, fileID)
		}
		task, err := repo.Tasks().FindByID(ctx, file.TaskID)
		if err != nil || task == nil || task.UserID != userID {
			return fmt.Errorf("%w: source_assets.task_file_id %q is not owned by the current user", ErrMontageInput, fileID)
		}
		if !montageSourceTaskAuthorized(task, projectID, trustedSources...) {
			return fmt.Errorf("%w: source_assets.task_file_id %q is not authorized for this project", ErrMontageInput, fileID)
		}
		if !montageSourceTaskFileTypeMatches(asset.Type, file.MimeType) {
			return fmt.Errorf("%w: source_assets.task_file_id %q does not match asset type %q", ErrMontageInput, fileID, asset.Type)
		}
	}
	return nil
}

func ValidateMaterializableMontageSourceTaskFiles(ctx context.Context, repo repository.Repository, expectedStorageProvider, userID, projectID string, assets []model.MontageAsset, trustedSources ...montageSourceTaskTrust) error {
	if err := ValidateMontageSourceTaskFiles(ctx, repo, userID, projectID, assets, trustedSources...); err != nil {
		return err
	}
	totalBytes := int64(0)
	for _, asset := range assets {
		fileID := strings.TrimSpace(asset.TaskFileID)
		if fileID == "" {
			continue
		}
		file, err := repo.TaskFiles().FindByID(ctx, fileID)
		if err != nil || file == nil {
			return fmt.Errorf("%w: source_assets.task_file_id %q is unavailable", ErrMontageInput, fileID)
		}
		if err := validateMontageSourceTaskFileMaterializable(file, expectedStorageProvider); err != nil {
			return fmt.Errorf("%w: source_assets.task_file_id %q %v", ErrMontageInput, fileID, err)
		}
		if file.FileSize > montageSourceTotalMaxBytes-totalBytes {
			return fmt.Errorf("%w: task-file source assets cannot exceed %d bytes in total", ErrMontageInput, montageSourceTotalMaxBytes)
		}
		totalBytes += file.FileSize
	}
	return nil
}

func validateMontageSourceTaskFileMaterializable(file *model.TaskFile, expectedStorageProvider string) error {
	if file == nil {
		return errors.New("is unavailable")
	}
	expectedStorageProvider = strings.TrimSpace(expectedStorageProvider)
	if expectedStorageProvider == "" || file.StorageProvider != expectedStorageProvider {
		return errors.New("uses an unavailable storage provider")
	}
	objectKey := strings.TrimSpace(file.OSSKey)
	if objectKey == "" || objectKey != file.OSSKey {
		return errors.New("has no canonical storage object")
	}
	if file.FileSize <= 0 || file.FileSize > montageSourceMaxBytes {
		return fmt.Errorf("must be between 1 and %d bytes", montageSourceMaxBytes)
	}
	contentHash := strings.TrimSpace(file.ContentHash)
	if contentHash != "" && (contentHash != file.ContentHash || !lowercaseSHA256.MatchString(contentHash)) {
		return errors.New("has invalid integrity metadata")
	}
	return nil
}

func montageSourceTaskAuthorized(sourceTask *model.Task, destinationProjectID string, trustedSources ...montageSourceTaskTrust) bool {
	if sourceTask == nil {
		return false
	}
	if sourceTask.ProjectID == destinationProjectID {
		return true
	}
	for _, trusted := range trustedSources {
		trusted.taskID = strings.TrimSpace(trusted.taskID)
		trusted.projectID = strings.TrimSpace(trusted.projectID)
		if trusted.taskID == "" {
			continue
		}
		if sourceTask.ID == trusted.taskID && (trusted.projectID == "" || sourceTask.ProjectID == trusted.projectID) {
			return true
		}
		if trusted.includeLineage && sourceTask.InputSourceTaskID == trusted.taskID &&
			(trusted.projectID == "" || sourceTask.InputSourceProjectID == trusted.projectID) {
			return true
		}
	}
	return false
}

func montageSourceTaskFileTypeMatches(assetType, rawMIME string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(rawMIME))
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.SplitN(rawMIME, ";", 2)[0]))
	}
	mediaType = strings.ToLower(mediaType)
	switch strings.TrimSpace(assetType) {
	case "video", "video_url":
		return strings.HasPrefix(mediaType, "video/")
	case "audio", "audio_url":
		return strings.HasPrefix(mediaType, "audio/")
	case "image", "image_url":
		return strings.HasPrefix(mediaType, "image/")
	default:
		return true
	}
}

func (s *MontageCapabilityService) pipeline(key string) (MontagePipelineCapability, bool) {
	key = strings.TrimSpace(key)
	allowed := false
	for _, rawKey := range s.config.AllowedPipelines {
		if strings.TrimSpace(rawKey) == key {
			allowed = true
			break
		}
	}
	if !allowed {
		return customMontagePipelineCapability(key), false
	}
	item, ok := montagePipelineCapabilities[key]
	if !ok {
		item = customMontagePipelineCapability(key)
	}
	return item, true
}

func validateMontageRequiredSources(requirement MontageSourceRequirement, assets []model.MontageAsset) error {
	hasVideo, hasAudio := false, false
	for _, asset := range assets {
		if strings.TrimSpace(asset.URL) == "" && strings.TrimSpace(asset.TaskFileID) == "" {
			continue
		}
		switch strings.TrimSpace(asset.Type) {
		case "video", "video_url":
			hasVideo = true
		case "audio", "audio_url":
			hasAudio = true
		}
	}
	switch requirement {
	case MontageSourceRequirementVideo:
		if !hasVideo {
			return fmt.Errorf("selected pipeline requires a video source")
		}
	case MontageSourceRequirementVideoOrAudio:
		if !hasVideo && !hasAudio {
			return fmt.Errorf("selected pipeline requires a video or audio source")
		}
	}
	return nil
}
