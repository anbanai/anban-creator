package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/draft"
	"github.com/royalrick/anbanwriter/app/wechat"
	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// PublishingService handles WeChat draft publishing.
type PublishingService struct {
	repo                 repository.Repository
	logger               *zerolog.Logger
	createDraftServiceFn func(*model.Project) (draftClient, error)
}

type draftClient interface {
	CreateDraft([]draft.Article) (*draft.DraftResult, error)
	ListDrafts(offset, count int64) (*draft.ListDraftsResult, error)
	ListPublished(offset, count int64) (*draft.ListPublishedResult, error)
}

type appDraftClient struct {
	service *draft.Service
}

func (c *appDraftClient) CreateDraft(articles []draft.Article) (*draft.DraftResult, error) {
	return c.service.CreateDraft(articles)
}

func (c *appDraftClient) ListDrafts(offset, count int64) (*draft.ListDraftsResult, error) {
	return c.service.ListDrafts(offset, count)
}

func (c *appDraftClient) ListPublished(offset, count int64) (*draft.ListPublishedResult, error) {
	return c.service.ListPublished(offset, count)
}

var _ draftClient = (*appDraftClient)(nil)

// NewPublishingService creates a new PublishingService.
func NewPublishingService(repo repository.Repository, logger *zerolog.Logger) *PublishingService {
	s := &PublishingService{
		repo:   repo,
		logger: logger,
	}
	s.createDraftServiceFn = s.defaultCreateDraftService
	return s
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

// buildAppConfig creates an app config from a project (nil image config since publishing doesn't need image API).
// Style dimensions resolve project-only (no task context) — the byline comes from
// the project, matching the pre-refactor project-author behavior.
func (s *PublishingService) buildAppConfig(ch *model.Project) (*appconfig.Config, error) {
	return agent.BuildAppConfig(ch, ResolveStyle(ch, nil), nil, "", false, "")
}

// defaultCreateDraftService creates a draft.Service for the given project.
func (s *PublishingService) defaultCreateDraftService(ch *model.Project) (draftClient, error) {
	appCfg, err := s.buildAppConfig(ch)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}
	return &appDraftClient{service: draft.NewService(appCfg, s.logger)}, nil
}

// getProject retrieves and validates a project for the given user.
func (s *PublishingService) getProject(ctx context.Context, userID, projectID string) (*model.Project, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch == nil {
		return nil, fmt.Errorf("project not found")
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}
	if ch.GetWechatAppID() == "" || ch.GetWechatSecret() == "" {
		return nil, fmt.Errorf("project missing WeChat credentials")
	}
	return ch, nil
}

// PublishDraft creates a WeChat article draft for the given project.
func (s *PublishingService) PublishDraft(ctx context.Context, userID, projectID string, articles []DraftArticleInput) (*PublishDraftResult, error) {
	ch, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	// Pre-publish gate: reject drafts whose body images collapse to a single
	// URL — the mechanical backstop for "all content images identical" caused
	// by agents reusing one image when generation fails. This runs before the
	// draft client is built so a bad draft never reaches the WeChat API.
	for i, a := range articles {
		if err := validateContentImageDiversity(a.Content); err != nil {
			s.logger.Warn().
				Str("user_id", userID).
				Str("project_id", projectID).
				Int("article_index", i).
				Msg("publish_draft rejected: duplicate content images")
			return nil, err
		}
	}

	ds, err := s.createDraftServiceFn(ch)
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

	s.logger.Info().Str("user_id", userID).Str("project_id", projectID).Str("media_id", result.MediaID).Msg("article draft published")

	return &PublishDraftResult{
		MediaID:  result.MediaID,
		DraftURL: result.DraftURL,
	}, nil
}

// ListDrafts returns a paginated list of WeChat drafts for the given project.
func (s *PublishingService) ListDrafts(ctx context.Context, userID, projectID string, offset, count int64) (*draft.ListDraftsResult, error) {
	ch, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftServiceFn(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListDrafts(offset, count)
	if err != nil {
		// 48001: subscription/unverified accounts don't support draft API — degrade gracefully
		if code, ok := wechatErrCode(err); ok && code == 48001 {
			return &draft.ListDraftsResult{
				TotalCount: 0,
				ItemCount:  0,
				Items:      nil,
				Note:       "此公众号（订阅号/未认证服务号）不支持草稿列表查询",
			}, nil
		}
		return nil, fmt.Errorf("list drafts: %w", err)
	}

	return result, nil
}

// ListPublished returns a paginated list of published WeChat articles for the given project.
func (s *PublishingService) ListPublished(ctx context.Context, userID, projectID string, offset, count int64) (*draft.ListPublishedResult, error) {
	ch, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	ds, err := s.createDraftServiceFn(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListPublished(offset, count)
	if err != nil {
		// 48001: subscription/unverified accounts don't support freepublish API — degrade gracefully
		if code, ok := wechatErrCode(err); ok && code == 48001 {
			return &draft.ListPublishedResult{
				TotalCount: 0,
				ItemCount:  0,
				Items:      nil,
				Note:       "此公众号（订阅号/未认证服务号）不支持已发布文章列表查询",
			}, nil
		}
		return nil, fmt.Errorf("list published: %w", err)
	}

	return result, nil
}

func wechatErrCode(err error) (int, bool) {
	var wErr *wechat.WechatAPIError
	if errors.As(err, &wErr) {
		return wErr.ErrCode, true
	}
	if parsed := wechat.ParseWechatError(err); parsed != nil {
		return parsed.ErrCode, true
	}
	return 0, false
}
