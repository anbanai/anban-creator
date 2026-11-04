package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

// Freeze at request time; retries and delayed dispatch never re-read mutable persona/asset input.
func (s *TopicRadarService) freezeTopicScriptInput(ctx context.Context, asset *model.ContentAsset, project *model.Project, job *model.TopicScriptJob) ([]byte, error) {
	evidence, err := s.repo.ContentAssets().ListEvidence(ctx, job.UserID, asset.ID)
	if err != nil {
		return nil, err
	}
	refs := []model.TopicSourceRef{{Type: "asset", RefID: asset.ID, Title: asset.Title, Label: "选题输入"}}
	for _, e := range evidence {
		if e.SourceID != "" || e.URL != "" {
			kind := e.SourceKind
			if kind == "" {
				kind = "external"
			}
			refs = append(refs, model.TopicSourceRef{Type: kind, RefID: e.SourceID, URL: e.URL, Title: e.Title, Label: e.Notes})
		}
	}
	selected := map[string]any{"title": asset.Title, "introduction": asset.CoreQuestion, "source_summary": "根据选题与已记录来源创作；人工输入不代表事实已验证。", "source_refs": refs, "scores": map[string]float64{}, "video_fit": "按目标时长与平台创作"}
	frozen := map[string]any{"project": map[string]any{"id": project.ID, "name": project.Name, "platform": project.Platform, "instructions": project.Instructions, "keywords": project.Keywords, "profile": projectProfileFor(project)}, "asset": asset}
	target := map[string]any{"platform": job.TargetPlatform, "duration_seconds": job.DurationSeconds, "aspect_ratio": job.AspectRatio}
	data, err := json.Marshal(map[string]any{"selected_topic": selected, "target": target, "frozen_context": frozen})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return json.Marshal(map[string]any{"schema_version": "1.0", "task_id": "", "execution_id": "", "task_kind": model.TaskKindTopicScript, "snapshot": map[string]any{"snapshot_id": job.ID, "snapshot_hash": hex.EncodeToString(hash[:]), "captured_at": s.now().UTC().Format(time.RFC3339Nano), "project_id": job.ProjectID, "asset_id": asset.ID}, "source_records": refs, "selected_topic": selected, "target": target, "frozen_context": frozen})
}
