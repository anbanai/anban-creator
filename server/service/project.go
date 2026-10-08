package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrProjectNotFound                   = errors.New("project not found")
	ErrProjectOwnedByUser                = errors.New("project not owned by user")
	ErrProjectDeleteConflict             = errors.New("project delete conflict")
	ErrProjectUpdateConflict             = errors.New("project update conflict")
	ErrProjectMontageDefaults            = errors.New("视频生成项目默认设置无效")
	ErrProjectProfileVersionConflict     = errors.New("project profile version conflict")
	ErrProjectProfileAnalysisInProgress  = errors.New("project profile analysis is already in progress")
	ErrProjectProfileResultAlreadyStored = errors.New("project profile result already submitted")
	ErrProjectProfileUnsupportedPlatform = errors.New("project profile is only supported for WeChat and Seednote projects")
	ErrInvalidProjectChannel             = errors.New("invalid project channel")
	ErrProjectNameRequired               = errors.New("project name is required")
)

var supportedProjectChannels = map[string]struct{}{
	model.ChannelArticle:       {},
	model.ChannelSeednote:      {},
	model.ChannelWechatPicture: {},
}

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

func validateProjectMontageDefaults(project *model.Project, capabilities *MontageCapabilityService) error {
	if project == nil || !project.MontageDefaultsSet {
		return nil
	}
	if !model.IsMontagePlatform(project.Platform) {
		return fmt.Errorf("%w: montage_defaults 只能用于视频生成项目", ErrProjectMontageDefaults)
	}
	if capabilities != nil {
		return capabilities.ValidateProjectDefaults(project.MontageDefaults.Data())
	}
	duration := project.MontageDefaults.Data().Preferences.DurationSeconds
	if duration < 0 || duration > 600 {
		return fmt.Errorf("%w: 视频生成 duration_seconds 必须在 0 到 600 之间", ErrProjectMontageDefaults)
	}
	return nil
}

// ProjectService handles project CRUD operations with ownership verification.
type ProjectService struct {
	repo                repository.Repository
	logger              *zerolog.Logger
	templateSvc         *TemplateService
	memory              ProjectMemoryLifecycle
	profileMemory       ProjectProfileMemory
	hypitCapabilities   *HypitCapabilityService
	montageCapabilities *MontageCapabilityService
	imageAnalyses       *ImageAnalysisService
}

type ProjectMemoryLifecycle interface {
	DeleteProject(context.Context, string) error
}

// ProjectProfileMemory is the narrow Server-owned writer used for the
// database-to-memory profile projection.
type ProjectProfileMemory interface {
	EnsureProject(context.Context, string) error
	WriteMarkdownFile(context.Context, string, string, string) error
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

func (s *ProjectService) SetProjectProfileMemory(memory ProjectProfileMemory) {
	if s != nil {
		s.profileMemory = memory
	}
}

func (s *ProjectService) SetMontageCapabilityService(capabilities *MontageCapabilityService) {
	if s != nil {
		s.montageCapabilities = capabilities
	}
}

func (s *ProjectService) SetImageAnalysisService(analyses *ImageAnalysisService) {
	s.imageAnalyses = analyses
}

// Create creates a new project for the given user.
// It sets UserID and Status, validates the platform, then persists via the repository.
//
// Style dimensions are NOT defaulted here: VisualStyle (图片视觉) is free text and
// never defaults, and the article writer voice default (writer.DefaultStyleName)
// is injected at RESOLUTION time (resolver.ResolveStyle) so every consumer agrees
// on the single source of truth. The project stores only what the user set.
func (s *ProjectService) Create(ctx context.Context, userID string, ch *model.Project) (*model.Project, error) {
	if strings.TrimSpace(ch.Name) == "" {
		return nil, ErrProjectNameRequired
	}
	ch.Name = strings.TrimSpace(ch.Name)
	if strings.TrimSpace(ch.Platform) != "" && !validProjectPlatform(ch.Platform) {
		return nil, fmt.Errorf("invalid platform: %s", ch.Platform)
	}
	if err := validateHypitProject(ch, s.hypitCapabilities); err != nil {
		return nil, err
	}
	if err := validateProjectMontageDefaults(ch, s.montageCapabilities); err != nil {
		return nil, err
	}
	if ch.Instructions == "" && ch.Positioning != "" {
		ch.Instructions = ch.Positioning
	}
	// Platform-specific validation.
	pc := model.GetPlatformConfig(ch.Platform)
	if pc != nil {
		if len(pc.SupportedImageRatios) == 0 {
			if strings.TrimSpace(ch.ImageRatio) != "" {
				return nil, fmt.Errorf("image_ratio is not supported for platform %s", ch.Platform)
			}
		} else {
			if ch.ImageRatio == "" && pc.DefaultImageRatio != "" {
				ch.ImageRatio = pc.DefaultImageRatio
			}
			if !model.IsBusinessImageRatioAllowed(ch.Platform, ch.ImageRatio) {
				return nil, fmt.Errorf("%s: %s", model.ValidImageRatioHint, ch.ImageRatio)
			}
		}
	}
	ch.ID = uuid.New().String()
	ch.UserID = userID
	ch.Status = model.ProjectStatusActive
	if ch.Profile.Data().SchemaVersion == 0 {
		ch.Profile = datatypes.NewJSONType(model.NewProjectProfile())
	}
	if strings.TrimSpace(ch.VisualStyle) != "" {
		ch.VisualStyleSource = model.ImageAnalysisSourceManual
	}

	if s.imageAnalyses != nil && ch.Platform != model.PlatformMontage && ch.Platform != model.PlatformHypit && ch.ReferenceImageAssetID != "" && strings.TrimSpace(ch.VisualStyle) == "" {
		job, err := s.imageAnalyses.CreateProjectWithJob(ctx, ch)
		if err != nil {
			return nil, fmt.Errorf("create project: %w", err)
		}
		ch.ImageAnalysis = job.View()
		if err := s.repo.ProjectProfileStates().Create(ctx, &model.ProjectProfileState{ProjectID: ch.ID, Status: model.ProfileInitializationNotStarted}); err != nil && !missingProfileStateTable(err) {
			return nil, fmt.Errorf("create project profile state: %w", err)
		}
		s.reconcileProfileProjection(ctx, ch.ID, projectProfileFor(ch))
		return ch, nil
	}
	if err := s.repo.Projects().Create(ctx, ch); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	if err := s.repo.ProjectProfileStates().Create(ctx, &model.ProjectProfileState{ProjectID: ch.ID, Status: model.ProfileInitializationNotStarted}); err != nil && !missingProfileStateTable(err) {
		return nil, fmt.Errorf("create project profile state: %w", err)
	}
	s.reconcileProfileProjection(ctx, ch.ID, projectProfileFor(ch))

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
	if s.imageAnalyses != nil {
		if err := s.imageAnalyses.PresentProject(ctx, ch); err != nil {
			return nil, nil, fmt.Errorf("present project image analysis: %w", err)
		}
	}

	stats, err := s.repo.Projects().GetStats(ctx, projectID)
	if err != nil {
		s.logger.Error().Err(err).Str("project_id", projectID).Msg("failed to get project stats")
		// Return nil stats rather than failing the whole request.
		return ch, nil, nil
	}

	return ch, stats, nil
}

func projectProfileFor(project *model.Project) model.ProjectProfile {
	profile := project.Profile.Data()
	if profile.SchemaVersion == 0 {
		profile = model.NewProjectProfile()
	}
	if profile.InitializationStatus == "" {
		profile.InitializationStatus = model.ProfileInitializationNotStarted
	}
	return profile
}

func projectProfileForAgentContext(project *model.Project) model.ProjectProfile {
	profile := projectProfileFor(project)
	if !profile.IsConfirmed() {
		return model.NewProjectProfile()
	}
	return profile
}

func profileDimension(profile model.ProjectProfile, name string) model.ProfileDimension {
	switch name {
	case "identity":
		return profile.Dimensions.Identity
	case "style":
		return profile.Dimensions.Style
	case "audience":
		return profile.Dimensions.Audience
	case "platforms":
		return profile.Dimensions.Platforms
	case "preferences":
		return profile.Dimensions.Preferences
	case "memory":
		return profile.Dimensions.Memory
	default:
		return model.ProfileDimension{}
	}
}

func (s *ProjectService) writeProfileProjection(ctx context.Context, projectID string, profile model.ProjectProfile) error {
	if s.profileMemory == nil || !profile.IsConfirmed() {
		return nil
	}
	if err := s.profileMemory.EnsureProject(ctx, projectID); err != nil {
		return fmt.Errorf("ensure project profile memory: %w", err)
	}
	for _, name := range model.ProfileDimensions() {
		content, err := ProfileDimensionMarkdown(name, profileDimension(profile, name))
		if err != nil {
			return err
		}
		if err := s.profileMemory.WriteMarkdownFile(ctx, projectID, "profile/"+name+".md", content); err != nil {
			return fmt.Errorf("write profile/%s.md: %w", name, err)
		}
	}
	if err := s.profileMemory.WriteMarkdownFile(ctx, projectID, "AGENTS.md", ProfileAgentsMarkdown()); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}
	return nil
}

// reconcileProfileProjection is deliberately best-effort. The database is the
// profile source of truth; filesystem projection is recoverable and may be
// retried by a later profile read or task bootstrap.
func (s *ProjectService) reconcileProfileProjection(ctx context.Context, projectID string, profile model.ProjectProfile) {
	if err := s.writeProfileProjection(ctx, projectID, profile); err != nil && s.logger != nil {
		s.logger.Warn().Err(err).Str("project_id", projectID).Int64("profile_revision", profile.Version).Msg("profile memory projection is stale; will retry")
	}
}

func persistProfileState(ctx context.Context, repo repository.Repository, projectID string, profile model.ProjectProfile) error {
	state, err := repo.ProjectProfileStates().FindByProjectIDForUpdate(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = repo.ProjectProfileStates().Create(ctx, &model.ProjectProfileState{ProjectID: projectID, Status: profile.InitializationStatus, Revision: profile.Version, ActiveTaskID: profile.AnalysisTaskID, LastError: profile.LastError})
		if missingProfileStateTable(err) {
			return nil
		}
		return err
	}
	if err != nil {
		if missingProfileStateTable(err) {
			return nil
		}
		return err
	}
	state.Status, state.Revision, state.ActiveTaskID, state.LastError = profile.InitializationStatus, profile.Version, profile.AnalysisTaskID, profile.LastError
	return repo.ProjectProfileStates().Save(ctx, state)
}

func missingProfileStateTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") && strings.Contains(strings.ToLower(err.Error()), "project_profile_states")
}

func validateProjectProfilePlatform(project *model.Project) error {
	if project == nil {
		return ErrProjectProfileUnsupportedPlatform
	}
	// Projects are channel-neutral; the profile belongs to shared project
	// context and is therefore valid for migrated and newly-created projects.
	return nil
}

// GetProfile returns the project-scoped profile. A new project receives an
// empty draft shape so Studio can render the same six-dimensional editor.
func (s *ProjectService) GetProfile(ctx context.Context, userID, projectID string) (model.ProjectProfile, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return model.ProjectProfile{}, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return model.ProjectProfile{}, ErrProjectOwnedByUser
	}
	if err := validateProjectProfilePlatform(project); err != nil {
		return model.ProjectProfile{}, err
	}
	profile := projectProfileFor(project)
	s.reconcileProfileProjection(ctx, projectID, profile)
	return profile, nil
}

func (s *ProjectService) updateProfile(ctx context.Context, userID, projectID string, expectedVersion int64, next model.ProjectProfile) (model.ProjectProfile, error) {
	return s.updateProfileWithSource(ctx, userID, projectID, expectedVersion, next, "", "")
}

func (s *ProjectService) updateProfileWithSource(ctx context.Context, userID, projectID string, expectedVersion int64, next model.ProjectProfile, sourceTaskID, source string) (model.ProjectProfile, error) {
	if source != "agent" {
		for _, dimension := range []*model.ProfileDimension{&next.Dimensions.Identity, &next.Dimensions.Style, &next.Dimensions.Audience, &next.Dimensions.Platforms, &next.Dimensions.Preferences, &next.Dimensions.Memory} {
			if !containsProfileSource(dimension.Sources, "[用户编辑]") {
				dimension.Sources = append(dimension.Sources, "[用户编辑]")
			}
		}
	}
	if err := next.Validate(); err != nil {
		return model.ProjectProfile{}, fmt.Errorf("invalid project profile: %w", err)
	}
	var result model.ProjectProfile
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
		}
		if project.UserID != userID {
			return ErrProjectOwnedByUser
		}
		if err := validateProjectProfilePlatform(project); err != nil {
			return err
		}
		current := projectProfileFor(project)
		if expectedVersion != current.Version {
			return ErrProjectProfileVersionConflict
		}
		if next.SchemaVersion == 0 {
			next.SchemaVersion = 1
		}
		next.Version = current.Version + 1
		if next.Status == "" {
			next.Status = current.Status
		}
		project.Profile = datatypes.NewJSONType(next)
		if err := tx.Projects().Update(ctx, project); err != nil {
			return err
		}
		dimensions, err := json.Marshal(next.Dimensions)
		if err != nil {
			return fmt.Errorf("marshal profile dimensions: %w", err)
		}
		if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: projectID, Revision: next.Version, SixDimensions: dimensions, SourceTaskID: strings.TrimSpace(sourceTaskID)}); err != nil {
			return fmt.Errorf("record profile revision: %w", err)
		}
		if err := persistProfileState(ctx, tx, projectID, next); err != nil {
			return fmt.Errorf("persist profile state: %w", err)
		}
		result = next
		return nil
	})
	if err == nil {
		s.reconcileProfileProjection(ctx, projectID, result)
	}
	return result, err
}

// ApplyAgentProfileResult validates and atomically accepts one structured
// profile result from the profile-analysis execution. The task and project
// ownership checks happen server-side; callers cannot submit for another
// execution or overwrite a newer revision.
func (s *ProjectService) ApplyAgentProfileResult(ctx context.Context, userID, projectID, taskID string, expectedVersion int64, dimensions model.ProjectProfileDimensions, limits, missing []string) (model.ProjectProfile, error) {
	if taskID == "" {
		return model.ProjectProfile{}, fmt.Errorf("profile task id is required")
	}
	if expectedVersion < 0 {
		return model.ProjectProfile{}, fmt.Errorf("profile revision must be non-negative")
	}
	var result model.ProjectProfile
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
		}
		if project.UserID != userID {
			return ErrProjectOwnedByUser
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil || task.ProjectID != projectID || task.UserID != userID || task.Type != model.TaskTypeProfileAnalysis {
			return fmt.Errorf("profile analysis task is not authorized")
		}
		if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
			return fmt.Errorf("profile analysis task is not active")
		}
		if task.Result != nil && strings.TrimSpace(*task.Result) != "" {
			return ErrProjectProfileResultAlreadyStored
		}
		current := projectProfileFor(project)
		if expectedVersion != current.Version {
			return ErrProjectProfileVersionConflict
		}
		next := current
		next.Dimensions = dimensions
		next.AnalysisLimits = append([]string(nil), limits...)
		next.FollowUpQuestions = append([]string(nil), missing...)
		if current.IsConfirmed() {
			next.Status = model.ProfileStatusConfirmed
		} else {
			next.Status = model.ProfileStatusDraft
		}
		next.InitializationStatus = model.ProfileInitializationReady
		next.LastError = ""
		next.AnalysisTaskID = taskID
		next.Version = current.Version + 1
		if err := next.Validate(); err != nil {
			return fmt.Errorf("invalid agent profile result: %w", err)
		}
		project.Profile = datatypes.NewJSONType(next)
		if err := tx.Projects().Update(ctx, project); err != nil {
			return err
		}
		dimensionsJSON, err := json.Marshal(next.Dimensions)
		if err != nil {
			return fmt.Errorf("marshal profile dimensions: %w", err)
		}
		if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{
			ID: uuid.NewString(), ProjectID: projectID, Revision: next.Version,
			SixDimensions: dimensionsJSON, SourceTaskID: taskID,
		}); err != nil {
			return fmt.Errorf("record profile revision: %w", err)
		}
		if err := persistProfileState(ctx, tx, projectID, next); err != nil {
			return fmt.Errorf("persist profile state: %w", err)
		}
		summary, _ := json.Marshal(map[string]any{"profile_revision": next.Version, "status": next.InitializationStatus})
		summaryString := string(summary)
		task.Result = &summaryString
		if err := tx.Tasks().Update(ctx, task); err != nil {
			return fmt.Errorf("persist profile task result: %w", err)
		}
		result = next
		return nil
	})
	if err != nil {
		return model.ProjectProfile{}, err
	}
	s.reconcileProfileProjection(ctx, projectID, result)
	return result, nil
}

// MarkProfileTaskState updates only the lifecycle marker and error. It never
// changes dimensions, so failed executions preserve the last ready profile.
func (s *ProjectService) MarkProfileTaskState(ctx context.Context, projectID, taskID, state, message string) error {
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return err
		}
		profile := projectProfileFor(project)
		if profile.AnalysisTaskID != taskID {
			return nil
		}
		profile.InitializationStatus = state
		profile.LastError = strings.TrimSpace(message)
		project.Profile = datatypes.NewJSONType(profile)
		if err := tx.Projects().Update(ctx, project); err != nil {
			return err
		}
		return persistProfileState(ctx, tx, projectID, profile)
	})
}

func (s *ProjectService) ProfileRevisions(ctx context.Context, userID, projectID string) ([]*model.ProjectProfileRevision, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return nil, ErrProjectOwnedByUser
	}
	return s.repo.ProjectProfileRevisions().ListByProject(ctx, projectID)
}

func (s *ProjectService) RestoreProfileRevision(ctx context.Context, userID, projectID string, revision, expectedVersion int64) (model.ProjectProfile, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return model.ProjectProfile{}, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return model.ProjectProfile{}, ErrProjectOwnedByUser
	}
	row, err := s.repo.ProjectProfileRevisions().FindByProjectRevision(ctx, projectID, revision)
	if err != nil {
		return model.ProjectProfile{}, fmt.Errorf("find profile revision: %w", err)
	}
	var dimensions model.ProjectProfileDimensions
	if err := json.Unmarshal(row.SixDimensions, &dimensions); err != nil {
		return model.ProjectProfile{}, fmt.Errorf("decode profile revision: %w", err)
	}
	next := projectProfileFor(project)
	next.Dimensions = dimensions
	next.Status = model.ProfileStatusConfirmed
	next.InitializationStatus = model.ProfileInitializationReady
	next.LastError = ""
	return s.updateProfileWithSource(ctx, userID, projectID, expectedVersion, next, "", "user_restore")
}

func (s *ProjectService) ConfirmProfile(ctx context.Context, userID, projectID string, expectedVersion int64, profile model.ProjectProfile) (model.ProjectProfile, error) {
	// The analysis task link is server-owned bookkeeping. Preserve the current
	// link while accepting only the six-dimensional content from Studio.
	current, err := s.GetProfile(ctx, userID, projectID)
	if err != nil {
		return model.ProjectProfile{}, err
	}
	profile.AnalysisTaskID = current.AnalysisTaskID
	profile.Status = model.ProfileStatusConfirmed
	for _, dimension := range []*model.ProfileDimension{&profile.Dimensions.Identity, &profile.Dimensions.Style, &profile.Dimensions.Audience, &profile.Dimensions.Platforms, &profile.Dimensions.Preferences, &profile.Dimensions.Memory} {
		if !containsProfileSource(dimension.Sources, "[用户编辑]") {
			dimension.Sources = append(dimension.Sources, "[用户编辑]")
		}
	}
	if profile.Version < 0 {
		return model.ProjectProfile{}, fmt.Errorf("invalid project profile version")
	}
	return s.updateProfile(ctx, userID, projectID, expectedVersion, profile)
}

// SetProfileAnalysisTaskID links the server-owned analysis task to the
// project-scoped draft without confirming any of its contents. The version
// check keeps a concurrent manual edit from being silently overwritten.
func (s *ProjectService) SetProfileAnalysisTaskID(ctx context.Context, userID, projectID string, expectedVersion int64, taskID string) (model.ProjectProfile, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return model.ProjectProfile{}, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return model.ProjectProfile{}, ErrProjectOwnedByUser
	}
	profile := projectProfileFor(project)
	profile.AnalysisTaskID = strings.TrimSpace(taskID)
	if !profile.IsConfirmed() {
		profile.Status = model.ProfileStatusDraft
	}
	return s.updateProfile(ctx, userID, projectID, expectedVersion, profile)
}

func (s *ProjectService) UpdateProfileDimension(ctx context.Context, userID, projectID, dimension string, expectedVersion int64, value model.ProfileDimension) (model.ProjectProfile, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return model.ProjectProfile{}, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return model.ProjectProfile{}, ErrProjectOwnedByUser
	}
	profile := projectProfileFor(project)
	currentDimension := func() model.ProfileDimension {
		switch dimension {
		case "identity":
			return profile.Dimensions.Identity
		case "style":
			return profile.Dimensions.Style
		case "audience":
			return profile.Dimensions.Audience
		case "platforms":
			return profile.Dimensions.Platforms
		case "preferences":
			return profile.Dimensions.Preferences
		case "memory":
			return profile.Dimensions.Memory
		default:
			return model.ProfileDimension{}
		}
	}
	previous := currentDimension()
	if len(value.Sources) == 0 {
		value.Sources = append([]string(nil), previous.Sources...)
	}
	if len(value.Evidence) == 0 {
		value.Evidence = append([]string(nil), previous.Evidence...)
	}
	if len(value.MissingFields) == 0 {
		value.MissingFields = append([]string(nil), previous.MissingFields...)
	}
	if !containsProfileSource(value.Sources, "[用户编辑]") {
		value.Sources = append(value.Sources, "[用户编辑]")
	}
	if dimension == "memory" {
		merged := map[string]any{}
		for key, item := range previous.Content {
			merged[key] = item
		}
		for key, item := range value.Content {
			merged[key] = item
		}
		value.Content = merged
	}
	switch dimension {
	case "identity":
		profile.Dimensions.Identity = value
	case "style":
		profile.Dimensions.Style = value
	case "audience":
		profile.Dimensions.Audience = value
	case "platforms":
		profile.Dimensions.Platforms = value
	case "preferences":
		profile.Dimensions.Preferences = value
	case "memory":
		profile.Dimensions.Memory = value
	default:
		return model.ProjectProfile{}, fmt.Errorf("unknown profile dimension %q", dimension)
	}
	return s.updateProfile(ctx, userID, projectID, expectedVersion, profile)
}

func containsProfileSource(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

// List returns projects for the given user, filtered by the provided options.
func (s *ProjectService) List(ctx context.Context, userID string, opts repository.ProjectListOptions) ([]*model.Project, error) {
	projects, err := s.repo.Projects().ListByUserID(ctx, userID, opts)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if s.imageAnalyses != nil {
		for _, project := range projects {
			if err := s.imageAnalyses.PresentProject(ctx, project); err != nil {
				return nil, fmt.Errorf("present project image analysis: %w", err)
			}
		}
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

// ListAgentIDsByProjectIDs returns the read-only Agent capability projection
// used by Studio project identity surfaces.
func (s *ProjectService) ListAgentIDsByProjectIDs(ctx context.Context, projectIDs []string) (map[string][]string, error) {
	ids, err := s.repo.Projects().ListAgentIDsByProjectIDs(ctx, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("list project Agent IDs: %w", err)
	}
	return ids, nil
}

func (s *ProjectService) ownedProject(ctx context.Context, userID, projectID string) (*model.Project, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	if project.UserID != userID {
		return nil, ErrProjectOwnedByUser
	}
	return project, nil
}

func validProjectChannel(channel string) bool {
	_, ok := supportedProjectChannels[strings.TrimSpace(channel)]
	return ok
}

func isSensitiveChannelConfigKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "private_key") || lower == "app_key"
}

func cloneChannelConfigMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			cloned[key] = cloneChannelConfigMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if itemMap, ok := item.(map[string]any); ok {
					items[i] = cloneChannelConfigMap(itemMap)
				} else {
					items[i] = item
				}
			}
			cloned[key] = items
		default:
			cloned[key] = value
		}
	}
	return cloned
}

// mergeChannelConfig preserves server-owned credentials omitted from a
// redacted client response while allowing ordinary values to be replaced.
func mergeChannelConfig(existing, incoming map[string]any) map[string]any {
	merged := cloneChannelConfigMap(existing)
	for key, value := range incoming {
		if existingMap, ok := existing[key].(map[string]any); ok {
			if incomingMap, ok := value.(map[string]any); ok {
				merged[key] = mergeChannelConfig(existingMap, incomingMap)
				continue
			}
		}
		merged[key] = value
	}
	for key, value := range existing {
		if isSensitiveChannelConfigKey(key) {
			if _, ok := incoming[key]; !ok {
				merged[key] = value
			}
		}
	}
	return merged
}

func (s *ProjectService) GetChannelConfig(ctx context.Context, userID, projectID, channel string) (*model.ProjectChannelConfig, error) {
	if !validProjectChannel(channel) {
		return nil, fmt.Errorf("%w: unsupported channel %q", ErrInvalidProjectChannel, channel)
	}
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	config, err := s.repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return config, err
}

func (s *ProjectService) ListChannelConfigs(ctx context.Context, userID, projectID string) ([]*model.ProjectChannelConfig, error) {
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	return s.repo.ProjectChannelConfigs().List(ctx, projectID)
}

func (s *ProjectService) UpsertChannelConfig(ctx context.Context, userID, projectID, channel string, config map[string]any) (*model.ProjectChannelConfig, error) {
	channel = strings.TrimSpace(channel)
	if !validProjectChannel(channel) {
		return nil, fmt.Errorf("%w: unsupported channel %q", ErrInvalidProjectChannel, channel)
	}
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	// Connector-specific validation is intentionally centralized here. Until a
	// connector publishes a schema, the channel whitelist is the minimum valid
	// contract and unknown keys remain opaque server-owned configuration.
	if existing, err := s.repo.ProjectChannelConfigs().Get(ctx, projectID, channel); err == nil {
		config = mergeChannelConfig(existing.Config.Data(), config)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row := &model.ProjectChannelConfig{ID: uuid.NewString(), ProjectID: projectID, Channel: channel}
	row.Config = datatypes.NewJSONType(config)
	if err := s.repo.ProjectChannelConfigs().Upsert(ctx, row); err != nil {
		return nil, fmt.Errorf("upsert project channel config: %w", err)
	}
	return s.repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
}

func (s *ProjectService) DeleteChannelConfig(ctx context.Context, userID, projectID, channel string) error {
	if !validProjectChannel(channel) {
		return fmt.Errorf("%w: unsupported channel %q", ErrInvalidProjectChannel, channel)
	}
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return err
	}
	return s.repo.ProjectChannelConfigs().Delete(ctx, projectID, channel)
}

// Update updates mutable fields on a project owned by the user.
func (s *ProjectService) Update(ctx context.Context, userID, projectID string, ch *model.Project) (*model.Project, error) {
	return s.update(ctx, userID, projectID, ch, "", false)
}

// SetFeedbackPaused changes only the periodic feedback switch. Keeping this
// mutation separate from the broad project update prevents an observability
// control from accidentally changing generation defaults.
func (s *ProjectService) SetFeedbackPaused(ctx context.Context, userID, projectID string, paused bool) (*model.Project, error) {
	var result *model.Project
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
		}
		if project.UserID != userID {
			return ErrProjectOwnedByUser
		}
		project.FeedbackPaused = paused
		if err := tx.Projects().Update(ctx, project); err != nil {
			return err
		}
		result = project
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("set feedback pause: %w", err)
	}
	return result, nil
}

func (s *ProjectService) UpdateIfReferenceImageAssetID(ctx context.Context, userID, projectID string, ch *model.Project, expectedID string) (*model.Project, error) {
	return s.update(ctx, userID, projectID, ch, expectedID, true)
}

func (s *ProjectService) update(ctx context.Context, userID, projectID string, ch *model.Project, expectedReferenceAssetID string, enforceReferenceCAS bool) (*model.Project, error) {
	var existing *model.Project
	var queued *model.ImageAnalysisJob
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		current, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
		}
		if current.UserID != userID {
			return ErrProjectOwnedByUser
		}
		if enforceReferenceCAS && current.ReferenceImageAssetID != expectedReferenceAssetID {
			return ErrProjectUpdateConflict
		}
		before := *current
		if err := s.applyProjectUpdate(current, ch); err != nil {
			return err
		}
		if s.imageAnalyses != nil {
			queued, err = s.imageAnalyses.updateProjectTx(ctx, tx, &before, current)
		} else {
			err = tx.Projects().Update(ctx, current)
		}
		if err != nil {
			return err
		}
		existing = current
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	if s.imageAnalyses != nil {
		s.imageAnalyses.enqueue(ctx, queued)
		if queued != nil {
			existing.ImageAnalysis = queued.View()
		} else if err := s.imageAnalyses.PresentProject(ctx, existing); err != nil {
			return nil, fmt.Errorf("present project image analysis: %w", err)
		}
	}
	return existing, nil
}

func (s *ProjectService) applyProjectUpdate(existing, ch *model.Project) error {
	// Apply updatable fields from ch to existing (only non-empty values).
	platformChanged := ch.Platform != "" && ch.Platform != existing.Platform
	if ch.Name != "" {
		existing.Name = ch.Name
	}
	if ch.Platform != "" {
		if !validProjectPlatform(ch.Platform) {
			return fmt.Errorf("invalid platform: %s", ch.Platform)
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
	if ch.VisualStyleSet {
		existing.VisualStyle = ch.VisualStyle
		existing.VisualStyleSet = true
	}
	if ch.Writer != "" {
		existing.Writer = ch.Writer
	}
	if ch.Theme != "" {
		existing.Theme = ch.Theme
	}
	// Shared project edits omit channel-specific defaults. Preserve the author
	// unless supplied, while allowing an explicit empty byline to clear it.
	if ch.AuthorSet || ch.Author != "" {
		existing.Author = ch.Author
	}
	if ch.ReferenceImageSet {
		existing.ReferenceImageAssetID = ch.ReferenceImageAssetID
	}
	if ch.PortraitReferenceImageSet {
		existing.PortraitReferenceImageAssetID = ch.PortraitReferenceImageAssetID
	}
	// Empty update input preserves the persisted value for image-capable
	// platforms. Platforms without image settings always keep the field empty.
	supportedImageRatios := model.SupportedImageRatios(existing.Platform)
	if len(supportedImageRatios) == 0 {
		if strings.TrimSpace(ch.ImageRatio) != "" {
			return fmt.Errorf("image_ratio is not supported for platform %s", existing.Platform)
		}
		existing.ImageRatio = ""
	} else {
		if strings.TrimSpace(ch.ImageRatio) != "" {
			if !model.IsBusinessImageRatioAllowed(existing.Platform, ch.ImageRatio) {
				return fmt.Errorf("%s: %s", model.ValidImageRatioHint, ch.ImageRatio)
			}
			existing.ImageRatio = ch.ImageRatio
		} else if platformChanged || strings.TrimSpace(existing.ImageRatio) == "" {
			existing.ImageRatio = model.DefaultImageRatio(existing.Platform)
		}
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
	if ch.HypitDefaultsSet {
		existing.HypitDefaults = ch.HypitDefaults
		existing.HypitDefaultsSet = true
	}
	if err := validateHypitProject(existing, s.hypitCapabilities); err != nil {
		return err
	}
	if ch.MontageDefaultsSet {
		existing.MontageDefaults = ch.MontageDefaults
	}
	if model.IsMontagePlatform(existing.Platform) || ch.MontageDefaultsSet {
		candidate := *existing
		candidate.MontageDefaultsSet = true
		if err := validateProjectMontageDefaults(&candidate, s.montageCapabilities); err != nil {
			return err
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
		if err := s.memory.DeleteProject(ctx, projectID); err != nil {
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
	// Channel credentials are loaded through dedicated channel-config routes and
	// are never embedded in the project response.
	if ch != nil {
		ch.RuntimeChannelConfig = nil
	}
}

// SanitizeProjectForResponse clears project secrets before API responses.
func (s *ProjectService) SanitizeProjectForResponse(ch *model.Project) {
	SanitizeProject(ch)
}
