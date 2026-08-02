package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrProjectNotFound        = errors.New("project not found")
	ErrProjectOwnedByUser     = errors.New("project not owned by user")
	ErrProjectDeleteConflict  = errors.New("project delete conflict")
	ErrProjectUpdateConflict  = errors.New("project update conflict")
	ErrProjectMontageDefaults = errors.New("invalid montage project defaults")
	ErrInvalidAgentConfig     = errors.New("invalid agent config")
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

func validProjectPlatform(platform string) bool {
	return model.IsProjectPlatform(platform)
}

func validateProjectAgentConfig(project *model.Project) error {
	if project == nil || !project.AgentConfigSet {
		return nil
	}
	pack, ok := agentpack.Default().ForProjectPlatform(project.Platform)
	if !ok {
		return fmt.Errorf("%w: no Agent Pack is bound to project platform %q", ErrInvalidAgentConfig, project.Platform)
	}
	if err := agentpack.ValidateProjectConfig(pack, project.AgentConfig.Data()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAgentConfig, err)
	}
	return nil
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
	if !validProjectPlatform(ch.Platform) {
		return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
	}
	if err := validateProjectMontageDefaults(ch); err != nil {
		return nil, err
	}
	if err := validateProjectAgentConfig(ch); err != nil {
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
		if !model.IsBusinessImageRatioAllowed(ch.Platform, ch.ImageRatio) {
			return nil, fmt.Errorf("%s: %s", model.ValidImageRatioHint, ch.ImageRatio)
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
	if ch.AgentConfigSet || (ch.Platform != "" && ch.Platform != existing.Platform) {
		candidate := *existing
		candidate.Platform = effectivePlatform
		candidate.AgentConfigSet = true
		if ch.AgentConfigSet {
			candidate.AgentConfig = ch.AgentConfig
		}
		if err := validateProjectAgentConfig(&candidate); err != nil {
			return nil, err
		}
	}

	// Apply updatable fields from ch to existing (only non-empty values).
	platformChanged := ch.Platform != "" && ch.Platform != existing.Platform
	if ch.Name != "" {
		existing.Name = ch.Name
	}
	if ch.Platform != "" {
		if !validProjectPlatform(ch.Platform) {
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
	if ch.ReferenceImageSet {
		existing.ReferenceImageAssetID = ch.ReferenceImageAssetID
	}
	// Empty update input preserves the persisted value; "auto" is the explicit
	// user choice for semantic adaptation and is stored as-is.
	if strings.TrimSpace(ch.ImageRatio) != "" {
		if !model.IsBusinessImageRatioAllowed(existing.Platform, ch.ImageRatio) {
			return nil, fmt.Errorf("%s: %s", model.ValidImageRatioHint, ch.ImageRatio)
		}
		existing.ImageRatio = ch.ImageRatio
	} else if platformChanged || strings.TrimSpace(existing.ImageRatio) == "" {
		existing.ImageRatio = model.DefaultImageRatio(existing.Platform)
	}
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
	if ch.AgentConfigSet {
		existing.AgentConfig = ch.AgentConfig
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
	acquired, err := s.repo.Projects().BeginDelete(ctx, projectID)
	if err != nil {
		return fmt.Errorf("begin project delete: %w", err)
	}
	ch, err = s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("reload project delete authority: %w", err)
	}
	if !acquired && ch.DeletingAt == nil {
		return fmt.Errorf("begin project delete: deletion authority was not acquired")
	}
	releaseBarrier := func(original error) error {
		if !acquired {
			return original
		}
		released, releaseErr := s.repo.Projects().CancelDelete(context.WithoutCancel(ctx), projectID)
		if releaseErr != nil {
			return errors.Join(original, fmt.Errorf("release project delete barrier: %w", releaseErr))
		}
		if !released {
			return errors.Join(original, errors.New("release project delete barrier: authority was lost"))
		}
		return original
	}

	// Check if the project has associated tasks.
	stats, err := s.repo.Projects().GetStats(ctx, projectID)
	if err != nil {
		return releaseBarrier(fmt.Errorf("get project stats before delete: %w", err))
	}
	if stats != nil && stats.TotalTasks > 0 {
		return releaseBarrier(projectDeleteConflictError{msg: fmt.Sprintf("cannot delete project with %d associated tasks; archive it instead", stats.TotalTasks)})
	}

	// Check if the project has associated plans.
	planCount, err := s.repo.Plans().CountByUserID(ctx, userID, projectID)
	if err != nil {
		return releaseBarrier(fmt.Errorf("count project plans before delete: %w", err))
	}
	if planCount > 0 {
		return releaseBarrier(projectDeleteConflictError{msg: fmt.Sprintf("cannot delete project with %d associated plans; archive it instead", planCount)})
	}

	if s.memory != nil {
		if err := s.memory.DeleteProjectMemory(ctx, projectID); err != nil {
			return fmt.Errorf("delete project memory: %w", err)
		}
	}
	deleted, err := s.repo.Projects().DeleteIfDeletingAndEmpty(ctx, projectID)
	if errors.Is(err, repository.ErrProjectDeleteDependencies) {
		return projectDeleteConflictError{msg: "cannot delete project with associated tasks or plans; archive it instead"}
	}
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if !deleted {
		return fmt.Errorf("delete project: deletion authority was lost")
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
