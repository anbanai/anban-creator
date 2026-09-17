package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	appconfig "github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/app/draft"
	"github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// PublishingService handles WeChat draft publishing.
type PublishingService struct {
	repo                 repository.Repository
	logger               *zerolog.Logger
	createDraftServiceFn func(*model.Project) (draftClient, error)
}

type draftClient interface {
	ListDrafts(offset, count int64) (*draft.ListDraftsResult, error)
	ListPublished(offset, count int64) (*draft.ListPublishedResult, error)
}

type appDraftClient struct {
	service *draft.Service
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

// buildAppConfig creates an app config from a project (nil image config since
// publishing doesn't need image API). Style dimensions resolve project-only HERE:
// the per-article Author actually published to WeChat comes from draft.json
// (written by the agent using the task-aware get_project_profile, so task-level
// author overrides ARE honored on publish). The project author resolved here is
// only the fallback the draft service uses for an article with no Author (e.g.
// the HTML-file fallback path in extractArticleDraftFromWorkspace).
func (s *PublishingService) buildAppConfig(ch *model.Project) (*appconfig.Config, error) {
	return agent.BuildAppConfig(ch, ResolveStyle(ch, nil), nil, "", false)
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
