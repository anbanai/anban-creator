package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/app/converter"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
)

type ContentRenderService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

func NewContentRenderService(repo repository.Repository, logger *zerolog.Logger) *ContentRenderService {
	return &ContentRenderService{repo: repo, logger: logger}
}

func (s *ContentRenderService) resolveEffectiveTheme(ctx context.Context, taskID string, project *model.Project) string {
	var task *model.Task
	if taskID != "" {
		if found, err := s.repo.Tasks().FindByID(ctx, taskID); err == nil {
			task = found
		}
	}
	theme := ResolveStyle(project, task).Theme
	if theme == "" {
		theme = "autumn-warm"
	}
	return theme
}

type ConvertMarkdownResult struct {
	HTML   string        `json:"html"`
	Images []ImageRefDTO `json:"images,omitempty"`
}

type ImageRefDTO struct {
	Index       int    `json:"index"`
	Original    string `json:"original"`
	Placeholder string `json:"placeholder,omitempty"`
}

func (s *ContentRenderService) ConvertMarkdown(ctx context.Context, userID, projectID, markdown, theme, taskID string) (*ConvertMarkdownResult, error) {
	if markdown == "" {
		return nil, fmt.Errorf("markdown content is required")
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}
	if theme == "" {
		theme = s.resolveEffectiveTheme(ctx, taskID, project)
	}

	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))
	result := cvt.Convert(&converter.ConvertRequest{Markdown: markdown, Theme: theme})
	if !result.Success {
		return nil, fmt.Errorf("convert error: %s", result.Error)
	}
	images := make([]ImageRefDTO, 0, len(result.Images))
	for _, image := range result.Images {
		images = append(images, ImageRefDTO{Index: image.Index, Original: image.Original, Placeholder: image.Placeholder})
	}
	if s.logger != nil {
		s.logger.Info().Str("user_id", userID).Str("project_id", projectID).Str("theme", theme).Int("image_count", len(images)).Msg("markdown converted")
	}
	return &ConvertMarkdownResult{HTML: result.HTML, Images: images}, nil
}
