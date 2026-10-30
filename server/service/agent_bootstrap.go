package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/auth"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"
)

type BootstrapFile struct {
	Path            string `json:"path"`
	Text            string `json:"text,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	ContentSHA256   string `json:"content_sha256,omitempty"`
	Mode            uint32 `json:"mode"`
	ExpectedSize    int64  `json:"expected_size,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	ReplaceExisting bool   `json:"replace_existing,omitempty"`
}

const (
	ArtifactTransportDirect = "direct"
	ArtifactTransportStream = "stream"
)

type ArtifactTransport struct {
	Mode string `json:"mode"`
}

type AgentRuntimeProfile struct {
	ProfileID          string                                    `json:"profile_id"`
	Provider           string                                    `json:"provider"`
	Protocol           string                                    `json:"protocol"`
	DisplayName        string                                    `json:"display_name"`
	ProfileFingerprint string                                    `json:"profile_fingerprint"`
	Envs               map[string]string                         `json:"envs"`
	ModelUsageAliases  map[string]serveragent.ModelUsageIdentity `json:"model_usage_aliases"`
}

type AgentBootstrapResponse struct {
	ExecutionToken string `json:"execution_token"`
	ExecutionID    string `json:"execution_id"`
	TaskID         string `json:"task_id"`
	// TaskType is a deprecated wire alias. New runtimes consume TaskKind.
	TaskType               string              `json:"task_type,omitempty"`
	AgentID                string              `json:"agent_id"`
	Channel                string              `json:"channel"`
	TaskKind               string              `json:"task_kind"`
	AgentPackID            string              `json:"agent_pack_id"`
	AgentPackVersion       string              `json:"agent_pack_version"`
	AgentPackDigest        string              `json:"agent_pack_digest"`
	AgentPackCatalogDigest string              `json:"agent_pack_catalog_digest"`
	RuntimeAdapter         string              `json:"runtime_adapter"`
	RuntimeProfile         string              `json:"runtime_profile"`
	ProjectID              string              `json:"project_id"`
	Prompt                 string              `json:"prompt"`
	ExecutionProfile       AgentRuntimeProfile `json:"execution_profile"`
	AgentFlag              string              `json:"agent_flag"`
	AgentMemoryDirectory   string              `json:"agent_memory_directory"`
	ResumeSessionID        string              `json:"resume_session_id,omitempty"`
	ResumeContextPath      string              `json:"resume_context_path,omitempty"`
	Env                    map[string]string   `json:"env,omitempty"`
	Files                  []BootstrapFile     `json:"files"`
	ArtifactTransport      ArtifactTransport   `json:"artifact_transport"`
	recoveryImages         []*model.TaskFile
}

type AgentBootstrapConfig struct {
	TokenTTL                time.Duration
	ActiveDeadline          time.Duration
	SignedURLTTL            int
	Store                   storage.Provider
	ImageAPIConfig          *srvconfig.ImageAPIConfig
	MontageToolPolicy       map[string]srvconfig.MontageToolCapabilityPolicy
	MontagePipelineDefaults map[string]map[string]any
	Hypit                   srvconfig.HypitConfig
	MontageEnv              map[string]string
	VideoEdit               srvconfig.VideoEditConfig
	TalkingHeadEditor       srvconfig.TalkingHeadEditorConfig
	Registry                *AgentProfileRegistry
	ProjectMemory           ProjectMemoryReader
}

type AgentBootstrapService struct {
	repo   repository.Repository
	tokens *auth.ExecutionTokenService
	cfg    AgentBootstrapConfig
	logger zerolog.Logger
	now    func() time.Time
}

var (
	ErrAgentBootstrapConflict    = errors.New("agent bootstrap state conflict")
	ErrAgentBootstrapUnavailable = errors.New("agent bootstrap dependency unavailable")
)

type publicationRecoveryArtifactSpec struct {
	path     string
	mimeType string
	maxBytes int64
	required bool
}

var publicationRecoveryArtifactSpecs = []publicationRecoveryArtifactSpec{
	{path: "output/04-article-final.md", mimeType: "text/markdown", maxBytes: 8 << 20, required: true},
	{path: "output/content-quality-report.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/marketing-scan.json", mimeType: "application/json", maxBytes: 2 << 20},
	{path: "output/seo-result.md", mimeType: "text/markdown", maxBytes: 2 << 20, required: true},
	{path: "output/visual-rhythm-plan.md", mimeType: "text/markdown", maxBytes: 4 << 20, required: true},
	{path: "output/cover-plan.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/cover-prompt.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/image-plan.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/images.json", mimeType: "application/json", maxBytes: 4 << 20},
	{path: "output/cover-quality.json", mimeType: "application/json", maxBytes: 4 << 20},
	{path: "output/05-article.html", mimeType: "text/html", maxBytes: 8 << 20, required: true},
	{path: "output/final-review.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/viral-audit.md", mimeType: "text/markdown", maxBytes: 4 << 20},
	{path: "output/draft.json", mimeType: "application/json", maxBytes: 8 << 20},
}

// Picture publication recovery starts from the delivered research/content
// artifacts and verified images. The old publish package is intentionally not
// restored: it is the object that failed Server validation and must be rebuilt
// by the recovery Agent.
var picturePublicationRecoveryArtifactSpecs = []publicationRecoveryArtifactSpec{
	{path: "output/topic-analysis.md", mimeType: "text/markdown", maxBytes: 4 << 20, required: true},
	{path: "output/content-dna.json", mimeType: "application/json", maxBytes: 4 << 20, required: true},
	{path: "output/content-script.md", mimeType: "text/markdown", maxBytes: 4 << 20, required: true},
	{path: "output/content.md", mimeType: "text/markdown", maxBytes: 8 << 20, required: true},
	{path: "output/image-plan.md", mimeType: "text/markdown", maxBytes: 4 << 20, required: true},
	{path: "output/image-prompts.md", mimeType: "text/markdown", maxBytes: 8 << 20, required: true},
	{path: "output/quality-review.md", mimeType: "text/markdown", maxBytes: 4 << 20, required: true},
}

func publicationRecoveryArtifactSpecsForTask(task *model.Task) ([]publicationRecoveryArtifactSpec, error) {
	if taskUsesChannel(task, model.ChannelWechatPicture) {
		return picturePublicationRecoveryArtifactSpecs, nil
	}
	if taskUsesChannel(task, model.ChannelArticle) {
		return publicationRecoveryArtifactSpecs, nil
	}
	return nil, fmt.Errorf("%w: publication recovery task channel is unsupported", ErrAgentBootstrapConflict)
}

var lowercaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NewAgentBootstrapService(repo repository.Repository, tokens *auth.ExecutionTokenService, cfg AgentBootstrapConfig, logger zerolog.Logger) *AgentBootstrapService {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	if cfg.ActiveDeadline > 0 && cfg.TokenTTL > cfg.ActiveDeadline {
		cfg.TokenTTL = cfg.ActiveDeadline
	}
	if cfg.SignedURLTTL <= 0 || time.Duration(cfg.SignedURLTTL)*time.Second > cfg.TokenTTL {
		cfg.SignedURLTTL = int(cfg.TokenTTL.Seconds())
	}
	return &AgentBootstrapService{repo: repo, tokens: tokens, cfg: cfg, logger: logger, now: time.Now}
}

func (s *AgentBootstrapService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *AgentBootstrapService) Bootstrap(ctx context.Context, identity *serveragent.WorkloadIdentity) (*AgentBootstrapResponse, error) {
	if s == nil || s.repo == nil || s.tokens == nil || identity == nil {
		return nil, errors.New("agent bootstrap is not configured")
	}
	if err := validateBootstrapWorkloadIdentity(identity, s.currentTime()); err != nil {
		return nil, err
	}
	execution, task, project, err := s.loadAndValidate(ctx, s.repo, identity)
	if err != nil {
		return nil, err
	}
	response, err := s.buildResponse(ctx, execution, task, project, identity.Deadline)
	if err != nil {
		return nil, err
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.TaskExecutions().SetRuntimeIdentity(ctx, identity.ExecutionID, identity.RuntimeIdentity); err != nil {
			if errors.Is(err, repository.ErrRuntimeIdentityConflict) || errors.Is(err, repository.ErrRuntimeIdentityInactive) {
				return fmt.Errorf("%w: workload runtime identity changed", ErrAgentBootstrapConflict)
			}
			return err
		}
		execution, _, _, err := s.loadAndValidate(ctx, tx, identity)
		if err != nil {
			return err
		}
		if execution.Status == model.TaskExecutionStarting {
			won, err := tx.TaskExecutions().Transition(ctx, execution.ID, []string{model.TaskExecutionStarting}, model.TaskExecutionRunning, model.ExecutionTransition{Started: true, RuntimeInstanceID: identity.InstanceID})
			if err != nil {
				return err
			}
			if !won {
				refreshed, _, _, err := s.loadAndValidate(ctx, tx, identity)
				if err != nil {
					return err
				}
				if refreshed.Status != model.TaskExecutionRunning {
					return fmt.Errorf("%w: execution bootstrap state changed concurrently", ErrAgentBootstrapConflict)
				}
			}
			if won {
				for _, file := range response.recoveryImages {
					if _, err := tx.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, execution.ID, file); err != nil {
						return err
					}
				}
			}
		}
		now := time.Now()
		if err := tx.TaskExecutions().UpdateHeartbeat(ctx, execution.ID, now); err != nil {
			return err
		}
		return tx.Tasks().UpdateHeartbeat(ctx, task.ID)
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func validateBootstrapWorkloadIdentity(identity *serveragent.WorkloadIdentity, now time.Time) error {
	for _, member := range []struct {
		name  string
		value string
	}{
		{name: "target", value: identity.Target},
		{name: "scope", value: identity.Scope},
		{name: "workload", value: identity.Workload},
		{name: "instance", value: identity.InstanceID},
		{name: "execution", value: identity.ExecutionID},
		{name: "task", value: identity.TaskID},
		{name: "project", value: identity.ProjectID},
		{name: "user", value: identity.UserID},
	} {
		trimmed := strings.TrimSpace(member.value)
		if trimmed == "" || trimmed != member.value {
			return fmt.Errorf("%w: verified workload %s identity is incomplete or non-canonical", ErrAgentBootstrapConflict, member.name)
		}
	}
	if identity.Deadline.IsZero() || !identity.Deadline.After(now) {
		return fmt.Errorf("%w: verified workload deadline is not in the future", ErrAgentBootstrapConflict)
	}
	return nil
}

func (s *AgentBootstrapService) loadAndValidate(ctx context.Context, repo repository.Repository, identity *serveragent.WorkloadIdentity) (*model.TaskExecution, *model.Task, *model.Project, error) {
	execution, err := repo.TaskExecutions().FindByID(ctx, identity.ExecutionID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find execution: %w", err)
	}
	task, err := repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find task: %w", err)
	}
	project, err := repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find project: %w", err)
	}
	user, err := repo.Users().FindByID(ctx, task.UserID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find execution owner: %w", err)
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID || execution.TaskID != identity.TaskID || task.ID != identity.TaskID || task.ProjectID != identity.ProjectID || task.UserID != identity.UserID || project.ID != identity.ProjectID || project.UserID != identity.UserID || user.ID != identity.UserID {
		return nil, nil, nil, fmt.Errorf("%w: workload identity does not match current task execution ownership", ErrAgentBootstrapConflict)
	}
	if execution.Target != identity.Target || execution.RuntimeScope != identity.Scope || execution.RuntimeWorkload != identity.Workload {
		return nil, nil, nil, fmt.Errorf("%w: workload runtime identity mismatch", ErrAgentBootstrapConflict)
	}
	if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		return nil, nil, nil, fmt.Errorf("%w: task is terminal", ErrAgentBootstrapConflict)
	}
	switch execution.Status {
	case model.TaskExecutionStarting:
		if execution.RuntimeInstanceID != "" && execution.RuntimeInstanceID != identity.InstanceID {
			return nil, nil, nil, fmt.Errorf("%w: execution is bound to another runtime instance", ErrAgentBootstrapConflict)
		}
	case model.TaskExecutionRunning:
		if !execution.Started || execution.RuntimeInstanceID == "" || execution.RuntimeInstanceID != identity.InstanceID {
			return nil, nil, nil, fmt.Errorf("%w: running execution is bound to another runtime instance", ErrAgentBootstrapConflict)
		}
	default:
		return nil, nil, nil, fmt.Errorf("%w: execution status %q cannot bootstrap", ErrAgentBootstrapConflict, execution.Status)
	}
	return execution, task, project, nil
}

func (s *AgentBootstrapService) buildResponse(ctx context.Context, execution *model.TaskExecution, task *model.Task, project *model.Project, workloadDeadline time.Time) (*AgentBootstrapResponse, error) {
	if task != nil && task.Type == model.TaskTypeProfileAnalysis {
		if err := UpdateProfileLifecycle(ctx, s.repo, task.ProjectID, task.ID, model.ProfileInitializationRunning, ""); err != nil {
			return nil, fmt.Errorf("mark profile analysis running: %w", err)
		}
	}
	issuedAt := s.currentTime()
	tokenTTL := s.cfg.TokenTTL
	if task != nil && model.IsHypitPlatform(task.Type) {
		required := time.Duration(readHypitSnapshot(task).Limits.TimeoutMinutes) * time.Minute
		if tokenTTL < required {
			tokenTTL = required
		}
	}
	if task != nil && model.IsVideoEditPlatform(task.Type) {
		required := task.VideoEditTimeout()
		if execution != nil && execution.ActiveDeadlineSeconds > 0 {
			required = time.Duration(execution.ActiveDeadlineSeconds) * time.Second
		}
		if tokenTTL < required {
			tokenTTL = required
		}
	}
	if task != nil && model.IsTalkingHeadEditorPlatform(task.Type) {
		required := time.Duration(s.cfg.TalkingHeadEditor.TimeoutMinutes) * time.Minute
		if execution != nil && execution.ActiveDeadlineSeconds > 0 {
			required = time.Duration(execution.ActiveDeadlineSeconds) * time.Second
		}
		if tokenTTL < required {
			tokenTTL = required
		}
	}
	credentialDeadline := issuedAt.Add(tokenTTL)
	if workloadDeadline.Before(credentialDeadline) {
		credentialDeadline = workloadDeadline
	}
	// JWT NumericDate serializes at whole-second precision. Use that exact
	// boundary for download signing as well, never a rounded-up duration.
	credentialDeadline = credentialDeadline.UTC().Truncate(time.Second)
	if !credentialDeadline.After(issuedAt) || credentialDeadline.Sub(issuedAt) < time.Second {
		return nil, fmt.Errorf("%w: workload has no positive bootstrap credential lifetime", ErrAgentBootstrapConflict)
	}
	effective := serveragent.EffectiveProject(project, task)
	feedbackFile := BootstrapFile{}
	if s.repo != nil && s.repo.FeedbackLoop() != nil {
		// The first bootstrap freezes the strategy for this execution. Retries
		// reuse that context so a later monthly activation cannot affect a task
		// that is already running.
		gc, err := s.repo.FeedbackLoop().FindGenerationContext(ctx, execution.ID)
		if err != nil {
			return nil, fmt.Errorf("load generation context: %w", err)
		}
		var strategy *model.StrategySnapshot
		frozenStrategy := gc != nil && gc.StrategySnapshotID != ""
		if frozenStrategy {
			strategy, err = s.repo.FeedbackLoop().FindStrategyByID(ctx, gc.StrategySnapshotID)
			if err != nil {
				return nil, fmt.Errorf("load frozen feedback strategy: %w", err)
			}
		} else if gc == nil {
			strategyChannel := strings.TrimSpace(task.Channel)
			strategy, err = s.repo.FeedbackLoop().FindActiveStrategy(ctx, task.ProjectID, strategyChannel)
			if err != nil {
				return nil, fmt.Errorf("load feedback strategy: %w", err)
			}
			gc = &model.GenerationContext{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, ProjectID: task.ProjectID, StrategyMode: "advisory"}
		}
		strategyPayload := map[string]any{"mode": "advisory", "available": false, "strategy_unavailable": "no_active_strategy"}
		// A strategy is validated when a new execution freezes it. A retry of the
		// same execution keeps consuming that frozen snapshot even after it is
		// retired by a newer monthly activation.
		strategyChannel := strings.TrimSpace(task.Channel)
		strategyTaskKind := strings.TrimSpace(task.TaskKind)
		if strategyTaskKind == "" {
			strategyTaskKind = strings.TrimSpace(task.Type)
		}
		strategyUsable := strategy != nil && (frozenStrategy || feedbackStrategyUsable(strategy, strategyTaskKind, strategyChannel, issuedAt))
		if strategyUsable {
			gc.StrategySnapshotID, gc.StrategyRevision, gc.StrategyDigest = strategy.ID, strategy.Revision, strategy.Digest
			strategyPayload = map[string]any{"mode": "advisory", "available": true, "strategy_snapshot_id": strategy.ID, "strategy_revision": strategy.Revision, "strategy_digest": strategy.Digest, "recommendations": json.RawMessage(strategy.Recommendations), "evidence": json.RawMessage(strategy.Evidence), "limitations": strategy.Limitations}
		} else {
			if gc.UnavailableReason == "" {
				gc.UnavailableReason = "strategy_not_applicable"
			}
		}
		if gc.ID != "" {
			if err := s.repo.FeedbackLoop().CreateGenerationContext(ctx, gc); err != nil {
				return nil, fmt.Errorf("persist generation context: %w", err)
			}
		}
		strategyBytes, err := json.Marshal(strategyPayload)
		if err != nil {
			return nil, fmt.Errorf("marshal feedback strategy: %w", err)
		}
		feedbackFile = BootstrapFile{Path: ".anban-creator/feedback-strategy.json", Text: string(strategyBytes), Mode: 0600, ReplaceExisting: true}
	}
	taskReferenceAsset, projectStyleReferenceAsset, err := resolveRuntimeReferenceAssets(ctx, s.repo, task)
	if err != nil {
		return nil, fmt.Errorf("resolve reference asset: %w", err)
	}
	var files []BootstrapFile
	portraitAsset, err := resolveProjectPortraitReferenceAsset(ctx, s.repo, task)
	if err != nil {
		return nil, fmt.Errorf("resolve project portrait reference: %w", err)
	}
	if portraitAsset != nil {
		signed, err := s.signedReferenceAssetURL(ctx, portraitAsset, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign project portrait reference: %w", err)
		}
		files = append(files, BootstrapFile{
			Path: serveragent.ProjectPortraitReferenceImagePath, DownloadURL: signed, Mode: 0644,
			ExpectedSize: portraitAsset.Size, MaxBytes: 10 << 20,
		})
	}
	appCfg, err := serveragent.BuildAppConfig(effective, resolver.ResolveStyle(effective, task), s.cfg.ImageAPIConfig, task.ImageRatio, taskReferenceAsset != nil)
	if err != nil {
		return nil, fmt.Errorf("build runtime settings: %w", err)
	}
	// Bootstrap credentials are execution-scoped. Provider and publishing secrets
	// remain server-side and are reached through authenticated MCP calls.
	appCfg.Wechat.AppID, appCfg.Wechat.Secret = "", ""
	appCfg.Wechat.Article.Cover.Image.Key = ""
	appCfg.Wechat.Article.Content.Image.Key = ""
	if appCfg.Seednote != nil {
		appCfg.Seednote.Cover.Image.Key = ""
		appCfg.Seednote.Content.Image.Key = ""
	}
	settings, err := json.Marshal(appCfg)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime settings: %w", err)
	}
	files = append(files, BootstrapFile{Path: ".anban-creator/settings.json", Text: string(settings), Mode: 0600, ReplaceExisting: true})
	// Every execution receives a read-only snapshot. The database profile is
	// authoritative; versioned NAS projections are used only when current.
	profile := projectProfileForAgentContext(project)
	if model.IsContentPlanningTask(task.Type) {
		profile = task.ProjectSnapshot.Data().Profile
	}
	memoryFiles := map[string]BootstrapFile{}
	if !model.IsContentPlanningTask(task.Type) && s.cfg.ProjectMemory != nil && strings.TrimSpace(task.ProjectID) != "" {
		loaded, err := readProjectBootstrapFiles(ctx, s.cfg.ProjectMemory, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("read project memory context: %w", err)
		}
		for _, file := range loaded {
			memoryFiles[file.Path] = file
		}
	}
	if file, ok := memoryFiles["AGENTS.md"]; !ok || !profileProjectionMatchesVersion(file.Text, profile.Version) {
		content := ProfileAgentsMarkdownAtVersion(profile.Version)
		if instructions := strings.TrimSpace(effective.Instructions); instructions != "" {
			content += "\n\n## 项目定位\n\n" + instructions + "\n"
		}
		memoryFiles["AGENTS.md"] = BootstrapFile{Path: "AGENTS.md", Text: content, Mode: 0644}
	}
	for _, dimension := range model.ProfileDimensions() {
		path := "profile/" + dimension + ".md"
		if file, ok := memoryFiles[path]; ok && profileProjectionMatchesVersion(file.Text, profile.Version) {
			continue
		}
		content, err := ProfileDimensionMarkdownAtVersion(dimension, profileDimension(profile, dimension), profile.Version)
		if err != nil {
			return nil, fmt.Errorf("render profile input %s: %w", dimension, err)
		}
		memoryFiles[path] = BootstrapFile{Path: path, Text: content, Mode: 0644}
	}
	for _, path := range projectMemoryContextPaths {
		if file, ok := memoryFiles[path]; ok {
			files = append(files, file)
		}
	}
	if feedbackFile.Path != "" {
		files = append(files, feedbackFile)
	}
	attachments := task.InputAttachments.Data()
	if taskReferenceAsset != nil {
		signed, err := s.signedReferenceAssetURL(ctx, taskReferenceAsset, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign task reference image: %w", err)
		}
		files = append(files, BootstrapFile{
			Path: serveragent.TaskReferenceImagePath, DownloadURL: signed, Mode: 0644,
			ExpectedSize: taskReferenceAsset.Size, MaxBytes: 10 << 20,
		})
	}
	if projectStyleReferenceAsset != nil {
		signed, err := s.signedReferenceAssetURL(ctx, projectStyleReferenceAsset, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign project style reference image: %w", err)
		}
		files = append(files, BootstrapFile{
			Path: serveragent.ProjectStyleReferenceImagePath, DownloadURL: signed, Mode: 0644,
			ExpectedSize: projectStyleReferenceAsset.Size, MaxBytes: 10 << 20,
		})
	}
	planningFiles, err := s.buildContentPlanningFiles(ctx, execution, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, planningFiles...)
	attachmentFiles, err := s.buildAttachmentFiles(ctx, execution.ID, task, attachments, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, attachmentFiles...)
	montageFiles, err := s.buildMontageFiles(ctx, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, montageFiles...)
	localLifeFiles, err := s.buildLocalLifeFiles(ctx, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, localLifeFiles...)
	hypitFiles, err := s.buildHypitFiles(ctx, task, credentialDeadline, workloadDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, hypitFiles...)
	videoEditFiles, err := s.buildVideoEditFiles(ctx, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, videoEditFiles...)
	talkingHeadFiles, err := s.buildTalkingHeadEditorFiles(ctx, execution, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, talkingHeadFiles...)
	recoveryFiles, err := s.buildPublicationRecoveryFiles(ctx, execution, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, recoveryFiles...)
	recoveryImageFiles, recoveryImages, err := s.buildPublicationRecoveryImages(ctx, execution, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, recoveryImageFiles...)
	if err := validateBootstrapFilesForTask(files, task); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
	}
	tokenIssuedAt := s.currentTime()
	if !credentialDeadline.After(tokenIssuedAt) || credentialDeadline.Sub(tokenIssuedAt) < time.Second {
		return nil, fmt.Errorf("%w: workload has no positive bootstrap credential lifetime", ErrAgentBootstrapConflict)
	}
	token, err := s.tokens.IssueAt(auth.ExecutionClaims{UserID: task.UserID, ProjectID: task.ProjectID, TaskID: task.ID, ExecutionID: execution.ID}, tokenIssuedAt, credentialDeadline)
	if err != nil {
		return nil, err
	}
	bootstrapTaskType := taskTypeForIdentity(task.AgentID, task.Channel, task.TaskKind)
	if bootstrapTaskType == "" {
		bootstrapTaskType = task.Type
	}
	prompt := serveragent.BuildUserPrompt(serveragent.UserPromptParams{
		TaskType: bootstrapTaskType, Topic: task.Prompt, TaskID: task.ID, ProjectID: task.ProjectID,
		ReuseRunID: task.ReuseRunID, ContentAssetID: task.ContentAssetID,
		ImageRatio: task.ImageRatio, HasReferenceImage: taskReferenceAsset != nil,
		HasContentImage: task.HasContentImage, HasTailImage: task.HasTailImage,
		ArticleWithCover: task.ArticleWithCover, ArticleWithContentImages: task.ArticleWithContentImages,
		CoverUsePortrait: task.CoverUsePortrait,
	})
	if execution.Purpose == model.TaskExecutionPurposePublicationRecovery && task.Outcome != nil && canRepairPublicationPackage(task.Outcome.Publication) {
		if taskUsesChannel(task, model.ChannelWechatPicture) {
			prompt += "\n\n公众号贴图发布包修复：\n" +
				"- Server 已恢复原执行的选题、Content DNA、文案、图片规划、质量报告和已验证图片；保持这些内容与图片顺序不变，不能恢复或读取旧的 output/publish-package.json。\n" +
				"- 重新检查质量报告和图片路径后，只重建 output/publish-package.json。JSON 顶层只能包含 schema_version、status、source、data_at、missing、title、digest、content、cover_path、image_paths、readiness；readiness 只能包含 status。\n" +
				"- schema_version 必须是 1.0，source 必须是 wechat-picture-agent；status 与 readiness.status 必须一致且只能是 ready 或 blocked，ready 时 missing 必须为空。content 是纯文本图下注释；cover_path 和 image_paths 必须是具体 output 相对路径，禁止通配符。严禁添加 notes、image_order、crop、cover、images、caption、text、html 或任何其他字段；图片元数据和微信请求由 Server finalizer 管理。"
		} else {
			prompt += "\n\n发布包修复：\n" +
				"- Server 已恢复原执行的文章、图片与审核产物；保持正文不变，复用已经成功且可验证的图片，只补齐缺失或失败的已请求图片。\n" +
				"- 原发布包未通过校验。必须重新执行内容质量、营销合规、最终审核和爆款审核，更新对应证据文件；不得直接沿用或强行改写 readiness 为 ready。审核仍有阻断时保留 blocked 及实际原因。\n" +
				"- 依据本次审核结果重建 output/draft.json 并校验最终 HTML 哈希、标题、摘要及证据路径。1.0 顶层只能包含 schema_version、article、readiness；article 只能包含 title、digest、content_path、content_sha256；readiness 只能包含 status、code、evidence_paths。不得添加 cover、images 或其他字段，图片元数据由 Server 管理。"
		}
	} else if execution.Purpose == model.TaskExecutionPurposePublicationRecovery {
		if taskUsesChannel(task, model.ChannelWechatPicture) {
			prompt += "\n\n公众号贴图视觉生成恢复：\n" +
				"- 从 image_generation 阶段继续；Server 已恢复并校验原执行的选题、文案、图片规划、质量报告和已验证图片。\n" +
				"- 不得重新执行选题或正文创作，也不得改写已恢复的文案；只重新生成缺失或失败的已请求图片，然后按实际具体路径重建发布包。\n" +
				"- 发布包必须严格使用 Server 合同：顶层只能包含 schema_version、status、source、data_at、missing、title、digest、content、cover_path、image_paths、readiness，readiness 只能包含 status；禁止 notes、image_order、crop、cover、images、caption、text、html、通配符路径及任何其他字段。"
		} else {
			prompt += "\n\n视觉生成恢复：\n" +
				"- 从 image_generation 阶段继续；Server 已恢复并校验原执行的文章、SEO、视觉规划、HTML 和审核产物。\n" +
				"- 不得重新执行选题、正文创作、SEO 或语义审核，也不得改写已恢复的文章内容。\n" +
				"- 只重新生成缺失或失败的已请求图片，然后更新图片引用、最终 HTML 和 output/draft.json；已成功且可验证的图片不得重复生成。\n" +
				"- 重新校验并修复 output/draft.json 的 1.0 结构：顶层只能包含 schema_version、article、readiness；article 只能包含 title、digest、content_path、content_sha256；readiness 只能包含 status、code、evidence_paths。不得添加 cover、images 或其他字段，图片元数据由 Server 管理。"
		}
	}
	resumeContextPath := ""
	for _, attachment := range attachments {
		if attachment.Role == model.EntryAttachmentRoleResumeLatest {
			resumeContextPath, err = serveragent.ExecutionResumeContextPath(execution.ID)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
			}
			break
		}
	}
	executionProfile, err := s.resolveExecutionProfile(execution, task)
	if err != nil {
		return nil, err
	}
	runtimeEnv := executionProfile.RuntimeEnv()
	if err := model.ValidateClaudeProfileEnvs(runtimeEnv, true); err != nil {
		return nil, fmt.Errorf("%w: invalid Claude profile environment: %w", ErrAgentBootstrapUnavailable, err)
	}
	aliases := make(map[string]serveragent.ModelUsageIdentity, len(executionProfile.ModelUsageAliases))
	for raw, target := range executionProfile.ModelUsageAliases {
		aliases[raw] = serveragent.ModelUsageIdentity{Provider: executionProfile.Provider, Model: target}
	}
	if err := serveragent.ValidateModelUsageAliases(aliases); err != nil {
		return nil, fmt.Errorf("%w: invalid Claude model usage aliases: %w", ErrAgentBootstrapUnavailable, err)
	}
	packID := strings.TrimSpace(execution.AgentPackID)
	if packID == "" {
		packID = strings.TrimSpace(execution.AgentID)
	}
	pack, ok := agentpack.Default().ForAgent(packID)
	if !ok && strings.TrimSpace(execution.AgentID) == "" {
		packID = strings.TrimSpace(task.AgentID)
		if packID == "" {
			packID = serveragent.TaskTypeToAgent(task.Type)
		}
		pack, ok = agentpack.Default().ForAgent(packID)
	}
	if !ok || strings.TrimSpace(pack.Agent.Name) == "" {
		return nil, fmt.Errorf("%w: Agent Pack %q has no valid agent name", ErrAgentBootstrapConflict, packID)
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`).MatchString(pack.Agent.Name) {
		return nil, fmt.Errorf("%w: Agent Pack %q has invalid agent name", ErrAgentBootstrapConflict, packID)
	}
	return &AgentBootstrapResponse{
		ExecutionToken: token, ExecutionID: execution.ID, TaskID: task.ID, TaskType: bootstrapTaskType,
		AgentID: execution.AgentID, Channel: execution.Channel, TaskKind: execution.TaskKind,
		AgentPackID: execution.AgentPackID, AgentPackVersion: execution.AgentPackVersion,
		AgentPackDigest: execution.AgentPackDigest, AgentPackCatalogDigest: agentpack.CatalogDigest(), RuntimeAdapter: execution.RuntimeAdapter, RuntimeProfile: execution.RuntimeProfile,
		ProjectID: task.ProjectID, Prompt: prompt,
		ExecutionProfile: AgentRuntimeProfile{
			ProfileID: executionProfile.ID, Provider: executionProfile.Provider, Protocol: executionProfile.Protocol,
			DisplayName: executionProfile.DisplayName, ProfileFingerprint: task.AgentProfileFingerprint,
			Envs: model.CloneClaudeProfileEnvs(runtimeEnv), ModelUsageAliases: aliases,
		},
		AgentFlag:            "anban:" + pack.Agent.Name,
		AgentMemoryDirectory: ".claude/agent-memory", ResumeSessionID: execution.ResumeSessionID, ResumeContextPath: resumeContextPath,
		Env: s.runtimeEnv(task), Files: files, ArtifactTransport: ArtifactTransport{Mode: s.artifactTransportMode()},
		recoveryImages: recoveryImages,
	}, nil
}

func (s *AgentBootstrapService) buildPublicationRecoveryFiles(ctx context.Context, execution *model.TaskExecution, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if execution == nil || execution.Purpose != model.TaskExecutionPurposePublicationRecovery {
		return nil, nil
	}
	if task == nil {
		return nil, fmt.Errorf("%w: publication recovery task is required", ErrAgentBootstrapConflict)
	}
	specs, err := publicationRecoveryArtifactSpecsForTask(task)
	if err != nil {
		return nil, err
	}
	sourceExecutionID := strings.TrimSpace(execution.ParentExecutionID)
	if sourceExecutionID == "" || sourceExecutionID != execution.ParentExecutionID || sourceExecutionID == execution.ID {
		return nil, fmt.Errorf("%w: publication recovery source execution is invalid", ErrAgentBootstrapConflict)
	}
	source, err := s.repo.TaskExecutions().FindByID(ctx, sourceExecutionID)
	if err != nil {
		return nil, fmt.Errorf("find publication recovery source execution: %w", err)
	}
	if source.TaskID != task.ID {
		return nil, fmt.Errorf("%w: publication recovery source execution is not owned by task", ErrAgentBootstrapConflict)
	}
	if !isTerminalExecution(source.Status) {
		return nil, fmt.Errorf("%w: publication recovery source execution is not terminal", ErrAgentBootstrapConflict)
	}
	sourceFiles, err := s.repo.TaskFiles().FindByExecutionID(ctx, source.ID)
	if err != nil {
		return nil, fmt.Errorf("list publication recovery source artifacts: %w", err)
	}
	byPath := make(map[string]*model.TaskFile, len(sourceFiles))
	for _, file := range sourceFiles {
		if file == nil || file.TaskID != task.ID || file.ExecutionID != source.ID || file.State != model.TaskFileStateDelivered {
			continue
		}
		if _, allowed := findPublicationRecoveryArtifactSpecForTask(task, file.FilePath); allowed {
			byPath[file.FilePath] = file
		}
	}
	expectedStorageProvider := ""
	if s.cfg.Store != nil {
		expectedStorageProvider = s.cfg.Store.Name()
	}
	expectedPrefix := buildTaskMCPArtifactStoragePrefix(task, source.ID)
	for _, spec := range specs {
		file := byPath[spec.path]
		if file == nil {
			if spec.required {
				return nil, fmt.Errorf("%w: publication recovery artifact %q is unavailable", ErrAgentBootstrapConflict, spec.path)
			}
			continue
		}
		validHash := lowercaseSHA256.MatchString(file.ContentHash)
		validObjectKey := file.OSSKey == expectedPrefix+spec.path
		if validHash {
			validObjectKey = validObjectKey || file.OSSKey == buildTaskArtifactFinalStorageKey(task, source.ID, file.ContentHash, spec.path)
		}
		if file.MimeType != spec.mimeType || file.FileSize < 0 || file.FileSize > spec.maxBytes ||
			!validHash || file.StorageProvider != expectedStorageProvider || !validObjectKey {
			return nil, fmt.Errorf("%w: publication recovery artifact %q is invalid or unavailable", ErrAgentBootstrapConflict, spec.path)
		}
	}
	files := make([]BootstrapFile, 0, len(specs))
	for _, spec := range specs {
		file := byPath[spec.path]
		if file == nil {
			continue
		}
		signed, err := s.signedBootstrapObjectKey(ctx, file.OSSKey, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign publication recovery artifact %q: %w", spec.path, err)
		}
		files = append(files, BootstrapFile{
			Path: spec.path, DownloadURL: signed, ContentSHA256: file.ContentHash,
			Mode: 0644, ExpectedSize: file.FileSize, MaxBytes: spec.maxBytes, ReplaceExisting: true,
		})
	}
	return files, nil
}

func findPublicationRecoveryArtifactSpec(path string) (publicationRecoveryArtifactSpec, bool) {
	for _, spec := range publicationRecoveryArtifactSpecs {
		if spec.path == path {
			return spec, true
		}
	}
	return publicationRecoveryArtifactSpec{}, false
}

func findPublicationRecoveryArtifactSpecForTask(task *model.Task, path string) (publicationRecoveryArtifactSpec, bool) {
	if task == nil {
		return findPublicationRecoveryArtifactSpec(path)
	}
	specs, err := publicationRecoveryArtifactSpecsForTask(task)
	if err != nil {
		return publicationRecoveryArtifactSpec{}, false
	}
	for _, spec := range specs {
		if spec.path == path {
			return spec, true
		}
	}
	return publicationRecoveryArtifactSpec{}, false
}

func (s *AgentBootstrapService) resolveExecutionProfile(execution *model.TaskExecution, task *model.Task) (AgentExecutionProfile, error) {
	if s == nil || s.cfg.Registry == nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: agent profile registry is required", ErrAgentBootstrapUnavailable)
	}
	if execution == nil || task == nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: task execution profile is required", ErrAgentBootstrapConflict)
	}
	snapshot := task.AgentProfileSnapshot
	if execution.ExecutionProfile != task.ExecutionProfile || task.ExecutionProfile != snapshot.ProfileID ||
		execution.Provider != snapshot.Provider || !reflect.DeepEqual(execution.ProfileEnvs, snapshot.Envs) ||
		execution.ProfileFingerprint != task.AgentProfileFingerprint {
		return AgentExecutionProfile{}, fmt.Errorf("%w: execution profile identity does not match task snapshot", ErrAgentBootstrapConflict)
	}
	profile, err := s.cfg.Registry.ResolveRuntime(task.ExecutionProfile, snapshot, task.AgentProfileFingerprint)
	if err != nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %v", ErrAgentBootstrapUnavailable, err)
	}
	return profile, nil
}

func (s *AgentBootstrapService) artifactTransportMode() string {
	if s != nil && s.cfg.Store != nil && strings.EqualFold(strings.TrimSpace(s.cfg.Store.Name()), "oss") {
		return ArtifactTransportDirect
	}
	return ArtifactTransportStream
}

func (s *AgentBootstrapService) signedReferenceAssetURL(ctx context.Context, asset *model.Asset, credentialDeadline time.Time) (string, error) {
	if s.cfg.Store == nil {
		return "", fmt.Errorf("%w: storage provider is required for bootstrap downloads", ErrAgentBootstrapUnavailable)
	}
	if asset == nil || strings.TrimSpace(asset.StorageKey) == "" {
		return "", fmt.Errorf("%w: reference asset storage key is unavailable", ErrAgentBootstrapConflict)
	}
	signingNow := s.currentTime()
	ttl := int(credentialDeadline.Unix()-signingNow.Unix()) - 1
	if s.cfg.SignedURLTTL > 0 && ttl > s.cfg.SignedURLTTL {
		ttl = s.cfg.SignedURLTTL
	}
	if ttl <= 0 {
		return "", fmt.Errorf("%w: no positive signed download lifetime remains", ErrAgentBootstrapConflict)
	}
	signed, err := s.cfg.Store.DownloadURL(ctx, asset.StorageKey, ttl)
	if err != nil {
		return "", fmt.Errorf("%w: sign reference asset: %w", ErrAgentBootstrapUnavailable, err)
	}
	if strings.TrimSpace(signed) == "" {
		return "", fmt.Errorf("%w: storage returned an empty signed download URL", ErrAgentBootstrapUnavailable)
	}
	if s.currentTime().Unix()+int64(ttl) > credentialDeadline.Unix() {
		return "", fmt.Errorf("%w: signed download crossed the credential deadline", ErrAgentBootstrapConflict)
	}
	return signed, nil
}

func (s *AgentBootstrapService) montageEnv(task *model.Task) map[string]string {
	if task != nil && model.IsHypitPlatform(task.Type) {
		env := map[string]string{}
		for k, v := range s.cfg.Hypit.Env {
			if v != "" {
				env[k] = v
			}
		}
		return env
	}
	if task == nil || !model.IsMontagePlatform(task.Type) {
		return nil
	}
	env := make(map[string]string, len(s.cfg.MontageEnv))
	for key, value := range s.cfg.MontageEnv {
		if strings.TrimSpace(value) != "" {
			env[key] = value
		}
	}
	return env
}

func (s *AgentBootstrapService) runtimeEnv(task *model.Task) map[string]string {
	if task != nil && model.IsVideoEditPlatform(task.Type) {
		env := make(map[string]string, len(s.cfg.VideoEdit.Env)+1)
		for key, value := range s.cfg.VideoEdit.Env {
			if strings.TrimSpace(value) != "" {
				env[key] = value
			}
		}
		if key := strings.TrimSpace(s.cfg.VideoEdit.ElevenLabsAPIKey); key != "" {
			env["ELEVENLABS_API_KEY"] = key
		}
		return env
	}
	if task != nil && model.IsTalkingHeadEditorPlatform(task.Type) {
		env := make(map[string]string, len(s.cfg.TalkingHeadEditor.Env)+1)
		for key, value := range s.cfg.TalkingHeadEditor.Env {
			if strings.TrimSpace(value) != "" {
				env[key] = value
			}
		}
		if key := strings.TrimSpace(s.cfg.TalkingHeadEditor.VideoUseAPIKey); key != "" {
			env["ELEVENLABS_API_KEY"] = key
		}
		return env
	}
	return s.montageEnv(task)
}

func (s *AgentBootstrapService) buildMontageFiles(ctx context.Context, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if task == nil || !model.IsMontagePlatform(task.Type) {
		return nil, nil
	}
	input := task.MontageInput.Data()
	input.SourceAssets = append([]model.MontageAsset(nil), input.SourceAssets...)
	assetFiles, err := s.materializeMontageTaskFileSources(ctx, task, &input, credentialDeadline)
	if err != nil {
		return nil, err
	}
	values := []struct {
		path  string
		value any
	}{
		{"montage-input.json", input},
		{"montage-tool-policy.json", s.cfg.MontageToolPolicy},
		{"montage-pipeline-defaults.json", s.cfg.MontagePipelineDefaults},
	}
	files := make([]BootstrapFile, 0, len(assetFiles)+len(values))
	files = append(files, assetFiles...)
	for _, value := range values {
		raw, err := json.MarshalIndent(value.value, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", value.path, err)
		}
		files = append(files, BootstrapFile{Path: value.path, Text: string(raw), Mode: 0644})
	}
	return files, nil
}

func (s *AgentBootstrapService) buildVideoEditFiles(ctx context.Context, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if task == nil || !model.IsVideoEditPlatform(task.Type) {
		return nil, nil
	}
	input := task.VideoEditInput.Data()
	if err := input.Validate(s.cfg.VideoEdit.MaxAssets, s.cfg.VideoEdit.MaxDurationSeconds); err != nil {
		return nil, fmt.Errorf("%w: validate frozen video-edit input: %v", ErrAgentBootstrapConflict, err)
	}
	if s.repo == nil || s.cfg.Store == nil {
		return nil, fmt.Errorf("%w: video-edit source material storage is unavailable", ErrAgentBootstrapUnavailable)
	}
	maxAssetBytes := s.cfg.VideoEdit.MaxAssetBytes
	if maxAssetBytes <= 0 {
		maxAssetBytes = 256 << 20
	}
	maxTotalInputBytes := s.cfg.VideoEdit.MaxTotalInputBytes
	if maxTotalInputBytes <= 0 {
		maxTotalInputBytes = 1 << 30
	}
	files := make([]BootstrapFile, 0, len(input.SourceAssets)+2)
	totalInputBytes := int64(0)
	assets := make([]*model.Asset, len(input.SourceAssets))
	for i := range input.SourceAssets {
		asset := &input.SourceAssets[i]
		assetID := strings.TrimSpace(asset.AssetID)
		stored, err := s.repo.Assets().FindOwnedByID(ctx, assetID, task.UserID)
		if err != nil || stored == nil {
			return nil, fmt.Errorf("%w: video-edit source asset %q is unavailable", ErrAgentBootstrapConflict, assetID)
		}
		if err := validateVideoEditSourceAssetMaterializable(stored, task.UserID, s.cfg.Store.Name(), asset, maxAssetBytes); err != nil {
			return nil, fmt.Errorf("%w: video-edit source asset %q %v", ErrAgentBootstrapConflict, assetID, err)
		}
		if stored.Size > maxTotalInputBytes-totalInputBytes {
			return nil, fmt.Errorf("%w: video-edit source assets exceed %d bytes in total", ErrAgentBootstrapConflict, maxTotalInputBytes)
		}
		totalInputBytes += stored.Size
		assets[i] = stored
	}
	for i := range input.SourceAssets {
		asset := &input.SourceAssets[i]
		stored := assets[i]
		signed, err := s.signedBootstrapObjectKey(ctx, stored.StorageKey, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign video-edit source asset %q: %w", stored.ID, err)
		}
		name := serveragent.InputAttachmentFilename(i+1, model.EntryAttachment{FileName: stored.FileName, ContentType: stored.ContentType, Key: stored.StorageKey})
		rel := path.Join(".anban-creator/video-edit-assets", name)
		files = append(files, BootstrapFile{Path: rel, DownloadURL: signed, Mode: 0644, ExpectedSize: stored.Size, MaxBytes: maxAssetBytes})
		asset.AssetID = ""
		asset.WorkspacePath = rel
		asset.FileName = stored.FileName
		asset.MimeType = stored.ContentType
		asset.FileSize = stored.Size
	}
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal video-edit input: %w", err)
	}
	files = append(files, BootstrapFile{Path: "video-edit-input.json", Text: string(raw), Mode: 0644})
	policy := map[string]any{
		"quality_tier":  input.Preferences.QualityTier,
		"subtitle_mode": input.Preferences.SubtitleMode,
		"upstream":      "browser-use/video-use@43cfc566833548093455c9d4a46bd69913b7e552",
		"automatic":     true,
		"output_dir":    "output",
	}
	policyRaw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal video-edit policy: %w", err)
	}
	files = append(files, BootstrapFile{Path: "video-edit-policy.json", Text: string(policyRaw), Mode: 0644})
	return files, nil
}

func (s *AgentBootstrapService) buildTalkingHeadEditorFiles(ctx context.Context, execution *model.TaskExecution, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if task == nil || !model.IsTalkingHeadEditorPlatform(task.Type) {
		return nil, nil
	}
	input := task.TalkingHeadEditorInput.Data()
	if reviewAssetID := strings.TrimSpace(task.ReviewReferenceCutAssetID); reviewAssetID != "" {
		input.ReferenceCut = &model.TalkingHeadEditorAsset{Role: "reference", AssetID: reviewAssetID}
	}
	if err := input.Validate(s.cfg.TalkingHeadEditor.MaxAssets, s.cfg.TalkingHeadEditor.MaxDurationSeconds); err != nil {
		return nil, fmt.Errorf("%w: validate frozen talking-head-editor input: %v", ErrAgentBootstrapConflict, err)
	}
	if s.repo == nil || s.cfg.Store == nil {
		return nil, fmt.Errorf("%w: talking-head-editor source material storage is unavailable", ErrAgentBootstrapUnavailable)
	}
	files := make([]BootstrapFile, 0, len(input.SupportingAssets)+4)
	materialize := func(stored *model.Asset, rel string) (BootstrapFile, error) {
		if stored == nil {
			return BootstrapFile{}, fmt.Errorf("%w: talking-head-editor asset storage is invalid", ErrAgentBootstrapConflict)
		}
		signed, err := s.signedBootstrapObjectKey(ctx, stored.StorageKey, credentialDeadline)
		if err != nil {
			return BootstrapFile{}, err
		}
		return BootstrapFile{Path: rel, DownloadURL: signed, Mode: 0644, ExpectedSize: stored.Size, MaxBytes: 1 << 30}, nil
	}
	material := func(asset *model.TalkingHeadEditorAsset, rel string) error {
		if asset == nil || strings.TrimSpace(asset.AssetID) == "" {
			return fmt.Errorf("%w: talking-head-editor asset is missing", ErrAgentBootstrapConflict)
		}
		stored, err := s.repo.Assets().FindOwnedByID(ctx, asset.AssetID, task.UserID)
		if err != nil || stored == nil {
			return fmt.Errorf("%w: talking-head-editor asset %q is unavailable", ErrAgentBootstrapConflict, asset.AssetID)
		}
		file, err := materialize(stored, rel)
		if err != nil {
			return err
		}
		files = append(files, file)
		asset.AssetID, asset.URL, asset.FileName, asset.MimeType, asset.FileSize = "", "", stored.FileName, stored.ContentType, stored.Size
		return nil
	}
	if err := material(&input.PrimaryVideo, path.Join(".anban-creator/talking-head-editor-assets", "primary-"+safeAssetFilename(input.PrimaryVideo.FileName))); err != nil {
		return nil, err
	}
	for i := range input.SupportingAssets {
		if err := material(&input.SupportingAssets[i], path.Join(".anban-creator/talking-head-editor-assets", fmt.Sprintf("supporting-%02d-%s", i+1, safeAssetFilename(input.SupportingAssets[i].FileName)))); err != nil {
			return nil, err
		}
	}
	if input.ReferenceCut != nil {
		if err := material(input.ReferenceCut, path.Join(".anban-creator/talking-head-editor-assets", "reference-"+safeAssetFilename(input.ReferenceCut.FileName))); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(task.ReviewDecision) != "" && execution != nil && strings.TrimSpace(execution.ParentExecutionID) != "" {
		parentID := strings.TrimSpace(execution.ParentExecutionID)
		parentFiles, err := s.repo.TaskFiles().FindByExecutionID(ctx, parentID)
		if err != nil {
			return nil, fmt.Errorf("list talking-head-editor review artifacts: %w", err)
		}
		allowed := map[string]bool{
			"output/rough-cut.mp4":               true,
			"output/transcript.json":             true,
			"output/edit-decision-list.json":     true,
			"output/edit-strategy.md":            true,
			"output/semantic-cut.mp4":            true,
			"output/motion-sample.mp4":           true,
			"output/visual-plan.json":            true,
			"output/motion-review.json":          true,
			"output/motion-source-manifest.json": true,
		}
		prefix := buildTaskMCPArtifactStoragePrefix(task, parentID)
		for _, file := range parentFiles {
			if file == nil || file.TaskID != task.ID || file.ExecutionID != parentID || file.State != model.TaskFileStateDelivered || !allowed[filepath.ToSlash(file.FilePath)] {
				continue
			}
			if s.cfg.Store == nil || file.StorageProvider != s.cfg.Store.Name() || file.OSSKey != prefix+file.FilePath || file.FileSize <= 0 || file.FileSize > 1<<30 {
				return nil, fmt.Errorf("%w: invalid talking-head-editor review artifact %q", ErrAgentBootstrapConflict, file.FilePath)
			}
			signed, err := s.signedBootstrapObjectKey(ctx, file.OSSKey, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign talking-head-editor review artifact %q: %w", file.FilePath, err)
			}
			files = append(files, BootstrapFile{Path: filepath.ToSlash(file.FilePath), DownloadURL: signed, ContentSHA256: file.ContentHash, Mode: 0644, ExpectedSize: file.FileSize, MaxBytes: 1 << 30, ReplaceExisting: true})
		}
	}
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal talking-head-editor input: %w", err)
	}
	files = append(files, BootstrapFile{Path: "talking-head-editor-input.json", Text: string(raw), Mode: 0644})
	policy := map[string]any{
		"skills":            []string{"auto-editor", "video-use", "erduo-broll-loop-engineering", "talking-head-sound-design", "content-tagging"},
		"video_use_api_key": "server-injected",
		"review_gate":       "motion-sample",
		"review_decision":   task.ReviewDecision,
		"review_feedback":   task.ReviewFeedback,
		"review_revision":   task.ReviewRevision,
		"resume_from":       talkingHeadEditorResumeStage(task.ReviewDecision),
		"output_dir":        "output",
	}
	policyRaw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return nil, err
	}
	files = append(files, BootstrapFile{Path: "talking-head-editor-policy.json", Text: string(policyRaw), Mode: 0644})
	return files, nil
}

func talkingHeadEditorResumeStage(decision string) string {
	if strings.TrimSpace(decision) == "approve" {
		return "sound_and_review"
	}
	if strings.TrimSpace(decision) == "revise" {
		return "motion_packaging"
	}
	return "input_and_rough_cut"
}

func safeAssetFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "asset.bin"
	}
	name = regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(name, "-")
	if name == "" {
		return "asset.bin"
	}
	return name
}

func validateVideoEditSourceAssetMaterializable(asset *model.Asset, userID, expectedStorageProvider string, input *model.VideoEditAsset, maxBytes int64) error {
	if asset == nil || input == nil {
		return errors.New("is unavailable")
	}
	userID = strings.TrimSpace(userID)
	expectedStorageProvider = strings.TrimSpace(expectedStorageProvider)
	if userID == "" || asset.UserID != userID {
		return errors.New("is not owned by the task user")
	}
	if expectedStorageProvider != "oss" || (asset.Purpose != DirectUploadPurposeVideoEditAsset && asset.Purpose != DirectUploadPurposeAIEntryAttachment) {
		return errors.New("does not use the finalized video-edit upload storage")
	}
	if asset.ID == "" || asset.FileName == "" || asset.FileName == "." || asset.FileName == "/" || path.Base(asset.FileName) != asset.FileName ||
		asset.StorageKey != path.Join("assets/users", userID, asset.ID, asset.FileName) || path.Clean(asset.StorageKey) != asset.StorageKey {
		return errors.New("has no canonical finalized storage object")
	}
	if maxBytes <= 0 {
		maxBytes = 256 << 20
	}
	if asset.Size <= 0 || asset.Size > maxBytes || strings.TrimSpace(asset.ETag) == "" {
		return fmt.Errorf("must have valid integrity metadata and be between 1 and %d bytes", maxBytes)
	}
	if !montageSourceTaskFileTypeMatches(input.Type, asset.ContentType) {
		return fmt.Errorf("does not match asset type %q", input.Type)
	}
	if input.FileName != "" && input.FileName != asset.FileName ||
		input.MimeType != "" && normalizeDirectUploadContentType(input.MimeType) != normalizeDirectUploadContentType(asset.ContentType) ||
		input.FileSize > 0 && input.FileSize != asset.Size {
		return errors.New("metadata does not match the finalized asset")
	}
	return nil
}

func (s *AgentBootstrapService) buildLocalLifeFiles(ctx context.Context, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if task == nil || !model.IsLocalLifePlatform(task.Type) {
		return nil, nil
	}
	input := task.LocalLifeInput.Data()
	input.SourceAssets = append([]model.LocalLifeAsset(nil), input.SourceAssets...)
	files := make([]BootstrapFile, 0, len(input.SourceAssets)+1)
	totalBytes := int64(0)
	for i := range input.SourceAssets {
		asset := &input.SourceAssets[i]
		assetName := serveragent.InputAttachmentFilename(i+1, model.EntryAttachment{FileName: asset.FileName, ContentType: asset.MimeType})
		rel := path.Join(".anban-creator/local-life-assets", assetName)
		if fileID := strings.TrimSpace(asset.TaskFileID); fileID != "" {
			if s.repo == nil || s.cfg.Store == nil {
				return nil, fmt.Errorf("%w: local-life source material storage is unavailable", ErrAgentBootstrapUnavailable)
			}
			file, err := s.repo.TaskFiles().FindByID(ctx, fileID)
			if err != nil || file == nil {
				return nil, fmt.Errorf("%w: local-life source task file %q is unavailable", ErrAgentBootstrapConflict, fileID)
			}
			sourceTask, err := s.repo.Tasks().FindByID(ctx, file.TaskID)
			if err != nil || sourceTask == nil || sourceTask.UserID != task.UserID || !montageSourceTaskAuthorized(sourceTask, task.ProjectID, montageSourceTaskTrust{taskID: task.InputSourceTaskID, projectID: task.InputSourceProjectID, includeLineage: true}) {
				return nil, fmt.Errorf("%w: local-life source task file %q is not authorized", ErrAgentBootstrapConflict, fileID)
			}
			if !montageSourceTaskFileTypeMatches(asset.Type, file.MimeType) {
				return nil, fmt.Errorf("%w: local-life source task file %q does not match asset type %q", ErrAgentBootstrapConflict, fileID, asset.Type)
			}
			if err := validateMontageSourceTaskFileMaterializable(file, s.cfg.Store.Name()); err != nil {
				return nil, fmt.Errorf("%w: local-life source task file %q %v", ErrAgentBootstrapConflict, fileID, err)
			}
			if file.FileSize > montageSourceTotalMaxBytes-totalBytes {
				return nil, fmt.Errorf("%w: local-life source assets exceed the runtime aggregate size limit", ErrAgentBootstrapConflict)
			}
			totalBytes += file.FileSize
			signed, err := s.signedBootstrapObjectKey(ctx, file.OSSKey, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign local-life source task file %q: %w", fileID, err)
			}
			files = append(files, BootstrapFile{Path: rel, DownloadURL: signed, ContentSHA256: file.ContentHash, Mode: 0644, ExpectedSize: file.FileSize, MaxBytes: montageSourceMaxBytes})
			asset.URL, asset.TaskFileID, asset.FileName, asset.MimeType, asset.FileSize = rel, "", file.FileName, file.MimeType, file.FileSize
			continue
		}
		rawURL := strings.TrimSpace(asset.URL)
		if rawURL == "" {
			return nil, fmt.Errorf("%w: local-life source asset %d has no URL or task file", ErrAgentBootstrapConflict, i+1)
		}
		if s.cfg.Store == nil || !s.cfg.Store.IsOwnedURL(rawURL) {
			return nil, fmt.Errorf("%w: local-life source asset %d is not a Server-owned storage URL", ErrAgentBootstrapConflict, i+1)
		}
		key, ok := storage.StorageKeyFromURL(rawURL)
		if !ok || !strings.HasPrefix(strings.TrimPrefix(key, "/"), "assets/users/"+task.UserID+"/") {
			return nil, fmt.Errorf("%w: local-life source asset %d is outside the task user's storage namespace", ErrAgentBootstrapConflict, i+1)
		}
		if asset.FileSize > montageSourceTotalMaxBytes-totalBytes {
			return nil, fmt.Errorf("%w: local-life source assets exceed the runtime aggregate size limit", ErrAgentBootstrapConflict)
		}
		totalBytes += asset.FileSize
		signed, err := s.signedBootstrapObjectKey(ctx, strings.TrimPrefix(key, "/"), credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign local-life source asset %d: %w", i+1, err)
		}
		files = append(files, BootstrapFile{Path: rel, DownloadURL: signed, Mode: 0644, ExpectedSize: asset.FileSize, MaxBytes: montageSourceMaxBytes})
		asset.URL = rel
	}
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal local-life input: %w", err)
	}
	files = append(files, BootstrapFile{Path: "local-life-input.json", Text: string(raw), Mode: 0644})
	return files, nil
}

func (s *AgentBootstrapService) materializeMontageTaskFileSources(ctx context.Context, task *model.Task, input *model.MontageInput, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: montage input is required", ErrAgentBootstrapConflict)
	}
	files := make([]BootstrapFile, 0, len(input.SourceAssets))
	totalBytes := int64(0)
	for i := range input.SourceAssets {
		asset := &input.SourceAssets[i]
		assetID := strings.TrimSpace(asset.AssetID)
		if assetID != "" {
			if strings.TrimSpace(asset.TaskFileID) != "" || strings.TrimSpace(asset.URL) != "" {
				return nil, fmt.Errorf("%w: Montage source asset %q has multiple locators", ErrAgentBootstrapConflict, assetID)
			}
			if s.repo == nil || s.cfg.Store == nil {
				return nil, fmt.Errorf("%w: Montage source asset storage is unavailable", ErrAgentBootstrapUnavailable)
			}
			stored, err := s.repo.Assets().FindOwnedByID(ctx, assetID, task.UserID)
			if err != nil || stored == nil {
				return nil, fmt.Errorf("%w: Montage source asset %q is unavailable", ErrAgentBootstrapConflict, assetID)
			}
			if err := validateMontageStoredAssetMaterializable(stored, task.UserID, s.cfg.Store.Name(), *asset); err != nil {
				return nil, fmt.Errorf("%w: Montage source asset %q %v", ErrAgentBootstrapConflict, assetID, err)
			}
			if stored.Size > montageSourceTotalMaxBytes-totalBytes {
				return nil, fmt.Errorf("%w: Montage source assets exceed the runtime aggregate size limit", ErrAgentBootstrapConflict)
			}
			totalBytes += stored.Size
			signed, err := s.signedBootstrapObjectKey(ctx, stored.StorageKey, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign Montage source asset %q: %w", assetID, err)
			}
			name := serveragent.InputAttachmentFilename(i+1, model.EntryAttachment{FileName: stored.FileName, ContentType: stored.ContentType, Key: stored.StorageKey})
			rel := path.Join(".anban-creator/montage-assets", name)
			files = append(files, BootstrapFile{Path: rel, DownloadURL: signed, Mode: 0644, ExpectedSize: stored.Size, MaxBytes: montageSourceMaxBytes})
			asset.AssetID = ""
			asset.URL = rel
			asset.FileName = stored.FileName
			asset.MimeType = stored.ContentType
			asset.FileSize = stored.Size
			continue
		}
		fileID := strings.TrimSpace(asset.TaskFileID)
		if fileID == "" {
			continue
		}
		if s.repo == nil {
			return nil, fmt.Errorf("%w: task file repository is unavailable", ErrAgentBootstrapUnavailable)
		}
		file, err := s.repo.TaskFiles().FindByID(ctx, fileID)
		if err != nil || file == nil {
			return nil, fmt.Errorf("%w: Montage source task file %q is unavailable", ErrAgentBootstrapConflict, fileID)
		}
		sourceTask, err := s.repo.Tasks().FindByID(ctx, file.TaskID)
		if err != nil || sourceTask == nil || sourceTask.UserID != task.UserID {
			return nil, fmt.Errorf("%w: Montage source task file %q is not owned by the task user", ErrAgentBootstrapConflict, fileID)
		}
		if !montageSourceTaskAuthorized(sourceTask, task.ProjectID, montageSourceTaskTrust{
			taskID: task.InputSourceTaskID, projectID: task.InputSourceProjectID, includeLineage: true,
		}) {
			return nil, fmt.Errorf("%w: Montage source task file %q is not authorized for this project", ErrAgentBootstrapConflict, fileID)
		}
		if !montageSourceTaskFileTypeMatches(asset.Type, file.MimeType) {
			return nil, fmt.Errorf("%w: Montage source task file %q does not match asset type %q", ErrAgentBootstrapConflict, fileID, asset.Type)
		}
		if s.cfg.Store == nil {
			return nil, fmt.Errorf("%w: storage provider is required for Montage source downloads", ErrAgentBootstrapUnavailable)
		}
		expectedProvider := strings.TrimSpace(s.cfg.Store.Name())
		if err := validateMontageSourceTaskFileMaterializable(file, expectedProvider); err != nil {
			return nil, fmt.Errorf("%w: Montage source task file %q %v", ErrAgentBootstrapConflict, fileID, err)
		}
		if file.FileSize > montageSourceTotalMaxBytes-totalBytes {
			return nil, fmt.Errorf("%w: Montage task-file sources exceed the runtime aggregate size limit", ErrAgentBootstrapConflict)
		}
		totalBytes += file.FileSize
		objectKey := file.OSSKey
		contentHash := file.ContentHash
		signed, err := s.signedBootstrapObjectKey(ctx, objectKey, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign Montage source task file %q: %w", fileID, err)
		}
		name := serveragent.InputAttachmentFilename(i+1, model.EntryAttachment{
			FileName: file.FileName, ContentType: file.MimeType, Key: objectKey,
		})
		rel := path.Join(".anban-creator/montage-assets", name)
		files = append(files, BootstrapFile{
			Path: rel, DownloadURL: signed, ContentSHA256: contentHash, Mode: 0644,
			ExpectedSize: file.FileSize, MaxBytes: montageSourceMaxBytes,
		})
		asset.URL = rel
		asset.TaskFileID = ""
		asset.FileName = file.FileName
		asset.MimeType = file.MimeType
		asset.FileSize = file.FileSize
	}
	return files, nil
}

func (s *AgentBootstrapService) signedResumeAttachmentURL(ctx context.Context, task *model.Task, rawKey string, credentialDeadline time.Time) (string, error) {
	if task == nil || strings.TrimSpace(task.UserID) == "" || strings.TrimSpace(task.ProjectID) == "" || strings.TrimSpace(task.ID) == "" {
		return "", fmt.Errorf("%w: task identity is required for resume attachment", ErrAgentBootstrapConflict)
	}
	key := strings.TrimPrefix(strings.TrimSpace(rawKey), "/")
	prefix := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID, "resume") + "/"
	if key == "" || path.Clean(key) != key || !strings.HasPrefix(key, prefix) {
		return "", fmt.Errorf("%w: resume attachment is outside the current task namespace", ErrAgentBootstrapConflict)
	}
	return s.signedBootstrapObjectKey(ctx, key, credentialDeadline)
}

func (s *AgentBootstrapService) signedBootstrapObjectKey(ctx context.Context, key string, credentialDeadline time.Time) (string, error) {
	if s.cfg.Store == nil {
		return "", fmt.Errorf("%w: storage provider is required for bootstrap downloads", ErrAgentBootstrapUnavailable)
	}
	signingNow := s.currentTime()
	ttl := int(credentialDeadline.Unix()-signingNow.Unix()) - 1
	if s.cfg.SignedURLTTL > 0 && ttl > s.cfg.SignedURLTTL {
		ttl = s.cfg.SignedURLTTL
	}
	if ttl <= 0 {
		return "", fmt.Errorf("%w: no positive signed download lifetime remains", ErrAgentBootstrapConflict)
	}
	signed, err := s.cfg.Store.DownloadURL(ctx, key, ttl)
	if err != nil {
		return "", fmt.Errorf("%w: sign bootstrap download: %v", ErrAgentBootstrapUnavailable, err)
	}
	if strings.TrimSpace(signed) == "" {
		return "", fmt.Errorf("%w: storage returned an empty signed download URL", ErrAgentBootstrapUnavailable)
	}
	if s.currentTime().Unix()+int64(ttl) > credentialDeadline.Unix() {
		return "", fmt.Errorf("%w: signed download crossed the credential deadline", ErrAgentBootstrapConflict)
	}
	return signed, nil
}

func (s *AgentBootstrapService) buildAttachmentFiles(ctx context.Context, executionID string, task *model.Task, attachments []model.EntryAttachment, credentialDeadline time.Time) ([]BootstrapFile, error) {
	files := make([]BootstrapFile, 0, len(attachments)+2)
	type indexEntry struct {
		Index       int    `json:"index"`
		TypeIndex   int    `json:"type_index"`
		Type        string `json:"type,omitempty"`
		Role        string `json:"role,omitempty"`
		FileName    string `json:"file_name,omitempty"`
		ContentType string `json:"content_type,omitempty"`
		Size        int64  `json:"size,omitempty"`
		Path        string `json:"path"`
		Instruction string `json:"instruction,omitempty"`
	}
	index := make([]indexEntry, 0, len(attachments))
	typeIndexes := make(map[string]int)
	globalIndex := 0
	for i, attachment := range attachments {
		if attachment.Role == model.EntryAttachmentRoleResumeLatest {
			if strings.TrimSpace(attachment.Text) == "" {
				return nil, fmt.Errorf("%w: resume latest attachment requires text", ErrAgentBootstrapConflict)
			}
			resumePath, err := serveragent.ExecutionResumeContextPath(executionID)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
			}
			files = append(files, BootstrapFile{Path: resumePath, Text: attachment.Text, Mode: 0644})
			continue
		}
		assetID := strings.TrimSpace(attachment.AssetID)
		var asset *model.Asset
		if assetID != "" {
			if strings.TrimSpace(attachment.URL) != "" || strings.TrimSpace(attachment.Key) != "" || strings.TrimSpace(attachment.UploadID) != "" {
				return nil, fmt.Errorf("%w: attachment %q has ambiguous storage identity", ErrAgentBootstrapConflict, attachment.FileName)
			}
			var err error
			asset, err = NewReferenceAssetService(s.repo, nil, nil).RequireOwnedAttachment(ctx, task.UserID, assetID, []string{DirectUploadPurposeTaskReference, DirectUploadPurposeAIEntryAttachment, DirectUploadPurposeEcommercePhoto, DirectUploadPurposeMediaLibrary})
			if err != nil {
				return nil, fmt.Errorf("resolve attachment asset %q: %w", assetID, err)
			}
			attachment.Type = ClassifyDirectUploadFile(asset.ContentType, strings.ToLower(filepath.Ext(asset.FileName)))
			attachment.FileName = asset.FileName
			attachment.ContentType = asset.ContentType
			attachment.Size = asset.Size
		}
		name := serveragent.InputAttachmentFilename(i+1, attachment)
		dir := ".anban-creator/input-attachments"
		var rel string
		if attachment.Role == model.EntryAttachmentRoleResumeFile {
			canonicalName, err := serveragent.CanonicalResumeAttachmentFilename(attachment.FileName)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
			}
			name = canonicalName
			rel, err = serveragent.ExecutionResumeAttachmentPath(executionID, name)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
			}
		}
		if rel == "" {
			rel = path.Join(dir, name)
		}
		file := BootstrapFile{Path: rel, Mode: 0644}
		if asset != nil {
			signed, err := s.signedReferenceAssetURL(ctx, asset, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign attachment asset %q: %w", asset.ID, err)
			}
			file.DownloadURL = signed
			file.ExpectedSize = asset.Size
			policy := directUploadPolicies[asset.Purpose]
			file.MaxBytes = policy.maxSize
			if policy.maxSizeFor != nil {
				file.MaxBytes = policy.maxSizeFor(asset.ContentType, strings.ToLower(filepath.Ext(asset.FileName)))
			}
		} else if attachment.Role == model.EntryAttachmentRoleResumeFile {
			signed, err := s.signedResumeAttachmentURL(ctx, task, attachment.Key, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign attachment %q: %w", attachment.FileName, err)
			}
			file.DownloadURL = signed
			file.ExpectedSize = attachment.Size
			file.MaxBytes = maxTaskResumeFileBytes
		} else if strings.TrimSpace(attachment.Text) != "" {
			file.Text = attachment.Text
		} else {
			return nil, fmt.Errorf("%w: attachment %q has no immutable asset identity", ErrAgentBootstrapConflict, attachment.FileName)
		}
		files = append(files, file)
		if attachment.Role != model.EntryAttachmentRoleResumeFile {
			globalIndex++
			typeIndexes[attachment.Type]++
			index = append(index, indexEntry{Index: globalIndex, TypeIndex: typeIndexes[attachment.Type], Type: attachment.Type, Role: attachment.Role, FileName: attachment.FileName, ContentType: attachment.ContentType, Size: attachment.Size, Path: rel, Instruction: attachment.Instruction})
		}
	}
	if len(index) > 0 {
		raw, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return nil, err
		}
		files = append(files, BootstrapFile{Path: ".anban-creator/input-attachments/index.json", Text: string(raw), Mode: 0644})
	}
	return files, nil
}

func ValidateBootstrapFiles(files []BootstrapFile) error {
	return validateBootstrapFilesForTask(files, nil)
}
func validateBootstrapFilesForTask(files []BootstrapFile, task *model.Task) error {
	totalLimit := int64(agentBootstrapTotalMaxBytes)
	if task != nil && model.IsHypitPlatform(task.Type) {
		l := readHypitSnapshot(task).Limits
		totalLimit = l.MaxProjectBytes + l.MaxInputBytes + agentBootstrapTotalMaxBytes
	}
	seen := make(map[string]struct{}, len(files))
	totalBytes := int64(0)
	for i := range files {
		file := &files[i]
		raw := strings.TrimSpace(strings.ReplaceAll(file.Path, "\\", "/"))
		if raw == "" || strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) {
			return errors.New("bootstrap file path must be relative")
		}
		clean, err := CleanTaskFileRelativePath(raw)
		if err != nil || filepath.ToSlash(clean) != raw {
			return fmt.Errorf("invalid bootstrap file path %q", file.Path)
		}
		for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
			if err := serveragent.ValidatePortableFilenameComponent(component); err != nil {
				return fmt.Errorf("invalid bootstrap file path %q: %w", file.Path, err)
			}
		}
		if file.ReplaceExisting && filepath.ToSlash(clean) != ".anban-creator/settings.json" && filepath.ToSlash(clean) != ".anban-creator/feedback-strategy.json" {
			_, recoveryArtifact := findPublicationRecoveryArtifactSpecForTask(task, filepath.ToSlash(clean))
			if !recoveryArtifact || strings.TrimSpace(file.DownloadURL) == "" || !lowercaseSHA256.MatchString(file.ContentSHA256) {
				return fmt.Errorf("bootstrap file %q cannot replace existing workspace content", clean)
			}
		}
		portableKey := serveragent.PortableFilenameKey(filepath.ToSlash(clean))
		if _, exists := seen[portableKey]; exists {
			return fmt.Errorf("duplicate bootstrap file path %q", clean)
		}
		seen[portableKey] = struct{}{}
		if (file.Text == "") == (file.DownloadURL == "") {
			return fmt.Errorf("bootstrap file %q must have exactly one content source", clean)
		}
		if file.ContentSHA256 != "" && !lowercaseSHA256.MatchString(file.ContentSHA256) {
			return fmt.Errorf("bootstrap file %q has invalid content SHA-256", clean)
		}
		if file.ExpectedSize < 0 || file.MaxBytes < 0 {
			return fmt.Errorf("bootstrap file %q has an invalid byte limit", clean)
		}
		declaredBytes := int64(len([]byte(file.Text)))
		if file.DownloadURL != "" {
			declaredBytes = file.ExpectedSize
			if declaredBytes == 0 {
				declaredBytes = file.MaxBytes
			}
			if declaredBytes == 0 {
				declaredBytes = agentBootstrapFileMaxBytes
			}
			fileLimit := int64(agentBootstrapFileMaxBytes)
			if task != nil && model.IsHypitPlatform(task.Type) {
				l := readHypitSnapshot(task).Limits
				if clean == ".anban-creator/project.zip" {
					fileLimit = l.MaxProjectBytes
				} else if strings.HasPrefix(clean, "project/assets/") {
					fileLimit = l.MaxAssetBytes
				}
			}
			if declaredBytes > fileLimit || (file.MaxBytes > 0 && file.ExpectedSize > file.MaxBytes) {
				return fmt.Errorf("bootstrap file %q exceeds the runtime file size limit", clean)
			}
		}
		if declaredBytes > totalLimit-totalBytes {
			return errors.New("bootstrap files exceed the runtime total size limit")
		}
		totalBytes += declaredBytes
		if file.Mode == 0 || file.Mode&07000 != 0 || file.Mode&0002 != 0 || file.Mode&0111 != 0 {
			return fmt.Errorf("unsafe bootstrap file mode %#o", file.Mode)
		}
		file.Path = filepath.ToSlash(clean)
	}
	return nil
}
