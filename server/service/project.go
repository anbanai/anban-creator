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
	ErrProjectNotFound        = errors.New("project not found")
	ErrProjectOwnedByUser     = errors.New("project not owned by user")
	ErrProjectDeleteConflict  = errors.New("project delete conflict")
	ErrProjectUpdateConflict  = errors.New("project update conflict")
	ErrProjectMontageDefaults = errors.New("invalid montage project defaults")
)

type projectDeleteConflictError struct {
	msg string
}

func (e projectDeleteConflictError) Error() string {
	return e.msg
}

func (e projectDeleteConflictError) Is(target error) bool {
	return target == ErrProjectDeleteConflict
}

// validPlatforms defines the allowed platform values.
var validPlatforms = map[string]bool{
	model.PlatformArticle:   true,
	model.PlatformSeednote:  true,
	model.PlatformMoments:   true,
	model.PlatformEcommerce: true,
	model.PlatformMontage:   true,
}

func validateProjectMontageDefaults(project *model.Project) error {
	if project == nil || !project.MontageDefaultsSet {
		return nil
	}
	if !model.IsMontagePlatform(project.Platform) {
		return fmt.Errorf("%w: montage_defaults can only be set on montage projects", ErrProjectMontageDefaults)
	}
	duration := project.MontageDefaults.Data().Preferences.DurationSeconds
	if duration < 0 || duration > 600 {
		return fmt.Errorf("%w: montage duration_seconds must be between 0 and 600", ErrProjectMontageDefaults)
	}
	return nil
}

// ProjectService handles project CRUD operations with ownership verification.
type ProjectService struct {
	repo        repository.Repository
	logger      *zerolog.Logger
	templateSvc *TemplateService
	memory      ProjectMemoryLifecycle
}

type ProjectMemoryLifecycle interface {
	DeleteProjectMemory(context.Context, string) error
}

// NewProjectService creates a new ProjectService.
func NewProjectService(repo repository.Repository, logger *zerolog.Logger) *ProjectService {
	return &ProjectService{repo: repo, logger: logger}
}

// SetTemplateService injects an optional TemplateService for template recommendations.
func (s *ProjectService) SetTemplateService(svc *TemplateService) {
	s.templateSvc = svc
}

func (s *ProjectService) SetProjectMemoryLifecycle(memory ProjectMemoryLifecycle) {
	s.memory = memory
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
	if err := validateProjectMontageDefaults(ch); err != nil {
		return nil, err
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
	existing, err := s.prepareProjectUpdate(ctx, userID, projectID, ch)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Projects().Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return existing, nil
}

func (s *ProjectService) UpdateIfReferenceImageAssetID(ctx context.Context, userID, projectID string, ch *model.Project, expectedID string) (*model.Project, error) {
	existing, err := s.prepareProjectUpdate(ctx, userID, projectID, ch)
	if err != nil {
		return nil, err
	}
	if existing.ReferenceImageAssetID != expectedID {
		return nil, ErrProjectUpdateConflict
	}
	won, err := s.repo.Projects().UpdateIfReferenceImageAssetID(ctx, existing, expectedID)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	if !won {
		return nil, ErrProjectUpdateConflict
	}
	return existing, nil
}

func (s *ProjectService) prepareProjectUpdate(ctx context.Context, userID, projectID string, ch *model.Project) (*model.Project, error) {
	existing, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if existing.UserID != userID {
		return nil, ErrProjectOwnedByUser
	}
	effectivePlatform := existing.Platform
	if ch.Platform != "" {
		effectivePlatform = ch.Platform
	}
	if ch.MontageDefaultsSet {
		candidate := *ch
		candidate.Platform = effectivePlatform
		if err := validateProjectMontageDefaults(&candidate); err != nil {
			return nil, err
		}
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
	if ch.ReferenceImageSet {
		existing.ReferenceImageAssetID = ch.ReferenceImageAssetID
	}
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
	if ch.MontageDefaultsSet {
		existing.MontageDefaults = ch.MontageDefaults
	}
	// Merge Config: unconditionally update AppID to support credential clearing.
	// Only update Secret if non-empty to preserve existing secret during edits.
	existing.Config.WechatAppID = ch.Config.WechatAppID
	existing.Config.EnablePublishing = ch.Config.EnablePublishing
	existing.Config.RequirePublishApproval = ch.Config.RequirePublishApproval
	if ch.Config.WechatSecret != "" {
		existing.Config.WechatSecret = ch.Config.WechatSecret
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
		return projectDeleteConflictError{msg: fmt.Sprintf("cannot delete project with %d associated tasks; archive it instead", stats.TotalTasks)}
	}

	// Check if the project has associated plans.
	planCount, err := s.repo.Plans().CountByUserID(ctx, userID, projectID)
	if err != nil {
		s.logger.Warn().Err(err).Str("project_id", projectID).Msg("failed to count plans before delete")
	}
	if planCount > 0 {
		return projectDeleteConflictError{msg: fmt.Sprintf("cannot delete project with %d associated plans; archive it instead", planCount)}
	}

	if err := s.repo.Projects().Delete(ctx, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if s.memory != nil {
		if err := s.memory.DeleteProjectMemory(ctx, projectID); err != nil {
			return fmt.Errorf("delete project memory: %w", err)
		}
	}
	return nil
}

// SanitizeProject clears sensitive fields from a project before returning it in API responses.
func SanitizeProject(ch *model.Project) {
	ch.Config.WechatSecret = ""
}

// SanitizeProjectForResponse clears project secrets before API responses.
func (s *ProjectService) SanitizeProjectForResponse(ch *model.Project) {
	SanitizeProject(ch)
}
