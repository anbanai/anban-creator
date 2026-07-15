package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSeednoteFinalizationOwnership(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	t.Run("agent owns finalization calls", func(t *testing.T) {
		agentPath := filepath.Join(root, "claudecode", "agents", "seednote.md")
		raw, err := os.ReadFile(agentPath)
		if err != nil {
			t.Fatalf("read %s: %v", agentPath, err)
		}
		agentBody := string(raw)
		saveTemplatePattern := regexp.MustCompile(`save_eligible\s*=\s*true[^\n]*条件满足时调用[^\n]*save_template`)
		if matches := saveTemplatePattern.FindAllStringIndex(agentBody, -1); len(matches) != 1 {
			t.Errorf("seednote agent has %d conditional save_template invocation contracts, want exactly one", len(matches))
		}
		for _, tool := range []string{"finalize_task_title", "submit_agent_feedback"} {
			callPattern := regexp.MustCompile(regexp.QuoteMeta(tool) + `\s*\(`)
			if calls := callPattern.FindAllStringIndex(agentBody, -1); len(calls) != 1 {
				t.Errorf("seednote agent has %d %s call expressions, want exactly one", len(calls), tool)
			}
		}
		if err := validateSeednoteFinalizationContract(agentBody); err != nil {
			t.Error(err)
		}
		if err := validateSeednoteArchiveContract(agentBody); err != nil {
			t.Error(err)
		}
		if err := validateSeednoteTemplateSaveContract(agentBody); err != nil {
			t.Error(err)
		}
	})

	t.Run("hooks contain only mechanical gates", func(t *testing.T) {
		hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
		hooksRaw, err := os.ReadFile(hooksPath)
		if err != nil {
			t.Fatalf("read %s: %v", hooksPath, err)
		}
		hooksText := string(hooksRaw)
		forbiddenTools := []string{"finalize_task_title", "submit_agent_feedback"}
		for _, forbidden := range forbiddenTools {
			if strings.Contains(hooksText, forbidden) {
				t.Errorf("Claude hooks JSON must not contain %q in any field", forbidden)
			}
		}
		var cfg struct {
			Hooks map[string][]struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Prompt  string `json:"prompt"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(hooksRaw, &cfg); err != nil {
			t.Fatalf("decode %s: %v", hooksPath, err)
		}
		for event, groups := range cfg.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if hook.Type == "prompt" {
						t.Errorf("Claude hook %s/%s must not use prompt hooks", event, group.Matcher)
					}
					for _, forbidden := range forbiddenTools {
						if strings.Contains(hook.Prompt, forbidden) || strings.Contains(hook.Command, forbidden) {
							t.Errorf("Claude hook %s/%s must not contain %q in prompt or command text", event, group.Matcher, forbidden)
						}
					}
				}
			}
		}
	})

	t.Run("skill assigns finalization to the agent", func(t *testing.T) {
		skillPath := filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md")
		skillRaw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("read %s: %v", skillPath, err)
		}
		skillBody := string(skillRaw)
		if strings.Contains(skillBody, "hook 统一负责") {
			t.Error("seednote skill must not assign finalization ownership to hooks")
		}
		const ownershipStatement = "最终标题排重与入库由 seednote Agent 的 title_finalization 阶段负责；本专业流程不另建 Hook 副本。"
		if !strings.Contains(skillBody, ownershipStatement) {
			t.Errorf("seednote skill missing canonical Agent ownership statement %q", ownershipStatement)
		}
	})
}

func TestSeednoteFinalizationContractRejectsMutations(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(wd, "..", "..", "claudecode", "agents", "seednote.md"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	valid := string(raw)
	if err := validateSeednoteFinalizationContract(valid); err != nil {
		t.Fatalf("valid seednote contract rejected: %v", err)
	}
	if err := validateSeednoteArchiveContract(valid); err != nil {
		t.Fatalf("valid seednote archive contract rejected: %v", err)
	}

	finalize := `finalize_task_title` + `(task_id=$TASK_ID, title=$FINAL_TITLE)`
	tests := []struct {
		name string
		body string
	}{
		{
			name: "finalize after image generation",
			body: swapFirstOccurrences(valid, finalize, "### 图片生成"),
		},
		{
			name: "failure state missing message",
			body: strings.Replace(valid, `"message":"<原始错误摘要>"`, `"detail":"<原始错误摘要>"`, 1),
		},
		{
			name: "archive runtime script missing",
			body: strings.Replace(valid, `${CLAUDE_PLUGIN_ROOT}/scripts/archive-seednote-workspace.sh`, "missing-archive-script", 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalizationErr := validateSeednoteFinalizationContract(tt.body)
			archiveErr := validateSeednoteArchiveContract(tt.body)
			if finalizationErr == nil && archiveErr == nil {
				t.Fatal("mutated seednote finalization contract unexpectedly passed")
			}
		})
	}
}

func validateSeednoteFinalizationContract(body string) error {
	titleStage, err := markdownSection(body, "#### 步骤 7b：标题终稿锁定", "### 图片生成")
	if err != nil {
		return err
	}
	titleStageAt := strings.Index(body, "#### 步骤 7b：标题终稿锁定")
	imageStageAt := strings.Index(body, "### 图片生成")
	if titleStageAt < 0 || imageStageAt <= titleStageAt {
		return fmt.Errorf("seednote must finalize the title after writing/humanizer and before image generation")
	}
	lastHumanizerAt := strings.LastIndex(body[:titleStageAt], "humanizer")
	finalizeCall := regexp.MustCompile(`finalize_task_title\s*\(`).FindStringIndex(titleStage)
	if lastHumanizerAt < 0 || finalizeCall == nil {
		return fmt.Errorf("seednote must finalize the title after writing/humanizer and before image generation")
	}
	for _, phrase := range []string{"最多进行 3 次调用尝试", "连续 3 次均返回重复标题"} {
		if !strings.Contains(titleStage, phrase) {
			return fmt.Errorf("seednote title finalization stage missing duplicate retry contract %q", phrase)
		}
	}
	duplicateAt := strings.Index(titleStage, "返回 `duplicate title` 错误时")
	contentAt := indexAfter(titleStage, "`$DIR/content.md`", duplicateAt)
	firstLineAt := indexAfter(titleStage, "第一行更新为新标题", contentAt)
	humanizerAt := indexAfter(titleStage, "humanizer", firstLineAt)
	complianceAt := indexAfter(titleStage, "标题合规", humanizerAt)
	retryAt := indexAfter(titleStage, "重试", complianceAt)
	if duplicateAt < 0 || contentAt <= duplicateAt || firstLineAt <= contentAt || humanizerAt <= firstLineAt || complianceAt <= humanizerAt || retryAt <= complianceAt {
		return fmt.Errorf("seednote duplicate handling must update content.md, rerun humanizer/title compliance, then retry before image generation")
	}
	for _, code := range []string{`error_code="finalize_title_failed"`, `error_code="duplicate_title_exhausted"`} {
		if !strings.Contains(titleStage, code) {
			return fmt.Errorf("seednote title finalization stage missing stable error code %s", code)
		}
	}
	failureAt := strings.Index(titleStage, "写入 `$DIR/failure-state.json`")
	stopAt := strings.Index(titleStage, "随后停止")
	noImagesAt := strings.Index(titleStage, "不得进入图片生成")
	if failureAt < 0 || stopAt <= failureAt || noImagesAt <= stopAt {
		return fmt.Errorf("seednote title finalization failure must explicitly stop before image generation")
	}
	failureContract := titleStage[failureAt:stopAt]
	for _, field := range []string{`"status":"recoverable_failure"`, `"stage":"title_finalization"`, `"error_code":`, `"message":`, `"resume_from":"title_finalization"`} {
		if !strings.Contains(failureContract, field) {
			return fmt.Errorf("seednote failure-state contract missing %s", field)
		}
	}
	for _, downstream := range []string{"image-plan", "cover", "prompts", "review", "compliance", "archive"} {
		if !strings.Contains(titleStage, downstream) {
			return fmt.Errorf("seednote accepted title contract missing downstream %q", downstream)
		}
	}
	if !strings.Contains(body, `"error_code":"title_changed_after_visuals"`) || !strings.Contains(body, `"resume_from":"title_finalization"`) {
		return fmt.Errorf("seednote final compliance must fail recoverably instead of changing a visualized title")
	}

	finalReport, err := markdownSection(body, "#### 步骤 12：最终报告", "\n---")
	if err != nil {
		return err
	}
	reportAt := strings.Index(finalReport, "向用户交付可复核的结果摘要")
	feedbackAt := regexp.MustCompile(`submit_agent_feedback\s*\(`).FindStringIndex(finalReport)
	if reportAt < 0 || feedbackAt == nil || feedbackAt[0] <= reportAt {
		return fmt.Errorf("seednote feedback must occur after the final report")
	}
	return nil
}

func indexAfter(body, needle string, after int) int {
	if after < 0 || after >= len(body) {
		return -1
	}
	relativeAt := strings.Index(body[after+1:], needle)
	if relativeAt < 0 {
		return -1
	}
	return after + 1 + relativeAt
}

func validateSeednoteArchiveContract(body string) error {
	archiveStage, err := markdownSection(body, "#### 步骤 10：归档工作目录", "#### 步骤 11：模板保存")
	if err != nil {
		return err
	}
	for _, forbidden := range []string{`mv "$DIR"/*`, "2>/dev/null", "TAR_EXCLUDES", "build_manifest()", "archive_failure", `ARCHIVE_RESULT=$(`} {
		if strings.Contains(archiveStage, forbidden) {
			return fmt.Errorf("seednote archive stage contains unsafe command %q", forbidden)
		}
	}
	for _, required := range []string{
		`${CLAUDE_PLUGIN_ROOT}/scripts/archive-seednote-workspace.sh`, "Bash tool 直接", "stdout JSON 原样", "退出码", "Bash tool result", "候选级 reservation", "不进行 TTL 抢占", "status", "code", "message", "archive_dir",
		`"status":"recoverable_failure"`, `"stage":"archive"`, `"error_code":`, `"message":`, `"resume_from":"archive"`,
		"非零", "保留 source", "停止", "不得执行步骤 11", "不得提交成功反馈",
	} {
		if !strings.Contains(archiveStage, required) {
			return fmt.Errorf("seednote archive protocol missing %q", required)
		}
	}
	return nil
}

func validateSeednoteTemplateSaveContract(body string) error {
	templateStage, err := markdownSection(body, "#### 步骤 11：模板保存", "#### 步骤 12：最终报告")
	if err != nil {
		return err
	}
	if strings.Contains(templateStage, "structure=") {
		return fmt.Errorf("seednote save_template must not pass unsupported structure")
	}
	for _, field := range []string{"type", "name", "category", "style_prompt", "tags"} {
		if !strings.Contains(templateStage, field+"=") {
			return fmt.Errorf("seednote save_template missing schema field %q", field)
		}
	}
	for _, phrase := range []string{"ARCHIVE_SUCCEEDED=true", "重复", "resume", "同一 template ID", "created", "existing"} {
		if !strings.Contains(templateStage, phrase) {
			return fmt.Errorf("seednote template save contract missing %q", phrase)
		}
	}
	return nil
}

func markdownSection(body, start, end string) (string, error) {
	startAt := strings.Index(body, start)
	if startAt < 0 {
		return "", fmt.Errorf("missing section start %q", start)
	}
	rest := body[startAt+len(start):]
	endAt := strings.Index(rest, end)
	if endAt < 0 {
		return "", fmt.Errorf("missing section end %q after %q", end, start)
	}
	return rest[:endAt], nil
}

func swapFirstOccurrences(body, first, second string) string {
	firstAt := strings.Index(body, first)
	secondAt := strings.Index(body, second)
	if firstAt < 0 || secondAt < 0 {
		return body
	}
	const firstMarker = "__FIRST_TOOL_CALL__"
	const secondMarker = "__SECOND_TOOL_CALL__"
	body = strings.Replace(body, first, firstMarker, 1)
	body = strings.Replace(body, second, secondMarker, 1)
	body = strings.Replace(body, firstMarker, second, 1)
	return strings.Replace(body, secondMarker, first, 1)
}
