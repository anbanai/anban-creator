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
	Path        string `json:"path"`
	Text        string `json:"text,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Mode        uint32 `json:"mode"`
}

type AgentBootstrapResponse struct {
	ExecutionToken      string          `json:"execution_token"`
	TaskID              string          `json:"task_id"`
	TaskType            string          `json:"task_type"`
	ProjectID           string          `json:"project_id"`
	Prompt              string          `json:"prompt"`
	Model               string          `json:"model"`
	MaxTurns            int             `json:"max_turns"`
	AgentFlag           string          `json:"agent_flag"`
	AutoMemoryDirectory string          `json:"auto_memory_directory"`
	Files               []BootstrapFile `json:"files"`
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
}

type AgentBootstrapService struct {
	repo   repository.Repository
	tokens *auth.ExecutionTokenService
	cfg    AgentBootstrapConfig
	logger zerolog.Logger
}

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
	return &AgentBootstrapService{repo: repo, tokens: tokens, cfg: cfg, logger: logger}
}

func (s *AgentBootstrapService) Bootstrap(ctx context.Context, identity *serveragent.KubernetesWorkloadIdentity) (*AgentBootstrapResponse, error) {
	if s == nil || s.repo == nil || s.tokens == nil || identity == nil {
		return nil, errors.New("agent bootstrap is not configured")
	}
	execution, task, project, err := s.loadAndValidate(ctx, s.repo, identity)
	if err != nil {
		return nil, err
	}
	response, err := s.buildResponse(ctx, execution, task, project, identity.JobDeadline)
	if err != nil {
		return nil, err
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		execution, _, _, err := s.loadAndValidate(ctx, tx, identity)
		if err != nil {
			return err
		}
		if execution.Status == model.TaskExecutionStarting {
			won, err := tx.TaskExecutions().Transition(ctx, execution.ID, []string{model.TaskExecutionStarting}, model.TaskExecutionRunning, model.ExecutionTransition{Started: true, PodUID: identity.PodUID})
			if err != nil {
				return err
			}
			if !won {
				return errors.New("execution bootstrap state changed concurrently")
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

func (s *AgentBootstrapService) loadAndValidate(ctx context.Context, repo repository.Repository, identity *serveragent.KubernetesWorkloadIdentity) (*model.TaskExecution, *model.Task, *model.Project, error) {
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
		return nil, nil, nil, errors.New("workload identity does not match current task execution ownership")
	}
	if execution.Target != "kubernetes" || execution.Namespace != identity.Namespace || execution.JobName != identity.JobName {
		return nil, nil, nil, errors.New("workload Kubernetes runtime identity mismatch")
	}
	if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		return nil, nil, nil, errors.New("task is terminal")
	}
	switch execution.Status {
	case model.TaskExecutionStarting:
		if execution.PodUID != "" && execution.PodUID != identity.PodUID {
			return nil, nil, nil, errors.New("execution is bound to another Pod")
		}
	case model.TaskExecutionRunning:
		if !execution.Started || execution.PodUID == "" || execution.PodUID != identity.PodUID {
			return nil, nil, nil, errors.New("running execution is bound to another Pod")
		}
	default:
		return nil, nil, nil, fmt.Errorf("execution status %q cannot bootstrap", execution.Status)
	}
	return execution, task, project, nil
}

func (s *AgentBootstrapService) buildResponse(ctx context.Context, execution *model.TaskExecution, task *model.Task, project *model.Project, jobDeadline time.Time) (*AgentBootstrapResponse, error) {
	if !jobDeadline.After(time.Now()) {
		return nil, errors.New("Kubernetes Job active deadline is missing or expired")
	}
	effective := serveragent.EffectiveProject(project, task)
	files := []BootstrapFile{{Path: ".task-context", Text: "TASK_ID=" + task.ID + "\n", Mode: 0644}}
	appCfg, err := serveragent.BuildAppConfig(effective, resolver.ResolveStyle(effective, task), s.cfg.ImageAPIConfig, task.ImageRatio, task.SkipReferenceImage, task.ReferenceImageURL)
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
	reference := strings.TrimSpace(task.ReferenceImageURL)
	if reference == "" && !task.SkipReferenceImage {
		reference = strings.TrimSpace(effective.ReferenceImageURL)
	}
	if reference != "" {
		signed, err := s.signedDownloadURL(ctx, reference, "")
		if err != nil {
			return nil, fmt.Errorf("sign reference image: %w", err)
		}
		files = append(files, BootstrapFile{Path: ".anban-creator/reference.png", DownloadURL: signed, Mode: 0644})
	}
	attachmentFiles, err := s.buildAttachmentFiles(ctx, task.InputAttachments.Data())
	if err != nil {
		return nil, err
	}
	files = append(files, attachmentFiles...)
	montageFiles, err := s.buildMontageFiles(task)
	if err != nil {
		return nil, err
	}
	files = append(files, montageFiles...)
	productFiles, err := s.buildProductFiles(ctx, task)
	if err != nil {
		return nil, err
	}
	files = append(files, productFiles...)
	if err := ValidateBootstrapFiles(files); err != nil {
		return nil, err
	}
	expires := time.Now().Add(s.cfg.TokenTTL)
	if jobDeadline.Before(expires) {
		expires = jobDeadline
	}
	token, err := s.tokens.Issue(auth.ExecutionClaims{UserID: task.UserID, ProjectID: task.ProjectID, TaskID: task.ID, ExecutionID: execution.ID}, expires)
	if err != nil {
		return nil, err
	}
	prompt := serveragent.BuildUserPrompt(serveragent.UserPromptParams{TaskType: task.Type, Topic: task.Prompt, Goal: task.Goal, TaskID: task.ID, ProjectID: task.ProjectID, HasContentImage: task.HasContentImage, HasTailImage: task.HasTailImage, ArticleWithCover: task.ArticleWithCover, ArticleWithContentImages: task.ArticleWithContentImages})
	return &AgentBootstrapResponse{ExecutionToken: token, TaskID: task.ID, TaskType: task.Type, ProjectID: task.ProjectID, Prompt: prompt, Model: s.cfg.Model, MaxTurns: serveragent.DefaultMaxTurns(task.Type, s.cfg.MaxTurns), AgentFlag: "anban:" + serveragent.TaskToAgent(task), AutoMemoryDirectory: ".claude/memory", Files: files}, nil
}

func (s *AgentBootstrapService) buildProductFiles(ctx context.Context, task *model.Task) ([]BootstrapFile, error) {
	if task == nil || task.Type != model.PlatformEcommerce {
		return nil, nil
	}
	photos := task.Ecommerce.Data().ProductPhotos
	if len(photos) == 0 {
		return nil, nil
	}
	files := make([]BootstrapFile, 0, len(photos)+1)
	names := make([]string, 0, len(photos))
	for i, photo := range photos {
		signed, err := s.signedDownloadURL(ctx, photo, "")
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

func (s *AgentBootstrapService) signedDownloadURL(ctx context.Context, rawURL, objectKey string) (string, error) {
	if s.cfg.Store == nil {
		return "", errors.New("storage provider is required for bootstrap downloads")
	}
	key := strings.TrimSpace(objectKey)
	if key == "" {
		rawURL = strings.TrimSpace(rawURL)
		if !s.cfg.Store.IsOwnedURL(rawURL) {
			return "", errors.New("bootstrap download is not owned by configured storage")
		}
		var ok bool
		key, ok = storage.StorageKeyFromURL(rawURL)
		if !ok || key == "" {
			return "", errors.New("cannot resolve bootstrap storage object key")
		}
	}
	signed, err := s.cfg.Store.DownloadURL(ctx, strings.TrimPrefix(key, "/"), s.cfg.SignedURLTTL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(signed) == "" {
		return "", errors.New("storage returned an empty signed download URL")
	}
	return signed, nil
}

func (s *AgentBootstrapService) buildAttachmentFiles(ctx context.Context, attachments []model.EntryAttachment) ([]BootstrapFile, error) {
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
				return nil, errors.New("resume latest attachment requires text")
			}
			files = append(files, BootstrapFile{Path: ".anban-creator/resume/latest.md", Text: attachment.Text, Mode: 0644})
			continue
		}
		name := bootstrapAttachmentName(i+1, attachment.FileName)
		dir := ".anban-creator/input-attachments"
		if attachment.Role == model.EntryAttachmentRoleResumeFile {
			dir = ".anban-creator/resume/attachments"
		}
		rel := path.Join(dir, name)
		file := BootstrapFile{Path: rel, Mode: 0644}
		if strings.TrimSpace(attachment.Key) != "" || strings.TrimSpace(attachment.URL) != "" {
			signed, err := s.signedDownloadURL(ctx, attachment.URL, attachment.Key)
			if err != nil {
				return nil, fmt.Errorf("sign attachment %q: %w", attachment.FileName, err)
			}
			file.DownloadURL = signed
		} else if strings.TrimSpace(attachment.Text) != "" {
			file.Text = attachment.Text
		} else {
			return nil, fmt.Errorf("attachment %q has no content source", attachment.FileName)
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
		if _, exists := seen[clean]; exists {
			return fmt.Errorf("duplicate bootstrap file path %q", clean)
		}
		seen[clean] = struct{}{}
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
