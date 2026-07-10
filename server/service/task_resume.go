package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

var (
	ErrTaskResumeNoInput          = errors.New("task resume requires prompt or files")
	ErrTaskResumeNotTerminal      = errors.New("only completed, failed, or cancelled tasks can be resumed")
	ErrTaskResumeWorkspaceMissing = errors.New("task workspace is missing")
	ErrTaskResumeConflict         = errors.New("task resume conflict")
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
	remoteArtifacts := taskHasRemoteArtifacts(task)
	if task.CleanedUpAt != nil && !remoteArtifacts {
		return nil, ErrTaskResumeWorkspaceMissing
	}
	workDir := s.taskWorkspaceDir(task.ID)
	if !remoteArtifacts {
		if info, statErr := os.Stat(workDir); statErr != nil || !info.IsDir() {
			return nil, ErrTaskResumeWorkspaceMissing
		}
	}

	var runDir, latestBody string
	var resumeAttachments []model.EntryAttachment
	if remoteArtifacts {
		resumeAttachments, latestBody, err = s.persistRemoteResumeInputs(ctx, task, prompt, params.Files)
		if err != nil {
			return nil, err
		}
	} else {
		runDir, latestBody, err = writeResumeInputs(ctx, workDir, prompt, params.Files)
		if err != nil {
			return nil, err
		}
	}

	if remoteArtifacts && latestBody == "" {
		return nil, ErrTaskResumeNoInput
	}
	if !remoteArtifacts && runDir == "" {
		return nil, ErrTaskResumeWorkspaceMissing
	}

	swapped, err := s.repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID)
	if err != nil {
		_ = os.RemoveAll(runDir)
		s.deleteRemoteResumeAttachments(ctx, resumeAttachments)
		return nil, fmt.Errorf("reset task for resume: %w", err)
	}
	if !swapped {
		_ = os.RemoveAll(runDir)
		s.deleteRemoteResumeAttachments(ctx, resumeAttachments)
		return nil, ErrTaskResumeConflict
	}
	if remoteArtifacts {
		merged := replaceResumeInputAttachments(task.InputAttachments.Data(), resumeAttachments)
		if err := s.repo.Tasks().UpdateInputAttachments(ctx, task.ID, merged); err != nil {
			s.deleteRemoteResumeAttachments(ctx, resumeAttachments)
			errMsg := "继续执行输入保存失败: " + err.Error()
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, task.ID, model.TaskStatusFailed, errMsg)
			_ = s.repo.Tasks().SetCompletedAt(ctx, task.ID)
			return nil, fmt.Errorf("persist remote resume attachments: %w", err)
		}
	} else if err := writeResumeLatest(workDir, latestBody); err != nil {
		_ = os.RemoveAll(runDir)
		errMsg := "继续执行输入写入失败: " + err.Error()
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, task.ID, model.TaskStatusFailed, errMsg)
		_ = s.repo.Tasks().SetCompletedAt(ctx, task.ID)
		return nil, fmt.Errorf("write resume latest: %w", err)
	}
	if err := s.repo.Tasks().AppendProgressLog(ctx, task.ID, "--- 继续执行：用户提交了补充指令/文件 ---"); err != nil {
		s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("append resume progress log failed")
	}
	s.logger.Info().
		Str("task_id", task.ID).
		Str("resume_dir", runDir).
		Int("resume_bytes", len(latestBody)).
		Msg("task resume inputs written")

	resumed, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("reload resumed task: %w", err)
	}
	if resumed.ExecutionTarget == model.ExecutionTargetLocal {
		deadline := time.Now().Add(LocalClaimWindow)
		resumed.LocalClaimDeadline = &deadline
		if err := s.repo.Tasks().Update(ctx, resumed); err != nil {
			return nil, fmt.Errorf("reset local claim deadline: %w", err)
		}
		s.logger.Info().Str("task_id", task.ID).Msg("resumed task routed to local executor, awaiting desktop claim")
		return resumed, nil
	}
	if err := s.EnqueueExecution(ctx, resumed, nil); err != nil {
		return nil, err
	}
	return resumed, nil
}

func taskHasRemoteArtifacts(task *model.Task) bool {
	if task == nil || task.Result == nil || strings.TrimSpace(*task.Result) == "" {
		return false
	}
	var result struct {
		RemoteArtifacts bool `json:"remote_artifacts"`
	}
	return json.Unmarshal([]byte(*task.Result), &result) == nil && result.RemoteArtifacts
}

func (s *TaskService) persistRemoteResumeInputs(ctx context.Context, task *model.Task, prompt string, files []ResumeTaskFile) ([]model.EntryAttachment, string, error) {
	if s.store == nil && len(files) > 0 {
		return nil, "", ErrTaskResumeWorkspaceMissing
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
			return nil, "", fmt.Errorf("read resume file: %w", err)
		}
		key := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID, "resume", stamp, "attachments", safeName)
		upload, err := s.store.Upload(ctx, key, bytes.NewReader(buf.Bytes()), "application/octet-stream")
		if err != nil {
			return nil, "", fmt.Errorf("upload resume file %s: %w", safeName, err)
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

func (s *TaskService) deleteRemoteResumeAttachments(ctx context.Context, attachments []model.EntryAttachment) {
	if s.store == nil {
		return
	}
	for _, attachment := range attachments {
		if attachment.Role != model.EntryAttachmentRoleResumeFile || strings.TrimSpace(attachment.Key) == "" {
			continue
		}
		if err := s.store.Delete(ctx, strings.TrimSpace(attachment.Key)); err != nil && s.logger != nil {
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

func (s *TaskService) taskWorkspaceDir(taskID string) string {
	if s.workspaceDir != "" {
		return filepath.Join(s.workspaceDir, taskID)
	}
	return agent.DefaultWorkspaceDir(taskID)
}

func writeResumeInputs(ctx context.Context, workDir, prompt string, files []ResumeTaskFile) (string, string, error) {
	resumeRoot := filepath.Join(workDir, ".anban-creator", "resume")
	stamp := time.Now().Format("20060102-150405.000000000")
	runDir := filepath.Join(resumeRoot, stamp)
	attachmentsDir := filepath.Join(runDir, "attachments")
	if err := os.MkdirAll(attachmentsDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create resume attachments dir: %w", err)
	}

	written := make([]resumeWrittenFile, 0, len(files))
	usedNames := map[string]int{}
	for _, file := range files {
		if file.Reader == nil {
			continue
		}
		safeName := uniqueResumeFilename(sanitizeResumeFilename(file.OriginalName), usedNames)
		dstPath := filepath.Join(attachmentsDir, safeName)
		if err := copyResumeFile(ctx, dstPath, file.Reader); err != nil {
			_ = os.RemoveAll(runDir)
			return "", "", err
		}
		rel, _ := filepath.Rel(runDir, dstPath)
		written = append(written, resumeWrittenFile{
			OriginalName: file.OriginalName,
			SafeName:     safeName,
			Label:        strings.TrimSpace(file.Label),
			RelPath:      filepath.ToSlash(rel),
		})
	}

	body := buildResumeInputBody(prompt, written)
	if err := os.WriteFile(filepath.Join(runDir, "input.md"), []byte(body), 0o644); err != nil {
		_ = os.RemoveAll(runDir)
		return "", "", fmt.Errorf("write resume input: %w", err)
	}
	return runDir, body, nil
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

func writeResumeLatest(workDir, body string) error {
	resumeRoot := filepath.Join(workDir, ".anban-creator", "resume")
	tmpPath := filepath.Join(resumeRoot, fmt.Sprintf(".latest-%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(resumeRoot, "latest.md")); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func copyResumeFile(ctx context.Context, dstPath string, src io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create resume file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("write resume file: %w", err)
	}
	return nil
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
