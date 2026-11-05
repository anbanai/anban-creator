package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

type topicScriptArtifact struct {
	topicArtifactEnvelope
	Target *struct {
		Platform        string `json:"platform"`
		DurationSeconds int    `json:"duration_seconds"`
		AspectRatio     string `json:"aspect_ratio"`
	} `json:"target"`
	SourceRefs    []model.TopicSourceRef `json:"source_refs"`
	SchemaVersion string                 `json:"schema_version"`
	Title         string                 `json:"title"`
	Hook          string                 `json:"hook"`
	Scenes        []struct {
		SourceRefs      []model.TopicSourceRef `json:"source_refs"`
		DurationSeconds int                    `json:"duration_seconds"`
		Visual          string                 `json:"visual"`
		Voiceover       string                 `json:"voiceover"`
	} `json:"scenes"`
	CTA string `json:"cta"`
}

func (s *TopicRadarService) FinalizeTopicScript(ctx context.Context, jobID string, artifact []byte, artifactPath string) error {
	job, err := s.repo.TopicScriptJobs().FindByID(ctx, jobID)
	if err != nil {
		return err
	}
	return s.finalizeOwnedTopicScript(ctx, job, artifact, artifactPath)
}

func (s *TopicRadarService) finalizeOwnedTopicScript(ctx context.Context, job *model.TopicScriptJob, artifact []byte, artifactPath string) error {
	if job.Status == model.TopicScriptJobSucceeded || job.Status == model.TopicScriptJobFailed {
		return nil
	}
	var parsed topicScriptArtifact
	if err := json.Unmarshal(artifact, &parsed); err != nil {
		return s.failTopicScript(ctx, job, fmt.Errorf("invalid script artifact: %w", err))
	}
	if parsed.SchemaVersion != "1.0" || strings.TrimSpace(parsed.Title) == "" || strings.TrimSpace(parsed.Hook) == "" || len(parsed.Scenes) == 0 || strings.TrimSpace(parsed.CTA) == "" {
		return s.failTopicScript(ctx, job, errors.New("invalid script artifact contract"))
	}
	if err := parsed.topicArtifactEnvelope.validate("topic_script", job.TaskID, job.ExecutionID); err != nil {
		return s.failTopicScript(ctx, job, err)
	}
	if parsed.Target == nil || parsed.Target.Platform != job.TargetPlatform || parsed.Target.DurationSeconds != job.DurationSeconds || parsed.Target.AspectRatio != job.AspectRatio {
		return s.failTopicScript(ctx, job, errors.New("script target does not match request"))
	}
	if len(parsed.SourceRefs) == 0 {
		return s.failTopicScript(ctx, job, errors.New("script source references are required"))
	}
	total := 0
	refs := append([]model.TopicSourceRef(nil), parsed.SourceRefs...)
	for _, scene := range parsed.Scenes {
		total += scene.DurationSeconds
		refs = append(refs, scene.SourceRefs...)
		if scene.DurationSeconds <= 0 || strings.TrimSpace(scene.Visual) == "" || strings.TrimSpace(scene.Voiceover) == "" {
			return s.failTopicScript(ctx, job, errors.New("invalid script scene"))
		}
	}
	if total != job.DurationSeconds {
		return s.failTopicScript(ctx, job, errors.New("script scene duration does not match requested duration"))
	}
	if strings.TrimSpace(artifactPath) == "" {
		return s.failTopicScript(ctx, job, errors.New("script artifact path is required"))
	}
	if len(refs) > 0 {
		frozen, _ := frozenTopicSourceRefs(job.InputSnapshot)
		for _, ref := range refs {
			if _, found := canonicalTopicSourceRef(ref, frozen); !ref.Resolvable() || !found {
				return s.failTopicScript(ctx, job, errors.New("script source is outside frozen snapshot"))
			}
		}
	}
	hash := sha256.Sum256(artifact)
	now := s.now()
	job.Status = model.TopicScriptJobSucceeded
	job.ArtifactPath = strings.TrimSpace(artifactPath)
	job.ArtifactSHA256 = hex.EncodeToString(hash[:])
	job.Error = ""
	job.FinishedAt = &now
	_, err := s.repo.TopicScriptJobs().UpdateIfNonterminal(ctx, job)
	return err
}

func (s *TopicRadarService) failTopicScript(ctx context.Context, job *model.TopicScriptJob, err error) error {
	now := s.now()
	job.Status = model.TopicScriptJobFailed
	job.Error = err.Error()
	job.FinishedAt = &now
	if _, updateErr := s.repo.TopicScriptJobs().UpdateIfNonterminal(ctx, job); updateErr != nil {
		return updateErr
	}
	return err
}
