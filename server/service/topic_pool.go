package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// TopicPoolService manages the topic pool per channel.
type TopicPoolService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewTopicPoolService creates a new TopicPoolService.
func NewTopicPoolService(repo repository.Repository, logger *zerolog.Logger) *TopicPoolService {
	return &TopicPoolService{repo: repo, logger: logger}
}

const maxTopicLength = 500

// Add creates one or more topics in the pool for the given channel.
func (s *TopicPoolService) Add(ctx context.Context, userID, channelID string, topics []string) ([]*model.TopicPool, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("at least one topic is required")
	}
	if err := s.validateChannelOwnership(ctx, userID, channelID); err != nil {
		return nil, err
	}

	items := make([]*model.TopicPool, 0, len(topics))
	for _, t := range topics {
		if t == "" {
			continue
		}
		if len([]rune(t)) > maxTopicLength {
			return nil, fmt.Errorf("topic exceeds %d character limit: %.50q...", maxTopicLength, t)
		}
		items = append(items, &model.TopicPool{
			UserID:    userID,
			ChannelID: channelID,
			Topic:     t,
			Status:    model.TopicStatusUnused,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one non-empty topic is required")
	}

	if err := s.repo.TopicPools().CreateBatch(ctx, items); err != nil {
		return nil, fmt.Errorf("create topics: %w", err)
	}
	return items, nil
}

// List returns topics for a channel with optional status filter and pagination.
func (s *TopicPoolService) List(ctx context.Context, userID, channelID, status string, offset, limit int) ([]*model.TopicPool, int64, error) {
	if err := s.validateChannelOwnership(ctx, userID, channelID); err != nil {
		return nil, 0, err
	}
	return s.repo.TopicPools().FindByChannel(ctx, channelID, status, offset, limit)
}

// Claim atomically claims the next unused topic for the channel.
// Returns the topic text and its ID, or empty string/0 if none available.
func (s *TopicPoolService) Claim(ctx context.Context, userID, channelID string) (string, uint, error) {
	topic, err := s.repo.TopicPools().ClaimOne(ctx, userID, channelID)
	if err != nil {
		return "", 0, fmt.Errorf("claim topic: %w", err)
	}
	if topic == nil {
		return "", 0, nil
	}
	s.logger.Info().Uint("topic_id", topic.ID).Str("channel_id", channelID).Str("topic", topic.Topic).Msg("claimed topic from pool")
	return topic.Topic, topic.ID, nil
}

// ClaimForTask atomically claims a topic and associates it with the given task ID.
func (s *TopicPoolService) ClaimForTask(ctx context.Context, userID, channelID, taskID string) (string, error) {
	topic, err := s.repo.TopicPools().ClaimWithTask(ctx, userID, channelID, taskID)
	if err != nil {
		return "", fmt.Errorf("claim topic: %w", err)
	}
	if topic == nil {
		return "", nil
	}
	s.logger.Info().Uint("topic_id", topic.ID).Str("channel_id", channelID).Str("task_id", taskID).Msg("claimed topic from pool for task")
	return topic.Topic, nil
}

// Reset marks a used topic as unused again.
func (s *TopicPoolService) Reset(ctx context.Context, userID, channelID string, id uint) error {
	topic, err := s.repo.TopicPools().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find topic: %w", err)
	}
	if topic.UserID != userID {
		return fmt.Errorf("topic not owned by user")
	}
	if topic.ChannelID != channelID {
		return fmt.Errorf("topic does not belong to this channel")
	}
	if topic.Status != model.TopicStatusUsed {
		return fmt.Errorf("only used topics can be reset")
	}
	return s.repo.TopicPools().ResetStatus(ctx, id)
}

// Delete removes a topic from the pool.
func (s *TopicPoolService) Delete(ctx context.Context, userID, channelID string, id uint) error {
	topic, err := s.repo.TopicPools().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find topic: %w", err)
	}
	if topic.UserID != userID {
		return fmt.Errorf("topic not owned by user")
	}
	if topic.ChannelID != channelID {
		return fmt.Errorf("topic does not belong to this channel")
	}
	return s.repo.TopicPools().Delete(ctx, id)
}

func (s *TopicPoolService) validateChannelOwnership(ctx context.Context, userID, channelID string) error {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return fmt.Errorf("channel not owned by user")
	}
	return nil
}
