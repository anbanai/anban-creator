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
		agentPath := filepath.Join(root, "plugins", "agents", "seednote.md")
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
		if err := validateSeednoteDeliveryContract(agentBody); err != nil {
			t.Error(err)
		}
		if err := validateSeednoteTemplateSaveContract(agentBody); err != nil {
			t.Error(err)
		}
	})

	t.Run("hooks contain only mechanical gates", func(t *testing.T) {
		hooksPath := filepath.Join(root, "plugins", "hooks", "hooks.json")
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

	t.Run("duplicate umbrella skills are absent", func(t *testing.T) {
		for _, distro := range []string{"plugins"} {
			skillPath := filepath.Join(root, distro, "skills", "seednote", "SKILL.md")
			if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
				t.Errorf("%s must not ship a duplicate top-level Seednote Skill", skillPath)
			}
		}
	})
}

func TestRuntimeHooksDoNotSubmitAgentFeedback(t *testing.T) {
	root := filepath.Clean(filepath.Join(mustGetwd(t), "..", ".."))
	for _, relativePath := range []string{
		"plugins/hooks/hooks.json",
		"plugins/hooks/hooks.json",
	} {
		t.Run(relativePath, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(relativePath))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if !json.Valid(raw) {
				t.Fatalf("%s is not valid JSON", relativePath)
			}
			if strings.Contains(string(raw), "submit_agent_feedback") {
				t.Fatalf("%s must not own submit_agent_feedback side effects", relativePath)
			}
		})
	}
}

func TestActiveRuntimeFeedbackScoresAreSerialized(t *testing.T) {
	root := filepath.Clean(filepath.Join(mustGetwd(t), "..", ".."))
	paths := []string{
		"plugins/agents/moments.md",
		"plugins/agents/seednote.md",
		"plugins/agents/live-slicer.md",
		"plugins/agents/ecommerce.md",
		"plugins/agents/designer.md",
		"plugins/agents/article.md",
		"plugins/agents/montage.md",
		"plugins/agents/moments.toml",
		"plugins/agents/seednote.toml",
		"plugins/agents/live-slicer.toml",
		"plugins/agents/ecommerce.toml",
		"plugins/agents/designer.toml",
		"plugins/agents/article.toml",
		"plugins/agents/montage.toml",
	}
	for _, relativePath := range paths {
		t.Run(relativePath, func(t *testing.T) {
			body := readRuntimeContractFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			if err := validateSerializedFeedbackCall(body); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChangedRuntimeFeedbackOwnership(t *testing.T) {
	root := filepath.Clean(filepath.Join(mustGetwd(t), "..", ".."))
	tests := []struct {
		path         string
		reportMarker string
	}{
		{path: "plugins/agents/seednote.md", reportMarker: "#### 步骤 12：最终报告"},
		{path: "plugins/agents/ecommerce.md", reportMarker: "#### 步骤 10：生成 manifest 与最终报告"},
		{path: "plugins/agents/moments.md", reportMarker: "最终摘要包含"},
		{path: "plugins/agents/article.md", reportMarker: "步骤 9 的最终验收都已写入报告后"},
		{path: "plugins/agents/seednote.toml", reportMarker: "## 完成后交付摘要（运行结束时执行）"},
		{path: "plugins/agents/ecommerce.toml", reportMarker: "#### 步骤 10：生成 manifest 与最终报告"},
		{path: "plugins/agents/moments.toml", reportMarker: "最终摘要包含"},
		{path: "plugins/agents/article.toml", reportMarker: "## 完成后交付摘要（运行结束时执行）"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			body := readRuntimeContractFile(t, filepath.Join(root, filepath.FromSlash(tt.path)))
			if err := validateSerializedFeedbackCall(body); err != nil {
				t.Fatal(err)
			}
			reportAt := strings.Index(body, tt.reportMarker)
			feedbackAt := regexp.MustCompile(`submit_agent_feedback\s*\(`).FindStringIndex(body)
			if reportAt < 0 || feedbackAt == nil || feedbackAt[0] <= reportAt {
				t.Fatalf("feedback must occur after final report marker %q", tt.reportMarker)
			}
		})
	}
}

func TestSerializedFeedbackCallValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "JSON stringify with explicit scores",
			body: `submit_agent_feedback(scores=JSON.stringify({quality:8, completeness:9, efficiency:7}))`,
		},
		{
			name: "JSON stringify with score variables",
			body: `submit_agent_feedback(scores=JSON.stringify({quality, completeness, efficiency}))`,
		},
		{
			name: "JSON string literal",
			body: `submit_agent_feedback(scores='{"quality":8,"completeness":9,"efficiency":7}')`,
		},
		{
			name:    "raw object with explicit scores",
			body:    `submit_agent_feedback(scores={quality:8, completeness:9, efficiency:7})`,
			wantErr: true,
		},
		{
			name:    "raw object with score variables",
			body:    `submit_agent_feedback(scores={quality, completeness, efficiency})`,
			wantErr: true,
		},
		{
			name:    "serialized scores missing a dimension",
			body:    `submit_agent_feedback(scores=JSON.stringify({quality:8, completeness:9}))`,
			wantErr: true,
		},
		{
			name:    "unserialized score variable",
			body:    `submit_agent_feedback(scores=scores)`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSerializedFeedbackCall(tt.body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSerializedFeedbackCall() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func validateSerializedFeedbackCall(body string) error {
	calls := regexp.MustCompile(`submit_agent_feedback\s*\(`).FindAllStringIndex(body, -1)
	if len(calls) != 1 {
		return fmt.Errorf("runtime has %d submit_agent_feedback calls, want exactly one", len(calls))
	}
	lineEnd := strings.IndexByte(body[calls[0][0]:], '\n')
	if lineEnd < 0 {
		lineEnd = len(body) - calls[0][0]
	}
	callLine := body[calls[0][0] : calls[0][0]+lineEnd]
	jsonStringify := regexp.MustCompile(`scores\s*=\s*JSON\.stringify\s*\(\s*\{\s*quality\s*(?::\s*[^,}\n]+)?\s*,\s*completeness\s*(?::\s*[^,}\n]+)?\s*,\s*efficiency\s*(?::\s*[^,}\n]+)?\s*\}\s*\)`)
	singleQuotedJSON := regexp.MustCompile(`scores\s*=\s*'\s*\{\s*"quality"\s*:\s*[^,}\n]+\s*,\s*"completeness"\s*:\s*[^,}\n]+\s*,\s*"efficiency"\s*:\s*[^,}\n]+\s*\}\s*'`)
	if !jsonStringify.MatchString(callLine) && !singleQuotedJSON.MatchString(callLine) {
		return fmt.Errorf("submit_agent_feedback scores must be an explicit JSON string serialization")
	}
	return nil
}

func readRuntimeContractFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestSeednoteRuntimeDeliveryContracts(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(raw)
			if err := validateSeednoteRuntimeDocument(body); err != nil {
				t.Fatal(err)
			}
			t.Run("duplicate feedback", func(t *testing.T) {
				if err := validateSeednoteRuntimeDocument(body + "\nsubmit_agent_feedback(task_id=$TASK_ID)\n"); err == nil {
					t.Fatal("duplicate feedback unexpectedly passed")
				}
			})
			t.Run("feedback before final report", func(t *testing.T) {
				reportHeading := regexp.MustCompile(`(?m)^#{2,4} (?:步骤 [0-9]+：)?最终报告\s*$`).FindString(body)
				mutated := swapFirstOccurrences(body, reportHeading, "submit_agent_feedback(")
				if err := validateSeednoteRuntimeDocument(mutated); err == nil {
					t.Fatal("pre-report feedback unexpectedly passed")
				}
			})
		})
	}
}

func TestSeednoteRuntimeDeliveryContractRejectsMutations(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(wd, "..", "..", "plugins", "agents", "seednote.md"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	valid := string(raw)
	if err := validateSeednoteRuntimeDocument(valid); err != nil {
		t.Fatalf("valid Seednote runtime rejected: %v", err)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "delivery title no longer locked", body: strings.Replace(valid, "第一行等于已接受 `$FINAL_TITLE`", "第一行读取标题", 1)},
		{name: "duplicate exhaustion code removed", body: strings.Replace(valid, "duplicate_title_exhausted", "duplicate_title", 1)},
		{name: "failure state deleted too early", body: strings.Replace(valid, "恢复执行仅在所有交付校验通过后、即将报告成功前删除", "恢复执行开始时删除", 1)},
		{name: "template failure becomes blocking", body: strings.Replace(valid, "不阻塞", "会阻塞", 1)},
		{name: "template tags omit JSON stringify", body: strings.Replace(valid, "JSON.stringify(template-meta.tags)", "template-meta.tags", 1)},
		{name: "template adds unsupported structure", body: strings.Replace(valid, "style_prompt=", "structure=viral-template.json, style_prompt=", 1)},
		{name: "feedback scores become raw object", body: strings.Replace(valid, `scores='{"quality":8,"completeness":8,"efficiency":8}'`, `scores={"quality":8,"completeness":8,"efficiency":8}`, 1)},
		{name: "feedback precedes final report", body: swapFirstOccurrences(valid, "#### 步骤 12：最终报告", "submit_agent_feedback(")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSeednoteRuntimeDocument(tt.body); err == nil {
				t.Fatal("mutated Seednote runtime contract unexpectedly passed")
			}
		})
	}
}

func validateSeednoteRuntimeDocument(body string) error {
	titleSections := regexp.MustCompile(`(?m)^#{3,4} .*标题终稿锁定.*$`).FindAllStringIndex(body, -1)
	if len(titleSections) == 0 {
		return fmt.Errorf("Seednote runtime missing title finalization section")
	}
	for _, bounds := range titleSections {
		section := sectionUntilHeading(body, bounds[0], regexp.MustCompile(`(?m)^#{3,4} .*?(?:图片生成|生成图片).*$`))
		for _, required := range []string{
			"finalize_task_title",
			"$DIR/failure-state.json",
			"recoverable_failure",
			"title_finalization",
			"error_code",
			"message",
			"resume_from",
			"finalize_title_failed",
			"duplicate_title_exhausted",
		} {
			if !strings.Contains(section, required) {
				return fmt.Errorf("Seednote title finalization section missing %q", required)
			}
		}
	}

	deliverySections := regexp.MustCompile(`(?m)^#{3,4} 步骤 [0-9]+：交付校验\s*$`).FindAllStringIndex(body, -1)
	if len(deliverySections) == 0 {
		return fmt.Errorf("Seednote runtime missing delivery validation section")
	}
	for _, bounds := range deliverySections {
		section := sectionUntilHeading(body, bounds[0], regexp.MustCompile(`(?m)^#{2,4} .*$`))
		for _, required := range []string{"content.md", "FINAL_TITLE", "第一行", "等于", "所有交付校验通过后", "即将报告成功前", "删除", "failure-state.json"} {
			if !strings.Contains(section, required) {
				return fmt.Errorf("Seednote delivery section missing %q", required)
			}
		}
		validationAt := strings.Index(section, "逐项校验")
		deleteAt := strings.Index(section, "删除")
		if validationAt < 0 || deleteAt <= validationAt {
			return fmt.Errorf("Seednote must delete resolved failure state after delivery validations")
		}
	}

	templateHeading := regexp.MustCompile(`(?m)^#{3,4} .*模板保存.*$`).FindStringIndex(body)
	if templateHeading == nil {
		return fmt.Errorf("Seednote runtime missing template save section")
	}
	templateSection := sectionUntilHeading(body, templateHeading[0], regexp.MustCompile(`(?m)^#{2,4} .*$`))
	if strings.Contains(templateSection, "structure=") {
		return fmt.Errorf("Seednote template save must not pass unsupported structure")
	}
	for _, required := range []string{
		"$DIR/viral-template.json",
		"$DIR/template-meta.json",
		"type=",
		"name=",
		"category=",
		"style_prompt=",
		"tags=JSON.stringify(template-meta.tags)",
		"重复",
		"resume",
		"同一 template ID",
		"created",
		"existing",
		"warning",
		"不阻塞",
	} {
		if !strings.Contains(templateSection, required) {
			return fmt.Errorf("Seednote template save section missing %q", required)
		}
	}

	reportHeading := regexp.MustCompile(`(?m)^#{2,4} (?:步骤 [0-9]+：)?最终报告\s*$`).FindStringIndex(body)
	if reportHeading == nil {
		return fmt.Errorf("Seednote runtime missing final report section")
	}
	reportAt := reportHeading[0]
	feedbackCalls := regexp.MustCompile(`submit_agent_feedback\s*\(`).FindAllStringIndex(body, -1)
	if len(feedbackCalls) != 1 {
		return fmt.Errorf("Seednote runtime has %d submit_agent_feedback calls, want exactly one", len(feedbackCalls))
	}
	if err := validateSerializedFeedbackCall(body); err != nil {
		return err
	}
	if feedbackCalls[0][0] <= reportAt {
		return fmt.Errorf("Seednote feedback must occur after the final report")
	}
	return nil
}

func sectionUntilHeading(body string, start int, next *regexp.Regexp) string {
	rest := body[start:]
	currentLineEnd := strings.Index(rest, "\n")
	if currentLineEnd < 0 {
		return rest
	}
	afterHeading := rest[currentLineEnd+1:]
	if nextAt := next.FindStringIndex(afterHeading); nextAt != nil {
		return rest[:currentLineEnd+1+nextAt[0]]
	}
	return rest
}

func TestSeednoteFinalizationContractRejectsMutations(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(wd, "..", "..", "plugins", "agents", "seednote.md"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	valid := string(raw)
	if err := validateSeednoteFinalizationContract(valid); err != nil {
		t.Fatalf("valid seednote contract rejected: %v", err)
	}
	if err := validateSeednoteDeliveryContract(valid); err != nil {
		t.Fatalf("valid seednote delivery contract rejected: %v", err)
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
			name: "canonical result directory missing",
			body: strings.Replace(valid, "成果目录（`$DIR`）", "成果目录", 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalizationErr := validateSeednoteFinalizationContract(tt.body)
			deliveryErr := validateSeednoteDeliveryContract(tt.body)
			if finalizationErr == nil && deliveryErr == nil {
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
		return fmt.Errorf("seednote must finalize the title after writing and before image generation")
	}
	lastWritingAt := strings.LastIndex(body[:titleStageAt], "**产出**：`$DIR/content.md`")
	finalizeCall := regexp.MustCompile(`finalize_task_title\s*\(`).FindStringIndex(titleStage)
	if lastWritingAt < 0 || finalizeCall == nil {
		return fmt.Errorf("seednote must finalize the title after writing and before image generation")
	}
	for _, phrase := range []string{"最多进行 3 次调用尝试", "连续 3 次均返回重复标题"} {
		if !strings.Contains(titleStage, phrase) {
			return fmt.Errorf("seednote title finalization stage missing duplicate retry contract %q", phrase)
		}
	}
	duplicateAt := strings.Index(titleStage, "返回 `duplicate title` 错误时")
	contentAt := indexAfter(titleStage, "`$DIR/content.md`", duplicateAt)
	firstLineAt := indexAfter(titleStage, "第一行为新标题", contentAt)
	humanizeAt := indexAfter(titleStage, "轻量去 AI", firstLineAt)
	complianceAt := indexAfter(titleStage, "标题合规", humanizeAt)
	retryAt := indexAfter(titleStage, "重试", complianceAt)
	if duplicateAt < 0 || contentAt <= duplicateAt || firstLineAt <= contentAt || humanizeAt <= firstLineAt || complianceAt <= humanizeAt || retryAt <= complianceAt {
		return fmt.Errorf("seednote duplicate handling must update content.md, rerun built-in de-AI/title compliance, then retry before image generation")
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
	for _, downstream := range []string{"image-plan", "cover", "prompts", "review", "compliance", "交付"} {
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

func validateSeednoteDeliveryContract(body string) error {
	deliveryStage, err := markdownSection(body, "#### 步骤 10：交付校验", "#### 步骤 11：模板保存")
	if err != nil {
		return err
	}
	for _, forbidden := range []string{"archive_workspace", "$ARCHIVE_DIR", "archive-seednote-workspace.sh", `mv "$DIR"/*`, "ARCHIVE_SUCCEEDED"} {
		if strings.Contains(body, forbidden) {
			return fmt.Errorf("seednote workflow contains legacy archive term %q", forbidden)
		}
	}
	for _, required := range []string{
		"`$DIR/content.md`",
		"逐项校验",
		"`image-review.md` 记录可见内容质量观察和“审核不可用” warning",
		"`analyze_image` 运行错误只保留在服务端观测记录中",
		"不创建失败态",
		"不单独让交付校验失败",
		"failure-state.json",
		"仅在所有交付校验通过后、即将报告成功前删除",
	} {
		if !strings.Contains(deliveryStage, required) {
			return fmt.Errorf("seednote delivery validation missing %q", required)
		}
	}
	if !strings.Contains(body, "成果目录（`$DIR`）") {
		return fmt.Errorf("seednote final report must identify 成果目录（`$DIR`）")
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
	for _, phrase := range []string{"`$DIR/viral-template.json`", "`$DIR/template-meta.json`", "重复", "resume", "同一 template ID", "created", "existing"} {
		if !strings.Contains(templateStage, phrase) {
			return fmt.Errorf("seednote template save contract missing %q", phrase)
		}
	}
	if strings.Contains(templateStage, "ARCHIVE_SUCCEEDED") {
		return fmt.Errorf("seednote template save must not depend on archive state")
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
