package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
)

type CreateTopicNotificationChannelParams struct {
	UserID     string
	ProjectID  string
	Provider   string
	Name       string
	WebhookURL string
	Enabled    bool
}

func (s *TopicRadarService) CreateNotificationChannel(ctx context.Context, p CreateTopicNotificationChannelParams) (*model.TopicNotificationChannel, error) {
	if strings.TrimSpace(p.UserID) == "" || strings.TrimSpace(p.ProjectID) == "" || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.WebhookURL) == "" {
		return nil, fmt.Errorf("%w: user_id, project_id, name and webhook_url are required", ErrTopicSubscriptionInvalid)
	}
	if _, err := s.projectForUser(ctx, p.UserID, p.ProjectID); err != nil {
		return nil, err
	}
	if err := validateTopicNotificationProvider(p.Provider, p.WebhookURL); err != nil {
		return nil, fmt.Errorf("%w: invalid notification provider", ErrTopicSubscriptionInvalid)
	}
	if err := validateTopicNotificationWebhookURL(ctx, p.WebhookURL); err != nil {
		return nil, fmt.Errorf("%w: invalid webhook URL", ErrTopicSubscriptionInvalid)
	}
	item := &model.TopicNotificationChannel{ID: uuid.NewString(), UserID: p.UserID, ProjectID: p.ProjectID, Provider: strings.ToLower(strings.TrimSpace(p.Provider)), Name: strings.TrimSpace(p.Name), WebhookURL: strings.TrimSpace(p.WebhookURL), Enabled: p.Enabled}
	if err := s.repo.TopicNotificationChannels().Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
