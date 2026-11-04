package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

type topicNotificationPayload struct {
	TopicURL      string                 `json:"topic_url,omitempty"`
	Title         string                 `json:"title"`
	Introduction  string                 `json:"introduction"`
	SourceSummary string                 `json:"source_summary"`
	SourceRefs    []model.TopicSourceRef `json:"source_refs"`
	VideoFit      string                 `json:"video_fit,omitempty"`
}

func (s *TopicRadarService) QueueDigestNotifications(ctx context.Context, digestID string) ([]*model.TopicNotificationDelivery, error) {
	digest, err := s.repo.TopicDigests().FindByID(ctx, digestID)
	if err != nil {
		return nil, err
	}
	if digest.Status != model.TopicDigestStatusSucceeded {
		return nil, errors.New("topic digest is not successful")
	}
	channels, err := s.repo.TopicNotificationChannels().ListByProject(ctx, digest.UserID, digest.ProjectID)
	if err != nil {
		return nil, err
	}
	suggestions, err := s.repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		return nil, err
	}
	payloads := make([]topicNotificationPayload, 0, len(suggestions))
	for _, suggestion := range suggestions {
		var refs []model.TopicSourceRef
		if err := json.Unmarshal(suggestion.SourceRefs, &refs); err != nil {
			return nil, err
		}
		topicURL := ""
		if s.publicBaseURL != "" {
			topicURL = s.publicBaseURL + "/topics?" + url.Values{"project_id": {digest.ProjectID}, "asset_id": {suggestion.ContentAssetID}}.Encode()
		}
		payloads = append(payloads, topicNotificationPayload{TopicURL: topicURL, Title: suggestion.Title, Introduction: suggestion.Introduction, SourceSummary: suggestion.SourceSummary, SourceRefs: refs, VideoFit: suggestion.VideoFit})
	}
	payload, err := json.Marshal(map[string]any{"digest_id": digest.ID, "generated_at": digest.FinishedAt, "suggestions": payloads})
	if err != nil {
		return nil, err
	}
	created := make([]*model.TopicNotificationDelivery, 0, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		if err := validateTopicNotificationProvider(channel.Provider, channel.WebhookURL); err != nil {
			// A legacy/manually edited channel must not block fan-out to the
			// other valid channels; its configuration error stays visible in
			// channel management and the test action.
			continue
		}
		existing, findErr := s.repo.TopicNotificationDeliveries().FindByDigestChannel(ctx, digest.ID, channel.ID)
		if findErr == nil {
			created = append(created, existing)
			continue
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, findErr
		}
		delivery := &model.TopicNotificationDelivery{ID: uuid.NewString(), DigestID: digest.ID, ChannelID: channel.ID, PayloadVersion: "v1", Payload: payload, Status: model.TopicNotificationPending}
		created = append(created, delivery)
	}
	if len(created) == 0 {
		return created, nil
	}
	pending := make([]*model.TopicNotificationDelivery, 0, len(created))
	for _, item := range created {
		if item.CreatedAt.IsZero() {
			pending = append(pending, item)
		}
	}
	if len(pending) > 0 {
		if err := s.repo.TopicNotificationDeliveries().CreateBatch(ctx, pending); err != nil {
			return nil, err
		}
	}
	for i, item := range created {
		if item.CreatedAt.IsZero() {
			persisted, findErr := s.repo.TopicNotificationDeliveries().FindByDigestChannel(ctx, item.DigestID, item.ChannelID)
			if findErr != nil {
				return nil, findErr
			}
			created[i] = persisted
		}
	}
	return created, nil
}

func validateTopicNotificationProvider(provider, webhookURL string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return errors.New("notification webhook is required")
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case model.TopicNotificationProviderFeishu, model.TopicNotificationProviderWeCom, model.TopicNotificationProviderDingTalk:
		return nil
	default:
		return fmt.Errorf("unsupported notification provider %q", provider)
	}
}
