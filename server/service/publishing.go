package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/draft"
	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// PublishingService handles WeChat draft publishing.
type PublishingService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewPublishingService creates a new PublishingService.
func NewPublishingService(repo repository.Repository, logger *zerolog.Logger) *PublishingService {
	return &PublishingService{
		repo:   repo,
		logger: logger,
	}
}

// DraftArticleInput holds the fields for a single article in a draft publish request.
type DraftArticleInput struct {
	Title            string `json:"title"`
	Author           string `json:"author,omitempty"`
	Digest           string `json:"digest,omitempty"`
	Content          string `json:"content,omitempty"`
	ThumbMediaID     string `json:"thumb_media_id,omitempty"`
	ShowCoverPic     int    `json:"show_cover_pic,omitempty"`
	ContentSourceURL string `json:"content_source_url,omitempty"`
}

// PublishDraftResult holds the result of a draft publish operation.
type PublishDraftResult struct {
	MediaID  string `json:"media_id"`
	DraftURL string `json:"draft_url,omitempty"`
}

// buildAppConfig creates an app config from a channel (nil image config since publishing doesn't need image API).
func (s *PublishingService) buildAppConfig(ch *model.Channel) (*appconfig.Config, error) {
	return agent.BuildAppConfig(ch, nil, "")
}

// createDraftService creates a draft.Service for the given channel.
func (s *PublishingService) createDraftService(ch *model.Channel) (*draft.Service, error) {
	appCfg, err := s.buildAppConfig(ch)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}
	return draft.NewService(appCfg, s.logger), nil
}

// getChannel retrieves and validates a channel for the given user.
func (s *PublishingService) getChannel(ctx context.Context, userID, channelID string) (*model.Channel, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch == nil {
		return nil, fmt.Errorf("channel not found")
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}
	if ch.GetWechatAppID() == "" || ch.GetWechatSecret() == "" {
		return nil, fmt.Errorf("channel missing WeChat credentials")
	}
	return ch, nil
}

// PublishDraft creates a WeChat article draft for the given channel.
func (s *PublishingService) PublishDraft(ctx context.Context, userID, channelID string, articles []DraftArticleInput) (*PublishDraftResult, error) {
	ch, err := s.getChannel(ctx, userID, channelID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftService(ch)
	if err != nil {
		return nil, err
	}

	// Convert inputs to draft.Article.
	draftArticles := make([]draft.Article, len(articles))
	for i, a := range articles {
		draftArticles[i] = draft.Article{
			Title:            a.Title,
			Author:           a.Author,
			Digest:           a.Digest,
			Content:          a.Content,
			ThumbMediaID:     a.ThumbMediaID,
			ShowCoverPic:     a.ShowCoverPic,
			ContentSourceURL: a.ContentSourceURL,
		}
	}

	result, err := ds.CreateDraft(draftArticles)
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}

	s.logger.Info().Str("user_id", userID).Str("channel_id", channelID).Str("media_id", result.MediaID).Msg("article draft published")

	return &PublishDraftResult{
		MediaID:  result.MediaID,
		DraftURL: result.DraftURL,
	}, nil
}

// ListDrafts returns a paginated list of WeChat drafts for the given channel.
func (s *PublishingService) ListDrafts(ctx context.Context, userID, channelID string, offset, count int64) (*draft.ListDraftsResult, error) {
	ch, err := s.getChannel(ctx, userID, channelID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftService(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListDrafts(offset, count)
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}

	return result, nil
}

// ListPublished returns a paginated list of published WeChat articles for the given channel.
func (s *PublishingService) ListPublished(ctx context.Context, userID, channelID string, offset, count int64) (*draft.ListPublishedResult, error) {
	ch, err := s.getChannel(ctx, userID, channelID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftService(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListPublished(offset, count)
	if err != nil {
		return nil, fmt.Errorf("list published: %w", err)
	}

	return result, nil
}
