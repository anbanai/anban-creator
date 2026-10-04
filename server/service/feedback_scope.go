package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

// Feedback identity belongs to a channel, while the project remains shared.
type feedbackProjectScope struct {
	*model.Project
	Channel   string
	AccountID string
}

func feedbackChannel(identity string) string {
	switch strings.TrimSpace(identity) {
	case model.PlatformWechat, model.ChannelArticle:
		return model.ChannelArticle
	case model.ChannelWechatPicture:
		return model.ChannelWechatPicture
	case model.ChannelSeednote:
		return model.ChannelSeednote
	default:
		return ""
	}
}

// Older analytics rows may predate Channel. Their explicit content type is
// more specific than the connector platform and preserves picture identity.
func feedbackContentChannelSQL(prefix string) string {
	return "COALESCE(NULLIF(" + prefix + "channel,''), CASE WHEN " + prefix + "content_type IN ('wechat-article','wechat-picture') THEN " + prefix + "content_type WHEN " + prefix + "platform = 'wechat' THEN 'wechat-article' ELSE " + prefix + "platform END)"
}

func scopeFeedbackContents(query *gorm.DB, prefix, identity string) *gorm.DB {
	if identity == model.PlatformWechat {
		return query.Where(feedbackContentChannelSQL(prefix)+" IN ?", []string{model.ChannelArticle, model.ChannelWechatPicture})
	}
	return query.Where(feedbackContentChannelSQL(prefix)+" = ?", identity)
}

func feedbackScopeForChannel(ctx context.Context, repo repository.Repository, project *model.Project, channel string) (feedbackProjectScope, error) {
	scope := feedbackProjectScope{Project: project, Channel: channel}
	scope.AccountID = channel + ":project:" + project.ID
	if channel == model.ChannelSeednote {
		if profile := strings.TrimSpace(project.ProfileURL); profile != "" {
			scope.AccountID = channel + ":" + profile
		}
		return scope, nil
	}
	configs, err := repo.ProjectChannelConfigs().List(ctx, project.ID)
	if err != nil {
		return scope, fmt.Errorf("load feedback account config: %w", err)
	}
	for _, config := range configs {
		if config.Channel == channel {
			if appID := strings.TrimSpace(mapStringValue(config.Config.Data(), "wechat_app_id")); appID != "" {
				scope.AccountID = "wechat:" + appID
			}
			break
		}
	}
	return scope, nil
}

func feedbackProjectScopes(ctx context.Context, repo repository.Repository, projects []*model.Project) ([]feedbackProjectScope, error) {
	scopes := []feedbackProjectScope{}
	for _, project := range projects {
		if project == nil {
			continue
		}
		channels := map[string]bool{}
		var identities []string
		db := repo.Analytics().DB().WithContext(ctx)
		if err := db.Model(&model.AnalyticsContent{}).Where("project_id = ?", project.ID).Distinct().Pluck(feedbackContentChannelSQL(""), &identities).Error; err != nil {
			return nil, err
		}
		var taskIdentities []string
		if err := db.Model(&model.Task{}).Where("project_id = ?", project.ID).Distinct().Pluck("COALESCE(NULLIF(channel,''), type)", &taskIdentities).Error; err != nil {
			return nil, err
		}
		for _, identity := range append(identities, taskIdentities...) {
			if channel := feedbackChannel(identity); channel != "" {
				channels[channel] = true
			}
		}
		ordered := make([]string, 0, len(channels))
		for channel := range channels {
			ordered = append(ordered, channel)
		}
		sort.Strings(ordered)
		for _, channel := range ordered {
			scope, err := feedbackScopeForChannel(ctx, repo, project, channel)
			if err != nil {
				return nil, err
			}
			scopes = append(scopes, scope)
		}
	}
	return scopes, nil
}
