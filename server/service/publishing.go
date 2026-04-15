package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/royalrick/anbanwriter/app/draft"
	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// PublishingService handles WeChat draft publishing (articles and Xiaolvshu image posts).
type PublishingService struct {
	repo      repository.Repository
	creditSvc *CreditService
	logger    *zerolog.Logger
}

// NewPublishingService creates a new PublishingService.
func NewPublishingService(repo repository.Repository, creditSvc *CreditService, logger *zerolog.Logger) *PublishingService {
	return &PublishingService{
		repo:      repo,
		creditSvc: creditSvc,
		logger:    logger,
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

// XlsPublishRequest holds the fields for a Xiaolvshu image post publish request.
type XlsPublishRequest struct {
	Title        string   `json:"title"`
	Content      string   `json:"content,omitempty"`
	Images       []string `json:"images,omitempty"`
	MediaIDs     []string `json:"media_ids,omitempty"`
	OpenComment  bool     `json:"open_comment,omitempty"`
	FansOnly     bool     `json:"fans_only,omitempty"`
	FromMarkdown string   `json:"from_markdown,omitempty"`
}

// XlsPublishResult holds the result of a Xiaolvshu image post publish operation.
type XlsPublishResult struct {
	MediaID     string   `json:"media_id"`
	DraftURL    string   `json:"draft_url,omitempty"`
	Count       int      `json:"count"`
	UploadedIDs []string `json:"uploaded_ids"`
}

// buildAppConfig creates an app config from a channel (nil image config since publishing doesn't need image API).
func (s *PublishingService) buildAppConfig(ch *model.Channel) (*appconfig.Config, error) {
	return agent.BuildAppConfig(ch, nil)
}

// createDraftService creates a draft.Service for the given channel.
func (s *PublishingService) createDraftService(ch *model.Channel) (*draft.Service, error) {
	appCfg, err := s.buildAppConfig(ch)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}
	zapLog, _ := zap.NewProduction()
	return draft.NewService(appCfg, zapLog), nil
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

	// Deduct credits.
	if s.creditSvc != nil {
		if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeDraftPublish, 1); creditErr != nil {
			s.logger.Error().Err(creditErr).Str("user_id", userID).Str("media_id", result.MediaID).Msg("failed to deduct credits for draft publish")
			// Log but do not fail — the draft is already published on WeChat.
		}
	}

	s.logger.Info().Str("user_id", userID).Str("channel_id", channelID).Str("media_id", result.MediaID).Msg("article draft published")

	return &PublishDraftResult{
		MediaID:  result.MediaID,
		DraftURL: result.DraftURL,
	}, nil
}

// PublishXls creates a WeChat Xiaolvshu (image post) draft for the given channel.
func (s *PublishingService) PublishXls(ctx context.Context, userID, channelID string, req XlsPublishRequest) (*XlsPublishResult, error) {
	ch, err := s.getChannel(ctx, userID, channelID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftService(ch)
	if err != nil {
		return nil, err
	}

	draftReq := &draft.ImageXlsRequest{
		Title:        req.Title,
		Content:      req.Content,
		Images:       req.Images,
		MediaIDs:     req.MediaIDs,
		OpenComment:  req.OpenComment,
		FansOnly:     req.FansOnly,
		FromMarkdown: req.FromMarkdown,
	}

	result, err := ds.CreateImageXls(draftReq)
	if err != nil {
		return nil, fmt.Errorf("create xls draft: %w", err)
	}

	// Deduct credits.
	if s.creditSvc != nil {
		if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeDraftPublish, 1); creditErr != nil {
			s.logger.Error().Err(creditErr).Str("user_id", userID).Str("media_id", result.MediaID).Msg("failed to deduct credits for xls publish")
			// Log but do not fail — the draft is already published on WeChat.
		}
	}

	s.logger.Info().Str("user_id", userID).Str("channel_id", channelID).Str("media_id", result.MediaID).Int("count", result.Count).Msg("xls draft published")

	return &XlsPublishResult{
		MediaID:     result.MediaID,
		DraftURL:    result.DraftURL,
		Count:       result.Count,
		UploadedIDs: result.UploadedIDs,
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
