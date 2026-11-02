package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

// Every read/write surface resolves project access again; ownership of a child
// alone must not keep access alive after its project has been disabled.
func (s *TopicRadarService) ListSubscriptions(ctx context.Context, userID, projectID string) ([]*model.TopicSubscription, error) {
	if _, err := s.projectForUser(ctx, userID, projectID); err != nil {
		return nil, err
	}
	return s.repo.TopicSubscriptions().ListByProject(ctx, userID, projectID)
}
func (s *TopicRadarService) subscriptionForUserIncludingDeleted(ctx context.Context, userID, id string) (*model.TopicSubscription, error) {
	item, err := s.repo.TopicSubscriptions().FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.UserID != userID {
		return nil, ErrTopicProjectAccessDenied
	}
	if _, err = s.projectForUser(ctx, userID, item.ProjectID); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *TopicRadarService) subscriptionForUser(ctx context.Context, userID, id string) (*model.TopicSubscription, error) {
	item, err := s.subscriptionForUserIncludingDeleted(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if item.DeletedAt != nil {
		return nil, ErrTopicSubscriptionNotFound
	}
	return item, nil
}

func (s *TopicRadarService) DeleteSubscription(ctx context.Context, userID, id string) error {
	item, err := s.subscriptionForUserIncludingDeleted(ctx, userID, id)
	if err != nil {
		return err
	}
	if item.DeletedAt != nil {
		return nil
	}
	return s.repo.TopicSubscriptions().Delete(ctx, item.ID, item.UserID, item.ProjectID)
}
func (s *TopicRadarService) ListDigests(ctx context.Context, userID, subscriptionID string) ([]*model.TopicDigest, error) {
	if _, err := s.subscriptionForUserIncludingDeleted(ctx, userID, subscriptionID); err != nil {
		return nil, err
	}
	return s.repo.TopicDigests().ListBySubscription(ctx, subscriptionID, 50)
}

type UpdateTopicSubscriptionParams struct {
	Name         *string        `json:"name"`
	Enabled      *bool          `json:"enabled"`
	Timezone     *string        `json:"timezone"`
	Schedule     datatypes.JSON `json:"schedule"`
	SourceConfig datatypes.JSON `json:"source_config"`
	Weights      datatypes.JSON `json:"weights"`
	ScriptPolicy datatypes.JSON `json:"script_policy"`
}

func (s *TopicRadarService) UpdateSubscription(ctx context.Context, userID, id string, p UpdateTopicSubscriptionParams) (*model.TopicSubscription, error) {
	item, err := s.subscriptionForUser(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	fields := make([]string, 0, 8)
	if p.Name != nil {
		fields = append(fields, "name")
		item.Name = strings.TrimSpace(*p.Name)
	}
	if p.Enabled != nil {
		fields = append(fields, "enabled")
		item.Enabled = *p.Enabled
	}
	if p.Timezone != nil {
		fields = append(fields, "timezone")
		item.Timezone = strings.TrimSpace(*p.Timezone)
	}
	if p.Schedule != nil {
		fields = append(fields, "schedule")
		item.Schedule = p.Schedule
	}
	if p.SourceConfig != nil {
		fields = append(fields, "source_config")
		item.SourceConfig = p.SourceConfig
	}
	if p.Weights != nil {
		fields = append(fields, "weights")
		item.Weights = p.Weights
	}
	if p.ScriptPolicy != nil {
		fields = append(fields, "script_policy")
		item.ScriptPolicy = p.ScriptPolicy
	}
	if item.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTopicSubscriptionInvalid)
	}
	if err = validateTopicSubscription(item, s.now()); err != nil {
		return nil, err
	}
	if !item.Enabled {
		item.NextRunAt = nil
	}
	if p.Enabled != nil || p.Schedule != nil || p.Timezone != nil {
		fields = append(fields, "next_run_at")
	}
	if err = s.repo.TopicSubscriptions().Patch(ctx, item, fields...); err != nil {
		return nil, err
	}
	return s.repo.TopicSubscriptions().FindByID(ctx, item.ID)
}

type TopicDigestDetail struct {
	Digest        *model.TopicDigest                 `json:"digest"`
	Suggestions   []*model.TopicSuggestion           `json:"suggestions"`
	Adoptions     []*model.TopicSuggestionAdoption   `json:"adoptions"`
	Scripts       []*model.TopicScriptJob            `json:"scripts"`
	Notifications []*model.TopicNotificationDelivery `json:"notifications"`
}

func (s *TopicRadarService) GetDigest(ctx context.Context, userID, id string) (*TopicDigestDetail, error) {
	digest, err := s.repo.TopicDigests().FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if digest.UserID != userID {
		return nil, ErrTopicProjectAccessDenied
	}
	if _, err = s.projectForUser(ctx, userID, digest.ProjectID); err != nil {
		return nil, err
	}
	detail := &TopicDigestDetail{Digest: digest}
	if detail.Suggestions, err = s.repo.TopicSuggestions().ListByDigest(ctx, id); err != nil {
		return nil, err
	}
	if detail.Adoptions, err = s.repo.TopicSuggestionAdoptions().ListByDigest(ctx, id); err != nil {
		return nil, err
	}
	if detail.Scripts, err = s.repo.TopicScriptJobs().ListByDigest(ctx, id); err != nil {
		return nil, err
	}
	if detail.Notifications, err = s.repo.TopicNotificationDeliveries().ListByDigest(ctx, id); err != nil {
		return nil, err
	}
	if detail.Suggestions == nil {
		detail.Suggestions = []*model.TopicSuggestion{}
	}
	if detail.Scripts == nil {
		detail.Scripts = []*model.TopicScriptJob{}
	}
	return detail, nil
}
func (s *TopicRadarService) GetScript(ctx context.Context, userID, id string) (*model.TopicScriptJob, error) {
	item, err := s.repo.TopicScriptJobs().FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.UserID != userID {
		return nil, ErrTopicProjectAccessDenied
	}
	if _, err = s.projectForUser(ctx, userID, item.ProjectID); err != nil {
		return nil, err
	}
	return item, nil
}
func (s *TopicRadarService) RetryScript(ctx context.Context, userID, id string) (*model.TopicScriptJob, error) {
	job, err := s.GetScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if job.Status != model.TopicScriptJobFailed {
		return nil, fmt.Errorf("%w: only failed scripts may be retried", ErrTopicSubscriptionInvalid)
	}
	return s.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: userID, SuggestionID: job.SuggestionID, TargetPlatform: job.TargetPlatform, DurationSeconds: job.DurationSeconds, AspectRatio: job.AspectRatio})
}

func (s *TopicRadarService) ListNotificationChannels(ctx context.Context, userID, projectID string) ([]*model.TopicNotificationChannel, error) {
	if _, err := s.projectForUser(ctx, userID, projectID); err != nil {
		return nil, err
	}
	return s.repo.TopicNotificationChannels().ListByProject(ctx, userID, projectID)
}
func (s *TopicRadarService) notificationChannelForUser(ctx context.Context, userID, id string) (*model.TopicNotificationChannel, error) {
	item, err := s.repo.TopicNotificationChannels().FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.UserID != userID {
		return nil, ErrTopicProjectAccessDenied
	}
	if _, err = s.projectForUser(ctx, userID, item.ProjectID); err != nil {
		return nil, err
	}
	return item, nil
}

type UpdateTopicNotificationChannelParams struct {
	Name       *string `json:"name"`
	Provider   *string `json:"provider"`
	WebhookURL *string `json:"webhook_url"`
	Enabled    *bool   `json:"enabled"`
}

func (s *TopicRadarService) UpdateNotificationChannel(ctx context.Context, userID, id string, p UpdateTopicNotificationChannelParams) (*model.TopicNotificationChannel, error) {
	item, err := s.notificationChannelForUser(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		item.Name = strings.TrimSpace(*p.Name)
	}
	if p.Provider != nil {
		item.Provider = strings.ToLower(strings.TrimSpace(*p.Provider))
	}
	// An omitted/blank secret preserves the current URL; it is never returned.
	if p.WebhookURL != nil && strings.TrimSpace(*p.WebhookURL) != "" {
		item.WebhookURL = strings.TrimSpace(*p.WebhookURL)
	}
	if p.Enabled != nil {
		item.Enabled = *p.Enabled
	}
	if item.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTopicSubscriptionInvalid)
	}
	if err = validateTopicNotificationProvider(item.Provider, item.WebhookURL); err != nil {
		return nil, fmt.Errorf("%w: invalid notification provider", ErrTopicSubscriptionInvalid)
	}
	if p.WebhookURL != nil && strings.TrimSpace(*p.WebhookURL) != "" {
		if err = validateTopicNotificationWebhookURL(ctx, item.WebhookURL); err != nil {
			return nil, fmt.Errorf("%w: invalid webhook URL", ErrTopicSubscriptionInvalid)
		}
	}
	if err = s.repo.TopicNotificationChannels().Update(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
func (s *TopicRadarService) DeleteNotificationChannel(ctx context.Context, userID, id string) error {
	if _, err := s.notificationChannelForUser(ctx, userID, id); err != nil {
		return err
	}
	return s.repo.TopicNotificationChannels().Delete(ctx, id)
}
