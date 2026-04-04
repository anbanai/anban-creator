package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

var (
	ErrChannelNotFound    = errors.New("channel not found")
	ErrChannelOwnedByUser = errors.New("channel not owned by user")
)

// validPlatforms defines the allowed platform values.
var validPlatforms = map[string]bool{
	model.PlatformArticle: true,
	model.PlatformXLS:     true,
	model.PlatformRednote: true,
}

// ChannelService handles channel CRUD operations with ownership verification.
type ChannelService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewChannelService creates a new ChannelService.
func NewChannelService(repo repository.Repository, logger *zerolog.Logger) *ChannelService {
	return &ChannelService{repo: repo, logger: logger}
}

// Create creates a new channel for the given user.
// It sets UserID and Status, validates the platform, then persists via the repository.
func (s *ChannelService) Create(ctx context.Context, userID string, ch *model.Channel) (*model.Channel, error) {
	if !validPlatforms[ch.Platform] {
		return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
	}

	ch.ID = uuid.New().String()
	ch.UserID = userID
	ch.Status = model.ChannelStatusActive

	if err := s.repo.Channels().Create(ctx, ch); err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}

	return ch, nil
}

// Get returns a channel and its stats, verifying that the channel belongs to the user.
func (s *ChannelService) Get(ctx context.Context, userID, channelID string) (*model.Channel, *repository.ChannelStats, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrChannelNotFound, err)
	}
	if ch.UserID != userID {
		return nil, nil, ErrChannelOwnedByUser
	}

	stats, err := s.repo.Channels().GetStats(ctx, channelID)
	if err != nil {
		s.logger.Error().Err(err).Str("channel_id", channelID).Msg("failed to get channel stats")
		// Return nil stats rather than failing the whole request.
		return ch, nil, nil
	}

	return ch, stats, nil
}

// List returns channels for the given user, filtered by the provided options.
func (s *ChannelService) List(ctx context.Context, userID string, opts repository.ChannelListOptions) ([]*model.Channel, error) {
	channels, err := s.repo.Channels().ListByUserID(ctx, userID, opts)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channels, nil
}

// Update updates mutable fields on a channel owned by the user.
func (s *ChannelService) Update(ctx context.Context, userID, channelID string, ch *model.Channel) (*model.Channel, error) {
	existing, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChannelNotFound, err)
	}
	if existing.UserID != userID {
		return nil, ErrChannelOwnedByUser
	}

	// Apply updatable fields from ch to existing.
	if ch.Name != "" {
		existing.Name = ch.Name
	}
	if ch.Platform != "" {
		if !validPlatforms[ch.Platform] {
			return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
		}
		existing.Platform = ch.Platform
	}
	existing.AvatarURL = ch.AvatarURL
	existing.Description = ch.Description
	existing.WechatAppID = ch.WechatAppID
	existing.WechatSecret = ch.WechatSecret
	existing.Keywords = ch.Keywords
	existing.Positioning = ch.Positioning
	existing.Style = ch.Style
	existing.Theme = ch.Theme
	existing.Author = ch.Author

	if err := s.repo.Channels().Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}

	return existing, nil
}

// Archive sets a channel's status to "archived" after verifying ownership.
func (s *ChannelService) Archive(ctx context.Context, userID, channelID string) error {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelNotFound, err)
	}
	if ch.UserID != userID {
		return ErrChannelOwnedByUser
	}

	if err := s.repo.Channels().UpdateStatus(ctx, channelID, model.ChannelStatusArchived); err != nil {
		return fmt.Errorf("archive channel: %w", err)
	}
	return nil
}

// Restore sets a channel's status back to "active" after verifying ownership.
func (s *ChannelService) Restore(ctx context.Context, userID, channelID string) error {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelNotFound, err)
	}
	if ch.UserID != userID {
		return ErrChannelOwnedByUser
	}

	if err := s.repo.Channels().UpdateStatus(ctx, channelID, model.ChannelStatusActive); err != nil {
		return fmt.Errorf("restore channel: %w", err)
	}
	return nil
}

// Delete permanently removes a channel after verifying ownership.
func (s *ChannelService) Delete(ctx context.Context, userID, channelID string) error {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelNotFound, err)
	}
	if ch.UserID != userID {
		return ErrChannelOwnedByUser
	}

	// Check if the channel has associated tasks.
	stats, err := s.repo.Channels().GetStats(ctx, channelID)
	if err != nil {
		s.logger.Warn().Err(err).Str("channel_id", channelID).Msg("failed to get channel stats before delete")
	}
	if stats != nil && stats.TotalTasks > 0 {
		return fmt.Errorf("cannot delete channel with %d associated tasks; archive it instead", stats.TotalTasks)
	}

	if err := s.repo.Channels().Delete(ctx, channelID); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	return nil
}
