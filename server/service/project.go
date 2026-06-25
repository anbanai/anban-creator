package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

var (
	ErrProjectNotFound    = errors.New("project not found")
	ErrProjectOwnedByUser = errors.New("project not owned by user")
)

// validPlatforms defines the allowed platform values.
var validPlatforms = map[string]bool{
	model.PlatformArticle:   true,
	model.PlatformSeednote:  true,
	model.PlatformEcommerce: true,
}

// ProjectService handles project CRUD operations with ownership verification.
type ProjectService struct {
	repo        repository.Repository
	logger      *zerolog.Logger
	templateSvc *TemplateService
}

// NewProjectService creates a new ProjectService.
func NewProjectService(repo repository.Repository, logger *zerolog.Logger) *ProjectService {
	return &ProjectService{repo: repo, logger: logger}
}

// SetTemplateService injects an optional TemplateService for template recommendations.
func (s *ProjectService) SetTemplateService(svc *TemplateService) {
	s.templateSvc = svc
}

// applyStyleDefaults normalizes the two text/style dimensions on a project:
//   - style (图片视觉) is free text and NEVER defaults to a writer resource key.
//     Previously the article platform defaulted ch.Style to writer.DefaultStyleName,
//     which seeded the bug where a writer key ("dan-koe") leaked into the visual
//     style field and was injected as an image-gen style. Visual style stays empty
//     unless the user sets one.
//   - writingStyle (写作风格) defaults to the platform's default writer
//     (article → writer.DefaultStyleName) when empty, so article projects always
//     carry a writing voice. Seednote has no writer dimension.
func applyStyleDefaults(platform, style, writingStyle string) (string, string) {
	if platform == model.PlatformArticle && writingStyle == "" {
		writingStyle = writer.DefaultStyleName
	}
	return style, writingStyle
}

// Create creates a new project for the given user.
// It sets UserID and Status, validates the platform, then persists via the repository.
func (s *ProjectService) Create(ctx context.Context, userID string, ch *model.Project) (*model.Project, error) {
	if !validPlatforms[ch.Platform] {
		return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
	}

	// Platform-specific validation.
	pc := model.GetPlatformConfig(ch.Platform)
	if pc != nil {
		if ch.ImageRatio == "" && pc.DefaultImageRatio != "" {
			ch.ImageRatio = pc.DefaultImageRatio
		}
	}

	ch.ID = uuid.New().String()
	ch.UserID = userID
	ch.Status = model.ProjectStatusActive
	ch.Style, ch.WritingStyle = applyStyleDefaults(ch.Platform, ch.Style, ch.WritingStyle)

	if err := s.repo.Projects().Create(ctx, ch); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	return ch, nil
}

// Get returns a project and its stats, verifying that the project belongs to the user.
func (s *ProjectService) Get(ctx context.Context, userID, projectID string) (*model.Project, *repository.ProjectStats, error) {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if ch.UserID != userID {
		return nil, nil, ErrProjectOwnedByUser
	}

	stats, err := s.repo.Projects().GetStats(ctx, projectID)
	if err != nil {
		s.logger.Error().Err(err).Str("project_id", projectID).Msg("failed to get project stats")
		// Return nil stats rather than failing the whole request.
		return ch, nil, nil
	}

	return ch, stats, nil
}

// List returns projects for the given user, filtered by the provided options.
func (s *ProjectService) List(ctx context.Context, userID string, opts repository.ProjectListOptions) ([]*model.Project, error) {
	projects, err := s.repo.Projects().ListByUserID(ctx, userID, opts)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

func (s *ProjectService) BatchStats(ctx context.Context, projectIDs []string) (map[string]*repository.ProjectStats, error) {
	stats, err := s.repo.Projects().GetStatsByProjectIDs(ctx, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("batch project stats: %w", err)
	}
	return stats, nil
}

// Update updates mutable fields on a project owned by the user.
func (s *ProjectService) Update(ctx context.Context, userID, projectID string, ch *model.Project) (*model.Project, error) {
	existing, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if existing.UserID != userID {
		return nil, ErrProjectOwnedByUser
	}

	// Apply updatable fields from ch to existing (only non-empty values).
	if ch.Name != "" {
		existing.Name = ch.Name
	}
	if ch.Platform != "" {
		if !validPlatforms[ch.Platform] {
			return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
		}
		existing.Platform = ch.Platform
	}
	if ch.AvatarURL != "" {
		existing.AvatarURL = ch.AvatarURL
	}
	if ch.ProfileURL != "" {
		existing.ProfileURL = ch.ProfileURL
	}
	if ch.Positioning != "" {
		existing.Positioning = ch.Positioning
	}
	if ch.Keywords != "" {
		existing.Keywords = ch.Keywords
	}
	if ch.Style != "" {
		existing.Style = ch.Style
	}
	if ch.WritingStyle != "" {
		existing.WritingStyle = ch.WritingStyle
	}
	if ch.Theme != "" {
		existing.Theme = ch.Theme
	}
	// 作者署名（byline）：unconditional assign 以支持清空，与 AuthorStyleIntro/
	// AuthorAvatarURL 一致。导入模型下"导入模板→清空署名"是合法操作，guarded assign
	// 会让清空后的保存静默回填旧署名。
	existing.Author = ch.Author
	// 公众号人设字段 + 绑定模板：unconditional assign 以支持清空（解除绑定、清头像/写作风格）。
	existing.AuthorStyleIntro = ch.AuthorStyleIntro
	existing.AuthorAvatarURL = ch.AuthorAvatarURL
	existing.TemplateID = ch.TemplateID
	// ReferenceImageURL: unconditional assign to support clearing.
	existing.ReferenceImageURL = ch.ReferenceImageURL
	// ImageRatio: unconditional assign to support clearing.
	existing.ImageRatio = ch.ImageRatio
	if ch.MaxConcurrentTasks > 0 {
		existing.MaxConcurrentTasks = ch.MaxConcurrentTasks
	}
	// Merge Config: unconditionally update AppID to support credential clearing.
	// Only update Secret if non-empty to preserve existing secret during edits.
	existing.Config.WechatAppID = ch.Config.WechatAppID
	existing.Config.EnablePublishing = ch.Config.EnablePublishing
	if ch.Config.WechatSecret != "" {
		existing.Config.WechatSecret = ch.Config.WechatSecret
	}
	existing.Style, existing.WritingStyle = applyStyleDefaults(existing.Platform, existing.Style, existing.WritingStyle)

	if err := s.repo.Projects().Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}

	return existing, nil
}

// Archive sets a project's status to "archived" after verifying ownership.
func (s *ProjectService) Archive(ctx context.Context, userID, projectID string) error {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if ch.UserID != userID {
		return ErrProjectOwnedByUser
	}

	if err := s.repo.Projects().UpdateStatus(ctx, projectID, model.ProjectStatusArchived); err != nil {
		return fmt.Errorf("archive project: %w", err)
	}
	return nil
}

// Restore sets a project's status back to "active" after verifying ownership.
func (s *ProjectService) Restore(ctx context.Context, userID, projectID string) error {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if ch.UserID != userID {
		return ErrProjectOwnedByUser
	}

	if err := s.repo.Projects().UpdateStatus(ctx, projectID, model.ProjectStatusActive); err != nil {
		return fmt.Errorf("restore project: %w", err)
	}
	return nil
}

// Delete permanently removes a project after verifying ownership.
func (s *ProjectService) Delete(ctx context.Context, userID, projectID string) error {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if ch.UserID != userID {
		return ErrProjectOwnedByUser
	}

	// Check if the project has associated tasks.
	stats, err := s.repo.Projects().GetStats(ctx, projectID)
	if err != nil {
		s.logger.Warn().Err(err).Str("project_id", projectID).Msg("failed to get project stats before delete")
	}
	if stats != nil && stats.TotalTasks > 0 {
		return fmt.Errorf("cannot delete project with %d associated tasks; archive it instead", stats.TotalTasks)
	}

	// Check if the project has associated plans.
	planCount, err := s.repo.Plans().CountByUserID(ctx, userID, projectID)
	if err != nil {
		s.logger.Warn().Err(err).Str("project_id", projectID).Msg("failed to count plans before delete")
	}
	if planCount > 0 {
		return fmt.Errorf("cannot delete project with %d associated plans; archive it instead", planCount)
	}

	if err := s.repo.Projects().Delete(ctx, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// SanitizeProject clears sensitive fields from a project before returning it in API responses.
func SanitizeProject(ch *model.Project) {
	ch.Config.WechatSecret = ""
}
