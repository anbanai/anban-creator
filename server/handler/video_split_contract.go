package handler

import (
	"encoding/json"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
)

func rejectLegacyVideoFields(body []byte) error {
	if hasJSONField(body, "video_input") || hasJSONField(body, "video_config") {
		return fmt.Errorf("video_input/video_config are not accepted; use video_creator_input/video_creator_config or video_editor_input/video_editor_config")
	}
	return nil
}

func rejectPlanVideoFields(body []byte) error {
	if err := rejectLegacyVideoFields(body); err != nil {
		return err
	}
	if hasJSONField(body, "video_editor_input") || hasJSONField(body, "video_editor_config") {
		return fmt.Errorf("video_editor_input/video_editor_config are not accepted on plans; use video_creator_input/video_creator_config")
	}
	return nil
}

func rejectVideoCreatorOnlyFields(body []byte) error {
	if err := rejectLegacyVideoFields(body); err != nil {
		return err
	}
	if hasJSONField(body, "video_editor_input") || hasJSONField(body, "video_editor_config") {
		return fmt.Errorf("video_editor_input/video_editor_config are not accepted on video creator requests; use video_creator_input/video_creator_config")
	}
	return nil
}

func validateSplitVideoTaskFields(creatorCfg *model.VideoTaskConfig, creatorInput *model.VideoInput, editorCfg *model.VideoTaskConfig, editorInput *model.VideoInput) error {
	hasCreator := creatorCfg != nil || creatorInput != nil
	hasEditor := editorCfg != nil || editorInput != nil
	if hasCreator && hasEditor {
		return fmt.Errorf("video_creator_* and video_editor_* fields cannot be set on the same task")
	}
	return nil
}

func splitVideoReferenceURLs(creatorCfg *model.VideoTaskConfig, creatorInput *model.VideoInput, editorCfg *model.VideoTaskConfig, editorInput *model.VideoInput) []string {
	var urls []string
	for _, cfg := range []*model.VideoTaskConfig{
		videoConfigForReferenceURLs(creatorCfg, creatorInput),
		videoConfigForReferenceURLs(editorCfg, editorInput),
	} {
		urls = append(urls, videoReferenceURLs(cfg)...)
	}
	return urls
}

func montageSourceAssetURLs(input *model.MontageInput) []string {
	if input == nil || len(input.SourceAssets) == 0 {
		return nil
	}
	urls := make([]string, 0, len(input.SourceAssets))
	for _, asset := range input.SourceAssets {
		if asset.URL != "" {
			urls = append(urls, asset.URL)
		}
	}
	return urls
}

func rewriteFinalizedVideoReferenceURLs(rewrites map[string]string, creatorCfg *model.VideoTaskConfig, creatorInput *model.VideoInput, editorCfg *model.VideoTaskConfig, editorInput *model.VideoInput) {
	for _, cfg := range []*model.VideoTaskConfig{creatorCfg, editorCfg} {
		if cfg == nil {
			continue
		}
		for i := range cfg.References {
			cfg.References[i].URL = rewriteFinalizedUploadURL(cfg.References[i].URL, rewrites)
		}
	}
	for _, input := range []*model.VideoInput{creatorInput, editorInput} {
		if input == nil {
			continue
		}
		for i := range input.References {
			input.References[i].URL = rewriteFinalizedUploadURL(input.References[i].URL, rewrites)
		}
	}
}

func rewriteFinalizedMontageAssetURLs(input *model.MontageInput, rewrites map[string]string) {
	if input == nil {
		return
	}
	for i := range input.SourceAssets {
		input.SourceAssets[i].URL = rewriteFinalizedUploadURL(input.SourceAssets[i].URL, rewrites)
	}
}

func validateMontageSourceAssetURLs(input *model.MontageInput) error {
	for _, url := range montageSourceAssetURLs(input) {
		if !validReferenceImageURL(url) {
			return fmt.Errorf("montage_input.source_assets.url must be an internal file path or an http(s) URL")
		}
	}
	return nil
}

func taskAPIResponses(tasks []*model.Task) []map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskAPIResponse(task))
	}
	return items
}

func taskAPIResponse(task *model.Task) map[string]any {
	if task == nil {
		return nil
	}
	resp := modelAPIMap(task)
	rewriteVideoAPIFields(resp, task.Type, task.VideoInput.Data(), task.VideoConfig.Data())
	rewriteMontageAPIField(resp, task.Type, task.MontageInput.Data())
	return resp
}

func planAPIResponses(plans []*model.Plan) []map[string]any {
	items := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		items = append(items, planAPIResponse(plan))
	}
	return items
}

func planAPIResponse(plan *model.Plan) map[string]any {
	if plan == nil {
		return nil
	}
	resp := modelAPIMap(plan)
	rewriteVideoAPIFields(resp, plan.Type, plan.VideoInput.Data(), plan.VideoConfig.Data())
	rewriteMontageAPIField(resp, plan.Type, plan.MontageInput.Data())
	return resp
}

func modelAPIMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return map[string]any{}
	}
	return resp
}

func rewriteVideoAPIFields(resp map[string]any, platform string, input model.VideoInput, cfg model.VideoTaskConfig) {
	delete(resp, "video_input")
	delete(resp, "video_config")
	switch {
	case model.IsVideoCreatorPlatform(platform):
		resp["video_creator_input"] = input
		resp["video_creator_config"] = cfg
	case model.IsVideoEditorPlatform(platform):
		resp["video_editor_input"] = input
		resp["video_editor_config"] = cfg
	}
}

func rewriteMontageAPIField(resp map[string]any, platform string, input model.MontageInput) {
	delete(resp, "montage_input")
	if model.IsMontagePlatform(platform) {
		resp["montage_input"] = input
	}
}
