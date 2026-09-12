package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
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
	ExecutionToken      string              `json:"execution_token"`
	ExecutionID         string              `json:"execution_id"`
	TaskID              string              `json:"task_id"`
	TaskType            string              `json:"task_type"`
	AgentPackID         string              `json:"agent_pack_id"`
	AgentPackVersion    string              `json:"agent_pack_version"`
	AgentPackDigest     string              `json:"agent_pack_digest"`
	RuntimeAdapter      string              `json:"runtime_adapter"`
	RuntimeProfile      string              `json:"runtime_profile"`
	ProjectID           string              `json:"project_id"`
	Prompt              string              `json:"prompt"`
	ExecutionProfile    AgentRuntimeProfile `json:"execution_profile"`
	MaxTurns            int                 `json:"max_turns"`
	AgentFlag           string              `json:"agent_flag"`
	AutoMemoryDirectory string              `json:"auto_memory_directory"`
	ResumeSessionID     string              `json:"resume_session_id,omitempty"`
	ResumeContextPath   string              `json:"resume_context_path,omitempty"`
	Env                 map[string]string   `json:"env,omitempty"`
	Files               []BootstrapFile     `json:"files"`
	ArtifactTransport   ArtifactTransport   `json:"artifact_transport"`
}

type AgentBootstrapConfig struct {
	MaxTurns                map[string]int
	TokenTTL                time.Duration
	ActiveDeadline          time.Duration
	SignedURLTTL            int
	Store                   storage.Provider
	ImageAPIConfig          *srvconfig.ImageAPIConfig
	MontageToolPolicy       map[string]srvconfig.MontageToolCapabilityPolicy
	MontagePipelineDefaults map[string]map[string]any
	MontageEnv              map[string]string
	Registry                *AgentProfileRegistry
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
	issuedAt := s.currentTime()
	credentialDeadline := issuedAt.Add(s.cfg.TokenTTL)
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
	taskReferenceAsset, projectStyleReferenceAsset, err := resolveRuntimeReferenceAssets(ctx, s.repo, task)
	if err != nil {
		return nil, fmt.Errorf("resolve reference asset: %w", err)
	}
	var files []BootstrapFile
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
	if instructions := strings.TrimSpace(effective.Instructions); instructions != "" {
		files = append(files, BootstrapFile{Path: "CLAUDE.md", Text: "# CLAUDE.md\n\n## 项目定位\n\n" + instructions, Mode: 0644})
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
	attachmentFiles, err := s.buildAttachmentFiles(ctx, execution.ID, task, attachments, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, attachmentFiles...)
	montageFiles, err := s.buildMontageFiles(task)
	if err != nil {
		return nil, err
	}
	files = append(files, montageFiles...)
	if err := ValidateBootstrapFiles(files); err != nil {
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
	prompt := serveragent.BuildUserPrompt(serveragent.UserPromptParams{
		TaskType: task.Type, Topic: task.Prompt, TaskID: task.ID, ProjectID: task.ProjectID,
		ImageRatio: task.ImageRatio, HasReferenceImage: taskReferenceAsset != nil,
		HasContentImage: task.HasContentImage, HasTailImage: task.HasTailImage,
		ArticleWithCover: task.ArticleWithCover, ArticleWithContentImages: task.ArticleWithContentImages,
	})
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
	profile, err := s.resolveExecutionProfile(execution, task)
	if err != nil {
		return nil, err
	}
	runtimeEnv := profile.RuntimeEnv()
	if err := model.ValidateClaudeProfileEnvs(runtimeEnv, true); err != nil {
		return nil, fmt.Errorf("%w: invalid Claude profile environment: %w", ErrAgentBootstrapUnavailable, err)
	}
	aliases := make(map[string]serveragent.ModelUsageIdentity, len(profile.ModelUsageAliases))
	for raw, target := range profile.ModelUsageAliases {
		aliases[raw] = serveragent.ModelUsageIdentity{Provider: profile.Provider, Model: target}
	}
	if err := serveragent.ValidateModelUsageAliases(aliases); err != nil {
		return nil, fmt.Errorf("%w: invalid Claude model usage aliases: %w", ErrAgentBootstrapUnavailable, err)
	}
	return &AgentBootstrapResponse{
		ExecutionToken: token, ExecutionID: execution.ID, TaskID: task.ID, TaskType: task.Type,
		AgentPackID: execution.AgentPackID, AgentPackVersion: execution.AgentPackVersion,
		AgentPackDigest: execution.AgentPackDigest, RuntimeAdapter: execution.RuntimeAdapter, RuntimeProfile: execution.RuntimeProfile,
		ProjectID: task.ProjectID, Prompt: prompt,
		ExecutionProfile: AgentRuntimeProfile{
			ProfileID: profile.ID, Provider: profile.Provider, Protocol: profile.Protocol,
			DisplayName: profile.DisplayName, ProfileFingerprint: task.AgentProfileFingerprint,
			Envs: model.CloneClaudeProfileEnvs(runtimeEnv), ModelUsageAliases: aliases,
		},
		MaxTurns: serveragent.DefaultMaxTurns(task.Type, s.cfg.MaxTurns), AgentFlag: "anban:" + serveragent.TaskToAgent(task),
		AutoMemoryDirectory: ".claude/memory", ResumeSessionID: execution.ResumeSessionID, ResumeContextPath: resumeContextPath,
		Env: s.montageEnv(task), Files: files, ArtifactTransport: ArtifactTransport{Mode: s.artifactTransportMode()},
	}, nil
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

func (s *AgentBootstrapService) buildMontageFiles(task *model.Task) ([]BootstrapFile, error) {
	if task == nil || !model.IsMontagePlatform(task.Type) {
		return nil, nil
	}
	values := []struct {
		path  string
		value any
	}{
		{"montage-input.json", task.MontageInput.Data()},
		{"montage-tool-policy.json", s.cfg.MontageToolPolicy},
		{"montage-pipeline-defaults.json", s.cfg.MontagePipelineDefaults},
	}
	files := make([]BootstrapFile, 0, len(values))
	for _, value := range values {
		raw, err := json.MarshalIndent(value.value, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", value.path, err)
		}
		files = append(files, BootstrapFile{Path: value.path, Text: string(raw), Mode: 0644})
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
			asset, err = NewReferenceAssetService(s.repo, nil, nil).RequireOwnedAttachment(ctx, task.UserID, assetID, []string{DirectUploadPurposeTaskReference, DirectUploadPurposeAIEntryAttachment, DirectUploadPurposeEcommercePhoto})
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
	seen := make(map[string]struct{}, len(files))
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
		if file.ReplaceExisting && filepath.ToSlash(clean) != ".anban-creator/settings.json" {
			return fmt.Errorf("bootstrap file %q cannot replace existing workspace content", clean)
		}
		portableKey := serveragent.PortableFilenameKey(filepath.ToSlash(clean))
		if _, exists := seen[portableKey]; exists {
			return fmt.Errorf("duplicate bootstrap file path %q", clean)
		}
		seen[portableKey] = struct{}{}
		if (file.Text == "") == (file.DownloadURL == "") {
			return fmt.Errorf("bootstrap file %q must have exactly one content source", clean)
		}
		if file.Mode == 0 || file.Mode&07000 != 0 || file.Mode&0002 != 0 || file.Mode&0111 != 0 {
			return fmt.Errorf("unsafe bootstrap file mode %#o", file.Mode)
		}
		file.Path = filepath.ToSlash(clean)
	}
	return nil
}
