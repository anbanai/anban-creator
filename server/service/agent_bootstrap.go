package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
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
	Path         string `json:"path"`
	Text         string `json:"text,omitempty"`
	DownloadURL  string `json:"download_url,omitempty"`
	Mode         uint32 `json:"mode"`
	ExpectedSize int64  `json:"expected_size,omitempty"`
	MaxBytes     int64  `json:"max_bytes,omitempty"`
}

type AgentBootstrapResponse struct {
	ExecutionToken      string                                    `json:"execution_token"`
	TaskID              string                                    `json:"task_id"`
	TaskType            string                                    `json:"task_type"`
	ProjectID           string                                    `json:"project_id"`
	Prompt              string                                    `json:"prompt"`
	Model               string                                    `json:"model"`
	MaxTurns            int                                       `json:"max_turns"`
	AgentFlag           string                                    `json:"agent_flag"`
	AutoMemoryDirectory string                                    `json:"auto_memory_directory"`
	ResumeSessionID     string                                    `json:"resume_session_id,omitempty"`
	ResumeContextPath   string                                    `json:"resume_context_path,omitempty"`
	RuntimeEnv          map[string]string                         `json:"runtime_env,omitempty"`
	ModelUsageAliases   map[string]serveragent.ModelUsageIdentity `json:"model_usage_aliases"`
	Env                 map[string]string                         `json:"env,omitempty"`
	Files               []BootstrapFile                           `json:"files"`
}

type AgentBootstrapConfig struct {
	Model                   string
	MaxTurns                map[string]int
	TokenTTL                time.Duration
	ActiveDeadline          time.Duration
	SignedURLTTL            int
	Store                   storage.Provider
	ImageAPIConfig          *srvconfig.ImageAPIConfig
	MontageToolPolicy       map[string]srvconfig.MontageToolCapabilityPolicy
	MontagePipelineDefaults map[string]map[string]any
	MontageEnv              map[string]string
	RuntimeEnv              map[string]string
	ModelUsageAliases       map[string]serveragent.ModelUsageIdentity
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

var unsafeBootstrapFilename = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

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
	execution, task, project, err := s.loadAndValidate(ctx, s.repo, identity)
	if err != nil {
		return nil, err
	}
	response, err := s.buildResponse(ctx, execution, task, project, identity.Deadline)
	if err != nil {
		return nil, err
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
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
				return fmt.Errorf("%w: execution bootstrap state changed concurrently", ErrAgentBootstrapConflict)
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
	if execution.RuntimeScope != identity.Scope || execution.RuntimeWorkload != identity.Workload {
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
	referenceAsset, err := resolveEffectiveReferenceAsset(ctx, s.repo, task)
	if err != nil {
		return nil, fmt.Errorf("resolve reference asset: %w", err)
	}
	files := []BootstrapFile{{Path: ".task-context", Text: "TASK_ID=" + task.ID + "\n", Mode: 0644}}
	appCfg, err := serveragent.BuildAppConfig(effective, resolver.ResolveStyle(effective, task), s.cfg.ImageAPIConfig, task.ImageRatio, referenceAsset != nil)
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
	files = append(files, BootstrapFile{Path: ".anban-creator/settings.json", Text: string(settings), Mode: 0600})
	if instructions := strings.TrimSpace(effective.Instructions); instructions != "" {
		files = append(files, BootstrapFile{Path: "CLAUDE.md", Text: "# CLAUDE.md\n\n## 项目定位\n\n" + instructions, Mode: 0644})
	}
	attachments := task.InputAttachments.Data()
	if referenceAsset != nil {
		signed, err := s.signedReferenceAssetURL(ctx, referenceAsset, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign reference image: %w", err)
		}
		files = append(files, BootstrapFile{
			Path: serveragent.ReferenceImagePath, DownloadURL: signed, Mode: 0644,
			ExpectedSize: referenceAsset.Size, MaxBytes: 10 << 20,
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
	productFiles, err := s.buildProductFiles(ctx, task, credentialDeadline)
	if err != nil {
		return nil, err
	}
	files = append(files, productFiles...)
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
	prompt := serveragent.BuildUserPrompt(serveragent.UserPromptParams{TaskType: task.Type, Topic: task.Prompt, Goal: task.Goal, TaskID: task.ID, ProjectID: task.ProjectID, HasContentImage: task.HasContentImage, HasTailImage: task.HasTailImage, ArticleWithCover: task.ArticleWithCover, ArticleWithContentImages: task.ArticleWithContentImages})
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
	runtimeEnv := serveragent.ClaudeRuntimeEnv(s.cfg.RuntimeEnv)
	if err := serveragent.ValidateClaudeRuntimeEnv(runtimeEnv); err != nil {
		return nil, fmt.Errorf("build Claude runtime environment: %w", err)
	}
	aliases := cloneBootstrapModelUsageAliases(s.cfg.ModelUsageAliases)
	if len(aliases) == 0 {
		return nil, fmt.Errorf("%w: Claude model usage aliases are required", ErrAgentBootstrapUnavailable)
	}
	if err := serveragent.ValidateModelUsageAliases(aliases); err != nil {
		return nil, fmt.Errorf("%w: invalid Claude model usage aliases: %w", ErrAgentBootstrapUnavailable, err)
	}
	return &AgentBootstrapResponse{ExecutionToken: token, TaskID: task.ID, TaskType: task.Type, ProjectID: task.ProjectID, Prompt: prompt, Model: s.cfg.Model, MaxTurns: serveragent.DefaultMaxTurns(task.Type, s.cfg.MaxTurns), AgentFlag: "anban:" + serveragent.TaskToAgent(task), AutoMemoryDirectory: ".claude/memory", ResumeSessionID: execution.ResumeSessionID, ResumeContextPath: resumeContextPath, RuntimeEnv: runtimeEnv, ModelUsageAliases: aliases, Env: s.montageEnv(task), Files: files}, nil
}

func cloneBootstrapModelUsageAliases(source map[string]serveragent.ModelUsageIdentity) map[string]serveragent.ModelUsageIdentity {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]serveragent.ModelUsageIdentity, len(source))
	for raw, identity := range source {
		result[raw] = identity
	}
	return result
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

func (s *AgentBootstrapService) buildProductFiles(ctx context.Context, task *model.Task, credentialDeadline time.Time) ([]BootstrapFile, error) {
	if task == nil || task.Type != model.PlatformEcommerce {
		return nil, nil
	}
	photos := task.Ecommerce.Data().ProductPhotos
	if len(photos) == 0 {
		return nil, nil
	}
	files := make([]BootstrapFile, 0, len(photos)+1)
	names := make([]string, 0, len(photos))
	attachments := task.InputAttachments.Data()
	for i, photo := range photos {
		purposes := []string{DirectUploadPurposeEcommercePhoto, DirectUploadPurposeAIEntryAttachment}
		source := bootstrapSourceForStoredValue(photo, attachments, purposes)
		signed, err := s.signedDownloadURL(ctx, task, source, credentialDeadline)
		if err != nil {
			return nil, fmt.Errorf("sign product photo %d: %w", i+1, err)
		}
		ext := ".png"
		if parsed, err := url.Parse(photo); err == nil {
			candidate := strings.ToLower(path.Ext(parsed.Path))
			switch candidate {
			case ".png", ".jpg", ".jpeg", ".webp":
				ext = candidate
			}
		}
		name := fmt.Sprintf("product_%02d%s", i+1, ext)
		names = append(names, name)
		files = append(files, BootstrapFile{Path: path.Join(".anban-creator/products", name), DownloadURL: signed, Mode: 0644})
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return nil, err
	}
	files = append(files, BootstrapFile{Path: ".anban-creator/products/index.json", Text: string(raw), Mode: 0644})
	return files, nil
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

type bootstrapDownloadSource struct {
	URL             string
	AssertedKey     string
	UploadID        string
	AllowedPurposes []string
}

func bootstrapSourceForStoredValue(value string, attachments []model.EntryAttachment, allowedPurposes []string) bootstrapDownloadSource {
	value = strings.TrimSpace(value)
	for _, attachment := range attachments {
		key := strings.TrimSpace(attachment.Key)
		uploadID := strings.TrimSpace(attachment.UploadID)
		if key == "" || uploadID == "" {
			continue
		}
		if key == value {
			return bootstrapDownloadSource{AssertedKey: key, UploadID: uploadID, AllowedPurposes: allowedPurposes}
		}
		if strings.TrimSpace(attachment.URL) == value {
			return bootstrapDownloadSource{URL: value, AssertedKey: key, UploadID: uploadID, AllowedPurposes: allowedPurposes}
		}
	}
	return bootstrapDownloadSource{URL: value, AllowedPurposes: allowedPurposes}
}

func (s *AgentBootstrapService) signedDownloadURL(ctx context.Context, task *model.Task, source bootstrapDownloadSource, credentialDeadline time.Time) (string, error) {
	if s.cfg.Store == nil {
		return "", fmt.Errorf("%w: storage provider is required for bootstrap downloads", ErrAgentBootstrapUnavailable)
	}
	if task == nil || strings.TrimSpace(task.UserID) == "" || strings.TrimSpace(task.ProjectID) == "" || strings.TrimSpace(task.ID) == "" {
		return "", fmt.Errorf("%w: task identity is required for bootstrap download", ErrAgentBootstrapConflict)
	}
	rawURL := strings.TrimSpace(source.URL)
	assertedKey := strings.TrimPrefix(strings.TrimSpace(source.AssertedKey), "/")
	var key string
	if rawURL != "" {
		if !s.cfg.Store.IsOwnedURL(rawURL) {
			return "", fmt.Errorf("%w: bootstrap download is not owned by configured storage", ErrAgentBootstrapConflict)
		}
		resolvedKey, ok := storage.StorageKeyFromURL(rawURL)
		if !ok || resolvedKey == "" {
			return "", fmt.Errorf("%w: cannot resolve bootstrap storage object key", ErrAgentBootstrapConflict)
		}
		key = strings.TrimPrefix(resolvedKey, "/")
	} else {
		if assertedKey == "" || strings.TrimSpace(source.UploadID) == "" {
			return "", fmt.Errorf("%w: key-only bootstrap download requires upload identity", ErrAgentBootstrapConflict)
		}
		key = assertedKey
	}
	if clean := path.Clean(key); clean != key || clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: bootstrap storage object key is invalid", ErrAgentBootstrapConflict)
	}
	if assertedKey != "" && assertedKey != key {
		return "", fmt.Errorf("%w: bootstrap storage object key assertion mismatch", ErrAgentBootstrapConflict)
	}
	if err := s.authorizeBootstrapObject(ctx, task, rawURL, key, source); err != nil {
		if errors.Is(err, ErrAgentBootstrapUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrAgentBootstrapConflict, err)
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

func (s *AgentBootstrapService) authorizeBootstrapObject(ctx context.Context, task *model.Task, rawURL, key string, source bootstrapDownloadSource) error {
	if rawURL != "" {
		taskPrefix := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID) + "/"
		legacyTaskPrefix := path.Join(task.UserID, task.ID) + "/"
		if strings.HasPrefix(key, taskPrefix) || strings.HasPrefix(key, legacyTaskPrefix) {
			return nil
		}
		if inputSourceTaskID := strings.TrimSpace(task.InputSourceTaskID); inputSourceTaskID != "" {
			sourcePrefix := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", inputSourceTaskID) + "/"
			if strings.HasPrefix(key, sourcePrefix) {
				return nil
			}
		}
		if storage.IsUserOwnedKey(task.UserID, key) {
			return nil
		}
	}
	finalizedPrefix := path.Join("assets/users", task.UserID) + "/"
	if !strings.HasPrefix(key, finalizedPrefix) || s.repo == nil {
		return errors.New("bootstrap storage object is outside task ownership")
	}
	id := strings.TrimSpace(source.UploadID)
	urlID := uploadSessionIDFromKey(key)
	if id == "" {
		id = urlID
	}
	if id == "" || (urlID != "" && urlID != id) {
		return errors.New("bootstrap finalized asset identity mismatch")
	}
	session, err := s.repo.UploadSessions().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: finalized upload session ownership lookup: %v", ErrAgentBootstrapUnavailable, err)
	}
	if session.UserID != task.UserID || session.Status != model.UploadSessionFinalized || session.AssetID != id || !directUploadPurposeAllowed(session.Purpose, source.AllowedPurposes) {
		return errors.New("bootstrap finalized asset ownership mismatch")
	}
	asset, err := s.repo.Assets().FindByID(ctx, session.AssetID)
	if err != nil {
		return fmt.Errorf("%w: finalized asset ownership lookup: %v", ErrAgentBootstrapUnavailable, err)
	}
	if asset.UserID != task.UserID || asset.StorageKey != key {
		return errors.New("bootstrap finalized asset ownership mismatch")
	}
	return nil
}

func (s *AgentBootstrapService) buildAttachmentFiles(ctx context.Context, executionID string, task *model.Task, attachments []model.EntryAttachment, credentialDeadline time.Time) ([]BootstrapFile, error) {
	files := make([]BootstrapFile, 0, len(attachments)+2)
	type indexEntry struct {
		Index       int    `json:"index"`
		Type        string `json:"type,omitempty"`
		FileName    string `json:"file_name,omitempty"`
		ContentType string `json:"content_type,omitempty"`
		Size        int64  `json:"size,omitempty"`
		Path        string `json:"path"`
	}
	index := make([]indexEntry, 0, len(attachments))
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
		name := bootstrapAttachmentName(i+1, attachment.FileName)
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
		if strings.TrimSpace(attachment.Key) != "" || strings.TrimSpace(attachment.URL) != "" {
			purposes := []string{DirectUploadPurposeAIEntryAttachment}
			if attachment.Role == model.EntryAttachmentRoleResumeFile {
				purposes = nil
			}
			signed, err := s.signedDownloadURL(ctx, task, bootstrapDownloadSource{URL: attachment.URL, AssertedKey: attachment.Key, UploadID: attachment.UploadID, AllowedPurposes: purposes}, credentialDeadline)
			if err != nil {
				return nil, fmt.Errorf("sign attachment %q: %w", attachment.FileName, err)
			}
			file.DownloadURL = signed
		} else if strings.TrimSpace(attachment.Text) != "" {
			file.Text = attachment.Text
		} else {
			return nil, fmt.Errorf("%w: attachment %q has no content source", ErrAgentBootstrapConflict, attachment.FileName)
		}
		files = append(files, file)
		if attachment.Role != model.EntryAttachmentRoleResumeFile {
			index = append(index, indexEntry{Index: i + 1, Type: attachment.Type, FileName: attachment.FileName, ContentType: attachment.ContentType, Size: attachment.Size, Path: rel})
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

func bootstrapAttachmentName(index int, raw string) string {
	name := unsafeBootstrapFilename.ReplaceAllString(filepath.Base(strings.TrimSpace(raw)), "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		name = "attachment"
	}
	return fmt.Sprintf("%02d-%s", index, name)
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
