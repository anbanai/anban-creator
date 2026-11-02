package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

type RequestTopicScriptParams struct {
	UserID          string
	SuggestionID    string
	TargetPlatform  string
	DurationSeconds int
	AspectRatio     string
}

func (s *TopicRadarService) RequestTopicScript(ctx context.Context, p RequestTopicScriptParams) (*model.TopicScriptJob, error) {
	return s.requestTopicScript(ctx, p, true)
}

func (s *TopicRadarService) requestTopicScript(ctx context.Context, p RequestTopicScriptParams, retryFailed bool) (*model.TopicScriptJob, error) {
	if strings.TrimSpace(p.UserID) == "" || strings.TrimSpace(p.SuggestionID) == "" || strings.TrimSpace(p.TargetPlatform) == "" {
		return nil, fmt.Errorf("%w: user_id, suggestion_id and target_platform are required", ErrTopicSubscriptionInvalid)
	}
	p.TargetPlatform, p.AspectRatio = strings.TrimSpace(p.TargetPlatform), strings.TrimSpace(p.AspectRatio)
	if err := validateTopicScriptTarget(p.TargetPlatform, p.DurationSeconds, p.AspectRatio); err != nil {
		return nil, err
	}
	if p.DurationSeconds == 0 {
		p.DurationSeconds = 60
	}
	if strings.TrimSpace(p.AspectRatio) == "" {
		p.AspectRatio = "9:16"
	}
	suggestion, err := s.repo.TopicSuggestions().FindByID(ctx, p.SuggestionID)
	if err != nil {
		return nil, err
	}
	digest, err := s.repo.TopicDigests().FindByID(ctx, suggestion.DigestID)
	if err != nil {
		return nil, err
	}
	if digest.UserID != p.UserID || suggestion.ProjectID != digest.ProjectID {
		return nil, ErrTopicProjectAccessDenied
	}
	if _, err := s.projectForUser(ctx, p.UserID, digest.ProjectID); err != nil {
		return nil, err
	}
	if digest.Status != model.TopicDigestStatusSucceeded {
		return nil, fmt.Errorf("%w: digest must be completed", ErrTopicSubscriptionInvalid)
	}
	fingerprintBytes, err := json.Marshal(struct {
		SuggestionID string `json:"suggestion_id"`
		Platform     string `json:"platform"`
		Duration     int    `json:"duration"`
		AspectRatio  string `json:"aspect_ratio"`
	}{p.SuggestionID, strings.TrimSpace(p.TargetPlatform), p.DurationSeconds, strings.TrimSpace(p.AspectRatio)})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(fingerprintBytes)
	fingerprint := hex.EncodeToString(hash[:])
	if existing, findErr := s.repo.TopicScriptJobs().FindByFingerprint(ctx, fingerprint); findErr == nil {
		if existing.Status != model.TopicScriptJobFailed || !retryFailed {
			return existing, nil
		}
		// A failed fingerprint is the same logical request and can be retried
		// without creating a second durable job row.
		claimed, err := s.repo.TopicScriptJobs().RetryFailed(ctx, existing)
		if err != nil {
			return nil, err
		}
		current, err := s.repo.TopicScriptJobs().FindByID(ctx, existing.ID)
		if err != nil {
			return nil, err
		}
		if !claimed {
			return current, nil
		}
		return current, s.enqueueTopicScript(ctx, current)
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	job := &model.TopicScriptJob{
		ID:              uuid.NewString(),
		SuggestionID:    suggestion.ID,
		DigestID:        digest.ID,
		UserID:          p.UserID,
		ProjectID:       digest.ProjectID,
		TargetPlatform:  strings.TrimSpace(p.TargetPlatform),
		DurationSeconds: p.DurationSeconds,
		AspectRatio:     strings.TrimSpace(p.AspectRatio),
		Fingerprint:     fingerprint,
		Status:          model.TopicScriptJobQueued,
	}
	if err := s.repo.TopicScriptJobs().Create(ctx, job); err != nil {
		if existing, findErr := s.repo.TopicScriptJobs().FindByFingerprint(ctx, fingerprint); findErr == nil {
			return existing, nil
		}
		return nil, err
	}
	persisted, err := s.repo.TopicScriptJobs().FindByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	return persisted, s.enqueueTopicScript(ctx, persisted)
}

func (s *TopicRadarService) enqueueTopicScript(ctx context.Context, job *model.TopicScriptJob) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]string{"script_job_id": job.ID})
	if err != nil {
		return err
	}
	key := job.ID
	if job.Attempt > 0 {
		key = fmt.Sprintf("%s:%d", job.ID, job.Attempt)
	}
	_, err = s.enqueuer.EnqueueUnique(model.TaskTypeTopicScript, payload, key)
	// Keep the durable queued row on transport failure; recovery re-enqueues it.
	return err
}

// Empty target fields are policy defaults; explicit requests require a platform.
func validateTopicScriptTarget(platform string, duration int, aspect string) error {
	switch strings.TrimSpace(platform) {
	case "", "douyin", "channels", "bilibili", "xiaohongshu":
	default:
		return fmt.Errorf("%w: unsupported script platform", ErrTopicSubscriptionInvalid)
	}
	if duration < 0 || duration > 600 {
		return fmt.Errorf("%w: script duration must be 1–600 seconds (0 uses the default)", ErrTopicSubscriptionInvalid)
	}
	switch strings.TrimSpace(aspect) {
	case "", "9:16", "16:9", "1:1":
	default:
		return fmt.Errorf("%w: unsupported script aspect ratio", ErrTopicSubscriptionInvalid)
	}
	return nil
}
