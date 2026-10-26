package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	appconfig "github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/app/draft"
	"github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
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

func (s *PublishingService) BuildAppConfigForProject(ch *model.Project) (*appconfig.Config, error) {
	return s.buildAppConfig(ch)
}

// defaultCreateDraftService creates a draft.Service for the given project.
func (s *PublishingService) defaultCreateDraftService(ch *model.Project) (draftClient, error) {
	appCfg, err := s.buildAppConfig(ch)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}
	return &appDraftClient{service: draft.NewService(appCfg, s.logger)}, nil
}

// getProject retrieves a project and validates ownership without requiring a
// connected WeChat account. History can still be reported as unavailable.
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
	return ch, nil
}

func (s *PublishingService) loadWeChatCredentials(ctx context.Context, ch *model.Project) (bool, error) {
	config, err := s.repo.ProjectChannelConfigs().Get(ctx, ch.ID, model.ChannelArticle)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("load project WeChat config: %w", err)
	}
	if config == nil {
		return false, nil
	}
	ch.RuntimeChannelConfig = config.Config.Data()
	return strings.TrimSpace(mapStringValue(ch.RuntimeChannelConfig, "wechat_app_id")) != "" &&
		strings.TrimSpace(mapStringValue(ch.RuntimeChannelConfig, "wechat_secret")) != "", nil
}

func unavailableDraftHistory(note string) *draft.ListDraftsResult {
	return &draft.ListDraftsResult{HistoryAvailable: false, Note: note}
}

func unavailablePublishedHistory(note string) *draft.ListPublishedResult {
	return &draft.ListPublishedResult{HistoryAvailable: false, Note: note}
}

func mapStringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

// ListDrafts returns a paginated list of WeChat drafts for the given project.
func (s *PublishingService) ListDrafts(ctx context.Context, userID, projectID string, offset, count int64) (*draft.ListDraftsResult, error) {
	ch, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	hasCredentials, err := s.loadWeChatCredentials(ctx, ch)
	if err != nil {
		return nil, err
	}
	if !hasCredentials {
		return unavailableDraftHistory("未配置公众号 AppID/Secret，无法查询草稿历史；这不代表草稿箱为空"), nil
	}

	ds, err := s.createDraftServiceFn(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListDrafts(offset, count)
	if err != nil {
		// 48001: subscription/unverified accounts don't support draft API — degrade gracefully
		if code, ok := wechatErrCode(err); ok && code == 48001 {
			return unavailableDraftHistory("此公众号不支持草稿列表查询；返回内容不是空草稿箱的证明"), nil
		}
		return nil, fmt.Errorf("list drafts: %w", err)
	}

	result.HistoryAvailable = true
	return result, nil
}

// ListPublished returns a paginated list of published WeChat articles for the given project.
func (s *PublishingService) ListPublished(ctx context.Context, userID, projectID string, offset, count int64) (*draft.ListPublishedResult, error) {
	ch, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	hasCredentials, err := s.loadWeChatCredentials(ctx, ch)
	if err != nil {
		return nil, err
	}
	if !hasCredentials {
		return unavailablePublishedHistory("未配置公众号 AppID/Secret，无法查询已发布历史；这不代表发布历史为空"), nil
	}

	ds, err := s.createDraftServiceFn(ch)
	if err != nil {
		return nil, err
	}

	result, err := ds.ListPublished(offset, count)
	if err != nil {
		// 48001: subscription/unverified accounts don't support freepublish API — degrade gracefully
		if code, ok := wechatErrCode(err); ok && code == 48001 {
			return unavailablePublishedHistory("此公众号不支持已发布文章列表查询；返回内容不是空发布历史的证明"), nil
		}
		return nil, fmt.Errorf("list published: %w", err)
	}

	result.HistoryAvailable = true
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
