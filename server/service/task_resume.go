package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

var (
	ErrTaskResumeNoInput            = errors.New("task resume requires prompt or files")
	ErrTaskResumeNotTerminal        = errors.New("only completed, failed, or cancelled tasks can be resumed")
	ErrTaskResumeUnavailable        = errors.New("task resume requires kubernetes NAS execution")
	ErrTaskResumeStorageUnavailable = errors.New("task resume storage is unavailable")
	ErrTaskResumeConflict           = errors.New("task resume conflict")
)

// ResumeTaskParams carries the operator's continuation prompt and optional
// supplemental files for resuming a terminal task in its original workspace.
type ResumeTaskParams struct {
	Prompt string
	Files  []ResumeTaskFile
}

// ResumeTaskFile is one supplemental file uploaded from Studio.
type ResumeTaskFile struct {
	OriginalName string
	Label        string
	Reader       io.Reader
	Size         int64
}

type resumeWrittenFile struct {
	OriginalName string
	SafeName     string
	Label        string
	RelPath      string
	Attachment   model.EntryAttachment
}

// Resume requeues an existing terminal task in the same workspace. Unlike Clone,
// it does not create a new task and does not bill a fresh task charge.
func (s *TaskService) Resume(ctx context.Context, userID, taskID string, params ResumeTaskParams) (*model.Task, error) {
	if !s.nasResumeEnabled {
		return nil, ErrTaskResumeUnavailable
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" && len(params.Files) == 0 {
		return nil, ErrTaskResumeNoInput
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task not found")
	}
	if !model.IsTerminalTaskStatus(task.Status) {
		return nil, ErrTaskResumeNotTerminal
	}
	resumeAttachments, latestBody, err := s.persistResumeInputs(ctx, task, prompt, params.Files)
	if err != nil {
		return nil, err
	}
	if latestBody == "" {
		return nil, ErrTaskResumeNoInput
	}
	merged := replaceResumeInputAttachments(task.InputAttachments.Data(), resumeAttachments)

	swapped, err := s.repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID, merged)
	if err != nil {
		s.deleteRemoteResumeAttachments(ctx, resumeAttachments)
		return nil, fmt.Errorf("reset task for resume: %w", err)
	}
	if !swapped {
		s.deleteRemoteResumeAttachments(ctx, resumeAttachments)
		return nil, ErrTaskResumeConflict
	}
	s.deleteRemoteResumeAttachments(ctx, task.InputAttachments.Data())
	if err := s.repo.Tasks().AppendProgressLog(ctx, task.ID, "--- 继续执行：用户提交了补充指令/文件 ---"); err != nil {
		s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("append resume progress log failed")
	}
	s.logger.Info().
		Str("task_id", task.ID).
		Int("resume_bytes", len(latestBody)).
		Msg("task resume inputs persisted")

	applyResumedTaskState(task, merged)
	if err := s.EnqueueExecution(ctx, task, nil); err != nil {
		errMsg := "failed to enqueue resumed task: " + err.Error()
		recoveryTimeout := s.persistTimeout
		if recoveryTimeout <= 0 {
			recoveryTimeout = 15 * time.Second
		}
		recoveryCtx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
		defer cancel()
		if swapped, updateErr := s.repo.Tasks().FailPendingTask(recoveryCtx, task.ID, errMsg); updateErr != nil {
			s.logger.Error().Err(updateErr).Str("task_id", task.ID).Msg("failed to recover unqueued resumed task")
		} else if !swapped {
			s.logger.Warn().Str("task_id", task.ID).Msg("unqueued resumed task was no longer pending during recovery")
		}
		if task.ProjectID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(recoveryCtx, task.ProjectID)
		}
		return nil, fmt.Errorf("enqueue resumed task: %w", err)
	}
	return task, nil
}

func applyResumedTaskState(task *model.Task, attachments []model.EntryAttachment) {
	task.Status = model.TaskStatusPending
	task.StartedAt = nil
	task.CompletedAt = nil
	task.LastHeartbeatAt = nil
	task.ErrorMessage = ""
	task.Result = nil
	task.Progress = 0
	task.LatestProgress = datatypes.NewJSONType(model.ProgressPayload{})
	task.WorkflowStatus = nil
	task.PublishApprovalState = ""
	task.PendingDraftArticles = nil
	task.Published = false
	task.PublishedAt = nil
	task.SetInputAttachments(attachments)
	task.ExecutionTarget = model.ExecutionTargetCloud
	task.LocalClaimDeadline = nil
	task.ExecutorInfo = datatypes.NewJSONType(model.ExecutorMeta{})
}

func (s *TaskService) persistResumeInputs(ctx context.Context, task *model.Task, prompt string, files []ResumeTaskFile) ([]model.EntryAttachment, string, error) {
	if s.store == nil && len(files) > 0 {
		return nil, "", ErrTaskResumeStorageUnavailable
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	written := make([]resumeWrittenFile, 0, len(files))
	usedNames := map[string]int{}
	for _, file := range files {
		if file.Reader == nil {
			continue
		}
		safeName := uniqueResumeFilename(sanitizeResumeFilename(file.OriginalName), usedNames)
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, file.Reader); err != nil {
			s.deleteWrittenResumeFiles(ctx, written)
			return nil, "", fmt.Errorf("read resume file: %w", err)
		}
		key := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID, "resume", stamp, "attachments", safeName)
		upload, err := s.store.Upload(ctx, key, bytes.NewReader(buf.Bytes()), "application/octet-stream")
		if err != nil {
			s.deleteWrittenResumeFiles(ctx, written, key)
			return nil, "", fmt.Errorf("%w: upload resume file %s: %v", ErrTaskResumeStorageUnavailable, safeName, err)
		}
		if upload == nil || strings.TrimSpace(upload.Key) == "" {
			s.deleteWrittenResumeFiles(ctx, written, key)
			return nil, "", fmt.Errorf("%w: upload resume file %s returned no object key", ErrTaskResumeStorageUnavailable, safeName)
		}
		written = append(written, resumeWrittenFile{
			OriginalName: file.OriginalName,
			SafeName:     safeName,
			Label:        strings.TrimSpace(file.Label),
			RelPath:      filepath.ToSlash(filepath.Join("attachments", safeName)),
			Attachment: model.EntryAttachment{
				Type:        "document",
				URL:         upload.URL,
				FileName:    safeName,
				ContentType: "application/octet-stream",
				Size:        int64(buf.Len()),
				Role:        model.EntryAttachmentRoleResumeFile,
				Key:         upload.Key,
			},
		})
	}
	body := buildResumeInputBody(prompt, written)
	attachments := []model.EntryAttachment{{
		Type:     "document",
		Text:     body,
		FileName: "latest.md",
		Role:     model.EntryAttachmentRoleResumeLatest,
		Size:     int64(len(body)),
	}}
	for _, file := range written {
		attachments = append(attachments, file.Attachment)
	}
	return attachments, body, nil
}

func (s *TaskService) deleteWrittenResumeFiles(ctx context.Context, written []resumeWrittenFile, extraKeys ...string) {
	attachments := make([]model.EntryAttachment, 0, len(written)+len(extraKeys))
	for _, file := range written {
		attachments = append(attachments, file.Attachment)
	}
	for _, key := range extraKeys {
		attachments = append(attachments, model.EntryAttachment{Role: model.EntryAttachmentRoleResumeFile, Key: key})
	}
	s.deleteRemoteResumeAttachments(ctx, attachments)
}

func (s *TaskService) deleteRemoteResumeAttachments(_ context.Context, attachments []model.EntryAttachment) {
	if s.store == nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, attachment := range attachments {
		if attachment.Role != model.EntryAttachmentRoleResumeFile || strings.TrimSpace(attachment.Key) == "" {
			continue
		}
		if err := s.store.Delete(cleanupCtx, strings.TrimSpace(attachment.Key)); err != nil && s.logger != nil {
			s.logger.Warn().Err(err).Str("key", attachment.Key).Msg("delete orphaned resume attachment failed")
		}
	}
}

func replaceResumeInputAttachments(existing, resume []model.EntryAttachment) []model.EntryAttachment {
	out := make([]model.EntryAttachment, 0, len(existing)+len(resume))
	for _, attachment := range existing {
		if model.IsResumeEntryAttachment(attachment) {
			continue
		}
		out = append(out, attachment)
	}
	out = append(out, resume...)
	return out
}

func buildResumeInputBody(prompt string, written []resumeWrittenFile) string {
	var b strings.Builder
	b.WriteString("# 继续执行补充\n\n")
	b.WriteString("请基于当前工作目录继续执行，不要清空或覆盖已有产物，除非补充指令明确要求替换。\n\n")
	if prompt != "" {
		b.WriteString("## 补充指令\n\n")
		b.WriteString(prompt)
		b.WriteString("\n\n")
	}
	if len(written) > 0 {
		b.WriteString("## 补充文件\n\n")
		for _, f := range written {
			b.WriteString("- 原文件名：")
			b.WriteString(f.OriginalName)
			b.WriteString("\n")
			b.WriteString("  - 保存文件名：")
			b.WriteString(f.SafeName)
			b.WriteString("\n")
			b.WriteString("  - 相对路径：")
			b.WriteString(f.RelPath)
			b.WriteString("\n")
			if label := f.Label; label != "" {
				b.WriteString("  - 文件说明：")
				b.WriteString(label)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func sanitizeResumeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "attachment"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r):
			b.WriteRune('_')
		case unicode.IsSpace(r):
			b.WriteRune('_')
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.Trim(b.String(), "._ ")
	if cleaned == "" {
		return "attachment"
	}
	return cleaned
}

func uniqueResumeFilename(name string, used map[string]int) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", base, used[name], ext)
}
