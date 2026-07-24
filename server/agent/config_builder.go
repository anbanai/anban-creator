package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"
)

// maxReferenceImageBytes caps downloaded reference image size to prevent
// unbounded memory/disk usage. Mirrors the upload limit in handler/file.go.
const maxReferenceImageBytes int64 = 10 << 20 // 10 MB

const (
	referenceImageDirName  = ".anban-creator"
	referenceImageFileName = "reference.png"
	ReferenceImagePath     = referenceImageDirName + "/" + referenceImageFileName
)

var referenceMaterializeBeforeCommitHook func() error

const maxInputAttachmentBytes int64 = 50 << 20 // 50 MB

var unsafeAttachmentFilenameRunes = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type MaterializedInputAttachment struct {
	AttachmentIndex int    `json:"attachment_index"`
	Type            string `json:"type,omitempty"`
	URL             string `json:"url,omitempty"`
	Text            string `json:"text,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Size            int64  `json:"size,omitempty"`
	Path            string `json:"path,omitempty"`
	Instruction     string `json:"instruction,omitempty"`
	UploadID        string `json:"upload_id,omitempty"`
	Key             string `json:"key,omitempty"`
}

type MaterializedInputAttachmentError struct {
	AttachmentIndex int    `json:"attachment_index"`
	Type            string `json:"type,omitempty"`
	URL             string `json:"url,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	Instruction     string `json:"instruction,omitempty"`
	UploadID        string `json:"upload_id,omitempty"`
	Key             string `json:"key,omitempty"`
	Error           string `json:"error"`
}

// EffectiveProject returns the task snapshot view when a task carries one,
// otherwise the live project. Runtime config generation should use this so old
// tasks remain reproducible after project edits.
func EffectiveProject(ch *model.Project, task *model.Task) *model.Project {
	if ch == nil || task == nil {
		return ch
	}
	return model.ProjectFromSnapshot(ch, task.ProjectSnapshot.Data())
}

// BuildAppConfig constructs an app/config.Config from a Project DB record plus the
// resolved style dimensions. For new tasks this project is the frozen task
// snapshot; old rows without a snapshot fall back through legacy task overrides.
// This bridges the multi-user server config to the single-account app config used
// by the Anban Creator agent runtime.
//
// resolved carries the effective values (resolver.ResolveStyle); only
// the dimensions each platform's settings.json slot consumes are read here:
// Article.Writer / Article.Author / Article.Theme and Seednote.VisualStyle,
// each driven by the resolved value (not the raw project column).
func BuildAppConfig(ch *model.Project, resolved resolver.Resolved, imageAPICfg *srvconfig.ImageAPIConfig, taskImageRatio string, hasReference bool) (*appconfig.Config, error) {
	cfg := &appconfig.Config{
		Name:        ch.Name,
		Positioning: ch.Instructions,
	}

	// Parse keywords (comma or space separated).
	if ch.Keywords != "" {
		for _, kw := range strings.Split(ch.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				cfg.Keywords = append(cfg.Keywords, kw)
			}
		}
	}

	// WeChat credentials from Project.
	cfg.Wechat.AppID = ch.GetWechatAppID()
	cfg.Wechat.Secret = ch.GetWechatSecret()

	// Platform-specific fields. The style/author/theme values come from the
	// resolved set. For current tasks, the raw project already represents the
	// frozen snapshot; legacy task overrides are applied only for old rows.
	switch ch.Platform {
	case model.ScopeArticle:
		// Author is the publish署名 (goes to draft.json's author key at publish).
		cfg.Wechat.Article.Author = resolved.Author
		// Writer is the writer RESOURCE key (e.g. "dan-koe") — NOT the image
		// visual style. The article visual style is orthogonal and is read by the
		// agent solely from get_project_profile (MCP); it never enters the user
		// prompt nor settings.json.
		cfg.Wechat.Article.Writer = resolved.Writer
		cfg.Wechat.Article.Theme = resolved.Theme
	case model.ScopeSeednote:
		cfg.Seednote = &appconfig.SeednoteConfig{}
		// Seednote visual style is an image description (no separate writer dimension).
		cfg.Seednote.VisualStyle = resolved.VisualStyle
	}

	// Apply global image API config from server config.
	if imageAPICfg != nil {
		if imageAPICfg.Cover != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Cover.Image = *imageAPICfg.Cover
			case model.ScopeSeednote:
				cfg.Seednote.Cover.Image = *imageAPICfg.Cover
			}
		}
		if imageAPICfg.Content != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Content.Image = *imageAPICfg.Content
			case model.ScopeSeednote:
				cfg.Seednote.Content.Image = *imageAPICfg.Content
			}
		}

	}

	// Apply image ratio override: task-level > project-level. Business defaults
	// live in the agent Skill workflows, not in server model configuration.
	effectiveRatio := taskImageRatio
	if effectiveRatio == "" {
		effectiveRatio = ch.ImageRatio
	}
	if effectiveRatio != "" {
		switch ch.Platform {
		case model.ScopeArticle:
			cfg.Wechat.Article.Cover.Image.Size = effectiveRatio
			cfg.Wechat.Article.Content.Image.Size = effectiveRatio
		case model.ScopeSeednote:
			cfg.Seednote.Cover.Image.Size = effectiveRatio
			cfg.Seednote.Content.Image.Size = effectiveRatio
		}
	}

	// Runtime reference assets always materialize at this fixed private path.
	if hasReference {
		switch ch.Platform {
		case model.ScopeArticle:
			cfg.Wechat.Article.Cover.Image.Refer = ReferenceImagePath
			cfg.Wechat.Article.Content.Image.Refer = ReferenceImagePath
		case model.ScopeSeednote:
			cfg.Seednote.Cover.Image.Refer = ReferenceImagePath
			cfg.Seednote.Content.Image.Refer = ReferenceImagePath
		}
	}

	return cfg, nil
}

// writeSettingsJSON writes the app config to the workspace's .anban-creator/settings.json.
// The Anban Creator runtime reads config from CWD/.anban-creator/settings.json as its
// highest-priority search path.
func writeSettingsJSON(workDir string, cfg *appconfig.Config) error {
	path := filepath.Join(workDir, appconfig.ConfigDir, appconfig.ConfigFileName)
	if err := appconfig.SaveConfig(path, cfg); err != nil {
		return fmt.Errorf("write settings.json: %w", err)
	}
	return nil
}

// BuildAutoMemorySettingsJSON returns a Claude Code settings JSON document that
// points auto memory at the task-local runtime memory directory.
func BuildAutoMemorySettingsJSON(autoMemoryDir string) (string, error) {
	return buildAutoMemorySettingsJSON(autoMemoryDir)
}

func buildAutoMemorySettingsJSON(autoMemoryDir string) (string, error) {
	autoMemoryDir = strings.TrimSpace(autoMemoryDir)
	if autoMemoryDir == "" {
		return "", nil
	}
	data, err := json.Marshal(map[string]string{"autoMemoryDirectory": autoMemoryDir})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeProjectCLAUDEMD writes a project's positioning into a fixed CLAUDE.md
// template in the workspace root. Claude Code loads CLAUDE.md from the cwd as
// project memory, so all skills/sub-agents in the session receive the same
// positioning Studio displays. It is a no-op when the project is nil or blank.
func writeProjectCLAUDEMD(workDir string, project *model.Project) error {
	if project == nil || strings.TrimSpace(project.Instructions) == "" {
		return nil
	}
	path := filepath.Join(workDir, "CLAUDE.md")
	content := "# CLAUDE.md\n\n## 项目定位\n\n" + strings.TrimSpace(project.Instructions)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write CLAUDE.md: %w", err)
	}
	return nil
}

// TaskTypeToAgent maps server task types to Claude Code agent names.
func TaskTypeToAgent(taskType string) string {
	switch taskType {
	case model.ScopeArticle:
		return "article"
	case model.ScopeSeednote:
		return "seednote"
	case model.ScopeMoments:
		return "moments"
	case model.ScopeEcommerce:
		return "ecommerce"
	case model.ScopeMontage:
		return "montage"
	case model.TaskTypeLiveSlicer:
		return model.TaskTypeLiveSlicer
	default:
		return "seednote"
	}
}

// TaskToAgent maps a full task snapshot to the Claude Code agent name.
func TaskToAgent(task *model.Task) string {
	if task == nil {
		return TaskTypeToAgent("")
	}
	return TaskTypeToAgent(task.Type)
}

// MaterializeReferenceAsset reads only the repository-owned storage key. It
// deliberately has no URL or HTTP fallback. Exact-size validation detects
// truncated or extended reads; same-size mutation remains governed by finalized
// object immutability and storage ACLs.
func MaterializeReferenceAsset(ctx context.Context, store storage.Provider, workDir string, asset *model.Asset) error {
	if asset == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil {
		return errors.New("storage provider is unavailable")
	}
	key := strings.TrimSpace(asset.StorageKey)
	if key == "" {
		return errors.New("reference asset storage key is empty")
	}
	if asset.Size <= 0 {
		return errors.New("reference asset size is invalid")
	}
	if asset.Size > maxReferenceImageBytes {
		return fmt.Errorf("reference asset: %w", storage.ErrObjectExceedsMaxSize)
	}
	data, err := storage.ReadObject(ctx, store, key, maxReferenceImageBytes)
	if err != nil {
		return fmt.Errorf("read reference asset: %w", err)
	}
	if int64(len(data)) != asset.Size {
		return fmt.Errorf("reference asset size mismatch: read %d bytes, expected %d", len(data), asset.Size)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return materializeReferenceAssetBytes(ctx, workDir, data)
}

// DownloadProductImages downloads each product photo URL into the workspace's
// .anban-creator/products/ directory (used by e-commerce tasks), preserving upload
// order with 1-indexed names (product_01.<ext>, product_02.<ext>, ...). It also
// writes index.json listing the exact filenames so the agent can reference them
// deterministically (extensions vary by upload). Returns the count successfully
// materialized; per-image failures are logged and skipped (best-effort).
//
// Product-photo resolution retains its independent attachment URL contract.
func DownloadProductImages(ctx context.Context, store storage.Provider, logger *zerolog.Logger, workDir, userID string, urls []string) int {
	if len(urls) == 0 {
		return 0
	}
	destDir := filepath.Join(workDir, appconfig.ConfigDir, "products")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("create products dir failed")
		}
		return 0
	}

	names := make([]string, 0, len(urls))
	for i, imageURL := range urls {
		data, err := fetchImageBytes(ctx, store, userID, imageURL)
		if err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("url", imageURL).Int("index", i+1).Msg("failed to download product photo, skipping")
			}
			continue
		}
		name := fmt.Sprintf("product_%02d%s", i+1, imageExtFromURL(imageURL))
		if err := os.WriteFile(filepath.Join(destDir, name), data, 0o644); err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("name", name).Msg("write product photo failed, skipping")
			}
			continue
		}
		names = append(names, name)
	}

	if len(names) > 0 {
		if indexBytes, err := json.Marshal(names); err == nil {
			if err := os.WriteFile(filepath.Join(destDir, "index.json"), indexBytes, 0o644); err != nil {
				if logger != nil {
					logger.Warn().Err(err).Msg("write products index.json failed")
				}
			}
		}
	}
	return len(names)
}

// DownloadInputAttachments materializes AI-entry attachments into
// .anban-creator/input-attachments and writes index.json with stable local paths.
func DownloadInputAttachments(ctx context.Context, store storage.Provider, logger *zerolog.Logger, workDir, userID string, attachments []model.EntryAttachment) int {
	destDir := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("create input attachments dir failed")
		}
		return 0
	}

	index := make([]MaterializedInputAttachment, 0, len(attachments))
	failures := make([]MaterializedInputAttachmentError, 0)
	for i, attachment := range attachments {
		if model.IsResumeEntryAttachment(attachment) {
			continue
		}
		attachmentIndex := i + 1
		name := inputAttachmentFilename(attachmentIndex, attachment)
		path := filepath.Join(destDir, name)
		var data []byte
		var err error
		if rawKey := strings.TrimSpace(attachment.Key); rawKey != "" {
			key, validKey := explicitStorageObjectKey(rawKey)
			if !validKey {
				err = fmt.Errorf("attachment storage key is invalid")
			} else if key, err = runtimeStorageObjectKey(key, userID, attachment.UploadID, true); err != nil {
			} else if store == nil {
				err = fmt.Errorf("storage provider is required for attachment key")
			} else {
				data, err = readStorageObject(ctx, store, key, maxInputAttachmentBytes)
			}
		} else if strings.TrimSpace(attachment.URL) != "" {
			data, err = fetchAttachmentBytes(ctx, store, attachment.URL, userID, attachment.UploadID, true)
		} else if strings.TrimSpace(attachment.Text) != "" {
			data = []byte(strings.TrimSpace(attachment.Text))
		} else {
			continue
		}
		if err != nil {
			failures = append(failures, MaterializedInputAttachmentError{
				AttachmentIndex: attachmentIndex,
				Type:            attachment.Type,
				URL:             attachment.URL,
				FileName:        attachment.FileName,
				Instruction:     attachment.Instruction,
				UploadID:        attachment.UploadID,
				Key:             attachment.Key,
				Error:           err.Error(),
			})
			if logger != nil {
				logger.Warn().Err(err).Str("url", attachment.URL).Int("index", attachmentIndex).Msg("failed to download input attachment, skipping")
			}
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			failures = append(failures, MaterializedInputAttachmentError{
				AttachmentIndex: attachmentIndex,
				Type:            attachment.Type,
				URL:             attachment.URL,
				FileName:        attachment.FileName,
				Instruction:     attachment.Instruction,
				UploadID:        attachment.UploadID,
				Key:             attachment.Key,
				Error:           err.Error(),
			})
			if logger != nil {
				logger.Warn().Err(err).Str("name", name).Msg("write input attachment failed, skipping")
			}
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(appconfig.ConfigDir, "input-attachments", name))
		index = append(index, MaterializedInputAttachment{
			AttachmentIndex: attachmentIndex,
			Type:            attachment.Type,
			URL:             attachment.URL,
			Text:            attachment.Text,
			FileName:        attachment.FileName,
			ContentType:     attachment.ContentType,
			Size:            attachment.Size,
			Path:            relPath,
			Instruction:     attachment.Instruction,
			UploadID:        attachment.UploadID,
			Key:             attachment.Key,
		})
	}

	if indexBytes, err := json.MarshalIndent(index, "", "  "); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("marshal input attachments index.json failed")
		}
	} else if err := os.WriteFile(filepath.Join(destDir, "index.json"), indexBytes, 0o644); err != nil && logger != nil {
		logger.Warn().Err(err).Msg("write input attachments index.json failed")
	}

	errorsPath := filepath.Join(destDir, "errors.json")
	if len(failures) > 0 {
		if failureBytes, err := json.MarshalIndent(failures, "", "  "); err != nil {
			if logger != nil {
				logger.Warn().Err(err).Msg("marshal input attachments errors.json failed")
			}
		} else if err := os.WriteFile(errorsPath, failureBytes, 0o644); err != nil && logger != nil {
			logger.Warn().Err(err).Msg("write input attachments errors.json failed")
		}
	} else if err := os.Remove(errorsPath); err != nil && !errors.Is(err, os.ErrNotExist) && logger != nil {
		logger.Warn().Err(err).Msg("remove stale input attachments errors.json failed")
	}
	return len(index)
}

func hasNonResumeInputAttachments(attachments []model.EntryAttachment) bool {
	for _, attachment := range attachments {
		if !model.IsResumeEntryAttachment(attachment) {
			return true
		}
	}
	return false
}

// MaterializeResumeInputs restores persisted task resume inputs into the
// workspace location consumed by AppendResumeContextToPrompt.
func MaterializeResumeInputs(ctx context.Context, store storage.Provider, logger *zerolog.Logger, workDir, userID string, attachments []model.EntryAttachment) (int, error) {
	var latest *model.EntryAttachment
	files := make([]model.EntryAttachment, 0)
	for i := range attachments {
		switch attachments[i].Role {
		case model.EntryAttachmentRoleResumeLatest:
			latest = &attachments[i]
		case model.EntryAttachmentRoleResumeFile:
			files = append(files, attachments[i])
		}
	}
	if latest == nil || strings.TrimSpace(latest.Text) == "" {
		return 0, nil
	}
	seenNames := make(map[string]struct{}, len(files))
	for _, attachment := range files {
		name, err := CanonicalResumeAttachmentFilename(attachment.FileName)
		if err != nil {
			return 0, err
		}
		portableKey := PortableFilenameKey(name)
		if _, exists := seenNames[portableKey]; exists {
			return 0, fmt.Errorf("duplicate resume attachment filename %q", name)
		}
		seenNames[portableKey] = struct{}{}
	}
	resumeRoot := filepath.Join(workDir, appconfig.ConfigDir, "resume")
	attachmentsDir := filepath.Join(resumeRoot, "attachments")
	if err := os.MkdirAll(resumeRoot, 0o755); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("create resume input root failed")
		}
		return 0, fmt.Errorf("create resume input root: %w", err)
	}
	stagedAttachmentsDir, err := os.MkdirTemp(resumeRoot, ".attachments-")
	if err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("stage resume input dir failed")
		}
		return 0, fmt.Errorf("stage resume input dir: %w", err)
	}
	defer os.RemoveAll(stagedAttachmentsDir)
	written := 0
	for _, attachment := range files {
		data, err := fetchResumeAttachmentBytes(ctx, store, attachment, userID)
		if err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("url", attachment.URL).Msg("failed to fetch resume attachment")
			}
			return written, fmt.Errorf("fetch resume attachment %q: %w", attachment.FileName, err)
		}
		name, err := CanonicalResumeAttachmentFilename(attachment.FileName)
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(filepath.Join(stagedAttachmentsDir, name), data, 0o644); err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("name", name).Msg("write resume attachment failed")
			}
			return written, fmt.Errorf("write resume attachment %q: %w", name, err)
		}
		written++
	}
	if err := os.RemoveAll(attachmentsDir); err != nil {
		return written, fmt.Errorf("replace resume attachment directory: %w", err)
	}
	if err := os.Rename(stagedAttachmentsDir, attachmentsDir); err != nil {
		return written, fmt.Errorf("publish resume attachment directory: %w", err)
	}
	body := []byte(latest.Text)
	tmpPath := filepath.Join(resumeRoot, fmt.Sprintf(".latest-%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("write resume latest temp failed")
		}
		return written, fmt.Errorf("write resume latest temp: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(resumeRoot, "latest.md")); err != nil {
		_ = os.Remove(tmpPath)
		if logger != nil {
			logger.Warn().Err(err).Msg("publish resume latest failed")
		}
		return written, fmt.Errorf("publish resume latest: %w", err)
	}
	return written + 1, nil
}

func fetchResumeAttachmentBytes(ctx context.Context, store storage.Provider, attachment model.EntryAttachment, userID string) ([]byte, error) {
	if store != nil && strings.TrimSpace(attachment.Key) != "" {
		key, err := runtimeStorageObjectKey(attachment.Key, userID, attachment.UploadID, true)
		if err != nil {
			return nil, err
		}
		data, err := storage.ReadObject(ctx, store, key, maxInputAttachmentBytes)
		if err == nil {
			return data, nil
		}
	}
	if strings.TrimSpace(attachment.URL) != "" {
		return fetchAttachmentBytes(ctx, store, attachment.URL, userID, attachment.UploadID, true)
	}
	if strings.TrimSpace(attachment.Text) != "" {
		return []byte(strings.TrimSpace(attachment.Text)), nil
	}
	return nil, fmt.Errorf("resume attachment has no readable source")
}

func fetchAttachmentBytes(ctx context.Context, store storage.Provider, rawURL, userID, uploadID string, requireUploadID bool) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if store != nil && store.IsOwnedURL(rawURL) {
		if key, ok := storage.StorageKeyFromURL(rawURL); ok {
			key, err := runtimeStorageObjectKey(key, userID, uploadID, requireUploadID)
			if err != nil {
				return nil, err
			}
			if data, err := storage.ReadObject(ctx, store, key, maxInputAttachmentBytes); err == nil {
				return data, nil
			} else if errors.Is(err, storage.ErrObjectExceedsMaxSize) || errors.Is(err, storage.ErrBoundedReadUnsupported) {
				return nil, fmt.Errorf("download: %w", err)
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInputAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxInputAttachmentBytes {
		return nil, fmt.Errorf("download: file too large (%d bytes)", len(data))
	}
	return data, nil
}

func inputAttachmentFilename(index int, attachment model.EntryAttachment) string {
	base := sanitizeAttachmentFilename(attachment.FileName)
	source := firstNonEmptyAttachmentSource(attachment.URL, attachment.Key)
	if base == "" {
		base = sanitizeAttachmentFilename(filenameFromURLPath(source))
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" {
		ext = inputAttachmentExt(attachment.ContentType, source)
	}
	if base == "" {
		base = "attachment" + ext
	} else if ext != "" && filepath.Ext(base) == "" {
		base += ext
	}
	return fmt.Sprintf("attachment_%02d_%s", index, base)
}

func firstNonEmptyAttachmentSource(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeAttachmentFilename(raw string) string {
	base := filepath.Base(strings.TrimSpace(stripQueryAndFragment(raw)))
	base = strings.Trim(base, ".")
	if base == "" || base == "/" {
		return ""
	}
	base = unsafeAttachmentFilenameRunes.ReplaceAllString(base, "-")
	return strings.Trim(base, "-")
}

func filenameFromURLPath(rawURL string) string {
	rawURL = stripQueryAndFragment(rawURL)
	if rawURL == "" {
		return ""
	}
	return filepath.Base(rawURL)
}

func stripQueryAndFragment(raw string) string {
	raw = strings.SplitN(raw, "#", 2)[0]
	return strings.SplitN(raw, "?", 2)[0]
}

func inputAttachmentExt(contentType, rawURL string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/mp4":
		return ".m4a"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "text/csv":
		return ".csv"
	case "text/markdown":
		return ".md"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	}
	if ext := strings.ToLower(filepath.Ext(stripQueryAndFragment(rawURL))); ext != "" {
		return ext
	}
	return ".bin"
}

// fetchImageBytes resolves an image URL to its bytes. For URLs owned by this
// backend (OSS / local storage) it reads via the storage provider (works on
// private buckets); otherwise it downloads via HTTP. This remains isolated to
// the product-photo URL contract.
func fetchImageBytes(ctx context.Context, store storage.Provider, userID, imageURL string) ([]byte, error) {
	if key, ok := explicitStorageObjectKey(imageURL); ok {
		key, err := runtimeStorageObjectKey(key, userID, "", false)
		if err != nil {
			return nil, err
		}
		if store == nil {
			return nil, fmt.Errorf("storage provider is required for object key %q", key)
		}
		return readStorageObject(ctx, store, key, maxReferenceImageBytes)
	}
	if store != nil && store.IsOwnedURL(imageURL) {
		if key, ok := storage.StorageKeyFromURL(imageURL); ok {
			key, err := runtimeStorageObjectKey(key, userID, "", false)
			if err != nil {
				return nil, err
			}
			if data, err := storage.ReadObject(ctx, store, key, maxReferenceImageBytes); err == nil {
				return data, nil
			} else if errors.Is(err, storage.ErrObjectExceedsMaxSize) || errors.Is(err, storage.ErrBoundedReadUnsupported) {
				return nil, fmt.Errorf("download: %w", err)
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxReferenceImageBytes))
}

func explicitStorageObjectKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(raw, "/") {
		return "", false
	}
	key, ok := storage.StorageKeyFromURL(raw)
	if !ok {
		return "", false
	}
	key = filepath.ToSlash(strings.TrimPrefix(key, "/"))
	clean := filepath.ToSlash(filepath.Clean(key))
	if clean != key || clean == "." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return key, true
}

func runtimeStorageObjectKey(raw, userID, uploadID string, requireUploadID bool) (string, error) {
	parsed, err := storage.ParseRuntimeStorageKey(raw)
	if err != nil {
		return "", err
	}
	if parsed.FinalizedUpload != nil {
		if requireUploadID && strings.TrimSpace(uploadID) == "" {
			return "", storage.ErrFinalizedUploadIdentityMismatch
		}
		if err := parsed.ValidateFinalizedUploadIdentity(userID, uploadID); err != nil {
			return "", err
		}
	}
	return parsed.Key, nil
}

func readStorageObject(ctx context.Context, store storage.Provider, key string, maxBytes int64) ([]byte, error) {
	data, err := storage.ReadObject(ctx, store, key, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read storage object: %w", err)
	}
	return data, nil
}

// imageExtFromURL infers a lowercase image extension from the URL path, defaulting
// to .png when unknown. Extension is taken from the URL (the canonical source for
// server-owned storage URLs and OSS object keys) rather than sniffing bytes.
func imageExtFromURL(imageURL string) string {
	switch ext := strings.ToLower(filepath.Ext(imageURL)); ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return ext
	default:
		return ".png"
	}
}
