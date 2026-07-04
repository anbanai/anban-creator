package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	if task.CleanedUpAt != nil {
		return nil, ErrTaskResumeWorkspaceMissing
	}
	workDir := s.taskWorkspaceDir(task.ID)
	if info, statErr := os.Stat(workDir); statErr != nil || !info.IsDir() {
		return nil, ErrTaskResumeWorkspaceMissing
	}

	runDir, latestBody, err := writeResumeInputs(ctx, workDir, prompt, params.Files)
	if err != nil {
		return nil, err
	}

	swapped, err := s.repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID)
	if err != nil {
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("reset task for resume: %w", err)
	}
	if !swapped {
		_ = os.RemoveAll(runDir)
		return nil, ErrTaskResumeConflict
	}
	if err := writeResumeLatest(workDir, latestBody); err != nil {
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

	type writtenFile struct {
		OriginalName string
		SafeName     string
		Label        string
		RelPath      string
	}
	written := make([]writtenFile, 0, len(files))
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
		written = append(written, writtenFile{
			OriginalName: file.OriginalName,
			SafeName:     safeName,
			Label:        strings.TrimSpace(file.Label),
			RelPath:      filepath.ToSlash(rel),
		})
	}

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
			if f.Label != "" {
				b.WriteString("  - 文件说明：")
				b.WriteString(f.Label)
				b.WriteString("\n")
			}
		}
	}
	body := b.String()
	if err := os.WriteFile(filepath.Join(runDir, "input.md"), []byte(body), 0o644); err != nil {
		_ = os.RemoveAll(runDir)
		return "", "", fmt.Errorf("write resume input: %w", err)
	}
	return runDir, body, nil
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
