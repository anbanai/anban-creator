package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrProjectNotFound       = errors.New("project not found")
	ErrProjectOwnedByUser    = errors.New("project not owned by user")
	ErrVideoModelUnavailable = errors.New("video model unavailable")
)

// validPlatforms defines the allowed platform values.
var validPlatforms = map[string]bool{
	model.PlatformArticle:   true,
	model.PlatformSeednote:  true,
	model.PlatformEcommerce: true,
	model.PlatformVideo:     true,
}

// ProjectService handles project CRUD operations with ownership verification.
type ProjectService struct {
	repo         repository.Repository
	logger       *zerolog.Logger
	templateSvc  *TemplateService
	videoCatalog VideoModelCatalog
}

// NewProjectService creates a new ProjectService.
func NewProjectService(repo repository.Repository, logger *zerolog.Logger) *ProjectService {
	return &ProjectService{repo: repo, logger: logger}
}

// SetTemplateService injects an optional TemplateService for template recommendations.
func (s *ProjectService) SetTemplateService(svc *TemplateService) {
	s.templateSvc = svc
}

func (s *ProjectService) SetVideoCatalog(catalog VideoModelCatalog) {
	if s == nil {
		return
	}
	s.videoCatalog = catalog
}

func (s *ProjectService) resolvedVideoCatalog() VideoModelCatalog {
	if s != nil && s.videoCatalog != nil {
		return s.videoCatalog
	}
	return DefaultVideoModelCatalog()
}

// Create creates a new project for the given user.
// It sets UserID and Status, validates the platform, then persists via the repository.
//
// Style dimensions are NOT defaulted here: VisualStyle (图片视觉) is free text and
// never defaults, and the article writer voice default (writer.DefaultStyleName)
// is injected at RESOLUTION time (resolver.ResolveStyle) so every consumer agrees
// on the single source of truth. The project stores only what the user set.
func (s *ProjectService) Create(ctx context.Context, userID string, ch *model.Project) (*model.Project, error) {
	if !validPlatforms[ch.Platform] {
		return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
	}
	if ch.Instructions == "" && ch.Positioning != "" {
		ch.Instructions = ch.Positioning
	}

	// Platform-specific validation.
	pc := model.GetPlatformConfig(ch.Platform)
	if pc != nil {
		if ch.ImageRatio == "" && pc.DefaultImageRatio != "" {
			ch.ImageRatio = pc.DefaultImageRatio
		}
	}
	if err := s.validateVideoProfile(ch); err != nil {
		return nil, err
	}

	ch.ID = uuid.New().String()
	ch.UserID = userID
	ch.Status = model.ProjectStatusActive

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
	if ch.Positioning != "" && !ch.InstructionsSet && ch.Instructions == "" {
		ch.Instructions = ch.Positioning
		ch.InstructionsSet = true
	}
	if ch.Keywords != "" {
		existing.Keywords = ch.Keywords
	}
	if ch.VisualStyle != "" {
		existing.VisualStyle = ch.VisualStyle
	}
	if ch.Writer != "" {
		existing.Writer = ch.Writer
	}
	if ch.Theme != "" {
		existing.Theme = ch.Theme
	}
	// 作者署名（author）：unconditional assign 以支持清空。
	// 导入模型下"导入模板→清空署名"是合法操作，guarded assign 会让清空后的保存静默回填旧署名。
	existing.Author = ch.Author
	// 建项来源模板：unconditional assign 以支持清空。
	existing.CreatedFromTemplateID = ch.CreatedFromTemplateID
	// ReferenceImageURL: unconditional assign to support clearing.
	existing.ReferenceImageURL = ch.ReferenceImageURL
	// ImageRatio: unconditional assign to support clearing.
	existing.ImageRatio = ch.ImageRatio
	if ch.InstructionsSet {
		existing.Instructions = ch.Instructions
	}
	if ch.MaxConcurrentTasks > 0 {
		existing.MaxConcurrentTasks = ch.MaxConcurrentTasks
	}
	if ch.EcommerceDefaultsSet {
		existing.EcommerceDefaults = ch.EcommerceDefaults
	}
	if ch.VideoProfileSet {
		existing.VideoDefaults = ch.VideoDefaults
		existing.VideoModelPolicy = ch.VideoModelPolicy
	}
	if err := s.validateVideoProfile(existing); err != nil {
		return nil, err
	}
	// Merge Config: unconditionally update AppID to support credential clearing.
	// Only update Secret if non-empty to preserve existing secret during edits.
	existing.Config.WechatAppID = ch.Config.WechatAppID
	existing.Config.EnablePublishing = ch.Config.EnablePublishing
	existing.Config.RequirePublishApproval = ch.Config.RequirePublishApproval
	if ch.Config.WechatSecret != "" {
		existing.Config.WechatSecret = ch.Config.WechatSecret
	}

	if err := s.repo.Projects().Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}

	return existing, nil
}

func (s *ProjectService) validateVideoProfile(ch *model.Project) error {
	if ch == nil || ch.Platform != model.PlatformVideo {
		return nil
	}
	catalog := s.resolvedVideoCatalog()
	defaults := ch.VideoDefaults.Data()
	policy := ch.VideoModelPolicy.Data()
	for _, key := range policy.AllowedModels {
		if _, ok := catalog[key]; !ok {
			return fmt.Errorf("模型未配置或不可用: %s: %w", key, ErrVideoModelUnavailable)
		}
	}
	if policy.DefaultModel != "" {
		if _, ok := catalog[policy.DefaultModel]; !ok {
			return fmt.Errorf("模型未配置或不可用: %s: %w", policy.DefaultModel, ErrVideoModelUnavailable)
		}
		if len(policy.AllowedModels) > 0 && !stringIn(policy.DefaultModel, policy.AllowedModels) {
			return fmt.Errorf("默认模型不在项目允许模型中: %s", policy.DefaultModel)
		}
	}
	if defaults.ModelKey != "" {
		if _, ok := catalog[defaults.ModelKey]; !ok {
			return fmt.Errorf("模型未配置或不可用: %s: %w", defaults.ModelKey, ErrVideoModelUnavailable)
		}
		if len(policy.AllowedModels) > 0 && !stringIn(defaults.ModelKey, policy.AllowedModels) {
			return fmt.Errorf("默认视频模型不在项目允许模型中: %s", defaults.ModelKey)
		}
	}
	return nil
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

// SanitizeProjectVideoProfile removes historical video model keys that are no
// longer present in the server-configured catalog before returning a project to
// Studio or agents. Persistence remains unchanged; create/update/estimate/task
// execution still fail closed through the service validation paths.
func SanitizeProjectVideoProfile(ch *model.Project, catalog VideoModelCatalog) {
	if ch == nil || ch.Platform != model.PlatformVideo {
		return
	}
	if catalog == nil {
		catalog = DefaultVideoModelCatalog()
	}
	defaults := ch.VideoDefaults.Data()
	policy := ch.VideoModelPolicy.Data()
	if defaults.ModelKey != "" {
		if _, ok := catalog[defaults.ModelKey]; !ok {
			defaults.ModelKey = ""
		}
	}
	allowed := make([]string, 0, len(policy.AllowedModels))
	allowedSet := map[string]bool{}
	for _, key := range policy.AllowedModels {
		if _, ok := catalog[key]; !ok {
			continue
		}
		allowed = append(allowed, key)
		allowedSet[key] = true
	}
	policy.AllowedModels = allowed
	if policy.DefaultModel != "" {
		if _, ok := catalog[policy.DefaultModel]; !ok || (len(allowedSet) > 0 && !allowedSet[policy.DefaultModel]) {
			policy.DefaultModel = ""
		}
	}
	if defaults.ModelKey != "" && len(allowedSet) > 0 && !allowedSet[defaults.ModelKey] {
		defaults.ModelKey = ""
	}
	ch.SetVideoDefaults(defaults)
	ch.SetVideoModelPolicy(policy)
}

// SanitizeProjectForResponse clears secrets and hides unavailable video models
// according to this service's configured catalog.
func (s *ProjectService) SanitizeProjectForResponse(ch *model.Project) {
	SanitizeProject(ch)
	SanitizeProjectVideoProfile(ch, s.resolvedVideoCatalog())
}
