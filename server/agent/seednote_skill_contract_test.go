package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extractSeednoteReferenceContract(t *testing.T, body string) string {
	t.Helper()
	const start = "<!-- seednote-reference-contract:start -->"
	const end = "<!-- seednote-reference-contract:end -->"
	startAt := strings.Index(body, start)
	endAt := strings.Index(body, end)
	if startAt < 0 || endAt <= startAt {
		t.Fatalf("missing Seednote reference contract markers")
	}
	section := body[startAt+len(start) : endAt]
	return strings.Join(strings.Fields(section), " ")
}

func TestPluginAssetsDoNotUseRemovedGenerateImageWorkflowFields(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, subtree := range []string{"agents", "skills"} {
		err := filepath.Walk(filepath.Join(root, subtree), func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() || (filepath.Ext(path) != ".md" && filepath.Ext(path) != ".toml") {
				return nil
			}
			body := readRepoFile(t, path)
			for _, removed := range []string{"verify_with_vision", "verification_prompt", "upload_to_cdn", "operation_id"} {
				if strings.Contains(body, removed) {
					t.Fatalf("%s still uses removed generate_image workflow field %q", path, removed)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageWorkflowAssetsDoNotTrackGenerationPlumbing(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	paths := []string{
		filepath.Join(root, "agents", "article.md"),
		filepath.Join(root, "agents", "article.toml"),
		filepath.Join(root, "agents", "designer.md"),
		filepath.Join(root, "agents", "designer.toml"),
		filepath.Join(root, "agents", "ecommerce.md"),
		filepath.Join(root, "agents", "ecommerce.toml"),
		filepath.Join(root, "agents", "seednote.md"),
		filepath.Join(root, "agents", "seednote.toml"),
		filepath.Join(root, "skills", "article-cover-design", "SKILL.md"),
		filepath.Join(root, "skills", "article-publishing", "SKILL.md"),
		filepath.Join(root, "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "skills", "article-visual-design", "references", "content.md"),
		filepath.Join(root, "skills", "article", "SKILL.md"),
		filepath.Join(root, "skills", "ecommerce-visual-design", "SKILL.md"),
		filepath.Join(root, "skills", "line-art-coloring", "SKILL.md"),
		filepath.Join(root, "skills", "portrait-pose-variants", "SKILL.md"),
		filepath.Join(root, "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "skills", "short-video-cover", "SKILL.md"),
	}
	for _, path := range paths {
		body := readRepoFile(t, path)
		for _, removed := range []string{
			"provider", "image_model", "model_fallback_reason", "selection_reason",
			"response_type", "revised_prompt", "output_mime", "generation_attempts",
			"verification_audit", "模型路由", "图像模型", "服务端核验对象",
		} {
			if strings.Contains(body, removed) {
				t.Fatalf("%s still tracks image-generation plumbing %q", path, removed)
			}
		}
	}
}

func TestRuntimeImageWorkflowDocsDoNotRouteByProvider(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	roots := []string{
		filepath.Join(root, "agents", "article.md"),
		filepath.Join(root, "agents", "article.toml"),
		filepath.Join(root, "agents", "designer.md"),
		filepath.Join(root, "agents", "designer.toml"),
		filepath.Join(root, "agents", "ecommerce.md"),
		filepath.Join(root, "agents", "ecommerce.toml"),
		filepath.Join(root, "agents", "seednote.md"),
		filepath.Join(root, "agents", "seednote.toml"),
		filepath.Join(root, "skills", "article-cover-design"),
		filepath.Join(root, "skills", "article-publishing"),
		filepath.Join(root, "skills", "article-visual-design"),
		filepath.Join(root, "skills", "article"),
		filepath.Join(root, "skills", "config"),
		filepath.Join(root, "skills", "ecommerce-visual-design"),
		filepath.Join(root, "skills", "ecommerce"),
		filepath.Join(root, "skills", "line-art-coloring"),
		filepath.Join(root, "skills", "portrait-pose-variants"),
		filepath.Join(root, "skills", "seednote-visual-design"),
		filepath.Join(root, "skills", "short-video-cover"),
	}
	for _, walkRoot := range roots {
		err := filepath.Walk(walkRoot, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() || (filepath.Ext(path) != ".md" && filepath.Ext(path) != ".toml") {
				return nil
			}
			body := strings.ToLower(readRepoFile(t, path))
			for _, forbidden := range []string{
				"openai", "gpt-image", "dall-e", "gemini",
				"volcengine", "火山引擎", "seedream", "doubao",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still routes image work by provider/model name %q", path, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestReferenceSelectionUsesSemanticOrderAndServerOwnedLimits(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, path := range []string{
		filepath.Join(root, "agents", "designer.md"),
		filepath.Join(root, "skills", "line-art-coloring", "SKILL.md"),
		filepath.Join(root, "skills", "line-art-coloring", "references", "verification.md"),
		filepath.Join(root, "skills", "ecommerce", "references", "examples.md"),
	} {
		body := readRepoFile(t, path)
		for _, required := range []string{"参考图按语义相关性排序", "服务端负责路由与数量限制"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing semantic reference ownership term %q", path, required)
			}
		}
	}
}

func TestConfigSkillDoesNotExposeImageRouteConfiguration(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins", "skills", "config")
	for _, path := range []string{
		filepath.Join(root, "SKILL.md"),
		filepath.Join(root, "references", "examples.md"),
	} {
		body := readRepoFile(t, path)
		for _, required := range []string{"visual_style", "reference_image_path"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing semantic visual setting %q", path, required)
			}
		}
		lower := strings.ToLower(body)
		for _, forbidden := range []string{
			"image_provider", "image_model", "image provider", "image model",
			"图片生成服务", "图片服务", "图片模型", "模型配置", "provider", "openai", "gemini", "volcengine", "seedream",
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s still exposes image route configuration %q", path, forbidden)
			}
		}
	}
}

func TestSeednoteImagePromptsContainOnlyCreativeContent(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	paths := []string{
		filepath.Join(root, "agents", "seednote.md"),
		filepath.Join(root, "agents", "seednote.toml"),
		filepath.Join(root, "skills", "seednote-visual-design", "SKILL.md"),
	}
	for _, path := range paths {
		body := readRepoFile(t, path)
		for _, required := range []string{"image-prompts.md", "用途：", "提示词："} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing creative image-prompts contract %q", path, required)
			}
		}
		for _, removed := range []string{
			"generation_attempts", "selection_reason", "response_type", "output_mime",
			"实际width/height", "provider", "image_model", `"verification"`, "路由选择",
		} {
			if strings.Contains(body, removed) {
				t.Fatalf("%s still requires technical image-prompts field %q", path, removed)
			}
		}
	}
}

func TestImageCapabilitiesRemainIndependent(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, path := range []string{
		filepath.Join(root, "agents", "article.md"),
		filepath.Join(root, "agents", "article.toml"),
		filepath.Join(root, "skills", "article-publishing", "SKILL.md"),
	} {
		body := readRepoFile(t, path)
		for _, required := range []string{"generate_image", "analyze_image", "upload_image", "上传失败只重试上传，不重新生成"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing independent image capability contract %q", path, required)
			}
		}
	}

	for _, path := range []string{
		filepath.Join(root, "agents", "seednote.md"),
		filepath.Join(root, "agents", "seednote.toml"),
		filepath.Join(root, "skills", "seednote-visual-design", "SKILL.md"),
	} {
		body := readRepoFile(t, path)
		for _, required := range []string{
			"generate_image", "analyze_image", "单独调用", "不能阻止继续生成后续计划图片",
		} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing independent analysis contract %q", path, required)
			}
		}
	}
}

func TestSeednoteAnalysisCannotStopLaterPlannedImageGeneration(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	for _, path := range []string{
		filepath.Join(root, "agents", "seednote.md"),
		filepath.Join(root, "agents", "seednote.toml"),
		filepath.Join(root, "skills", "seednote-visual-design", "SKILL.md"),
	} {
		body := readRepoFile(t, path)
		for _, required := range []string{
			"只有 `generate_image` 本身失败或超时时，才写入 `$DIR/failure-state.json` 并停止图片阶段",
			"分析或内容质量结果只影响当前输出图的记录与创作重试",
			"当前图达到创作重试上限时标记 `quality_status=failed`",
			"必须继续生成剩余计划图片",
			"全部计划图片生成完成后再执行整体质量闸门",
		} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing non-blocking Seednote analysis term %q", path, required)
			}
		}
		for _, forbidden := range []string{
			"遇到关键失败时停止在当前阶段",
			"创作重试预算耗尽",
			"质量重试预算耗尽",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still lets analysis/content quality exhaustion stop later planned image generation via %q", path, forbidden)
			}
		}
	}
}

func TestSeednoteWorkflowAnalyzesRequestBeforeReferenceImages(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}

	artifacts := []string{
		"request-analysis.json",
		"request-analysis.md",
		"reference-analysis.json",
		"reference-analysis.md",
		"image-plan.md",
		"image-prompts.md",
		"image-review.md",
		"reference-usage-summary.json",
	}
	ordered := []string{
		"先读取用户统一提示词",
		"写出 `request-analysis.json` 与 `request-analysis.md`",
		"遍历 `index.json` 中每张可用图片",
		"写出 `reference-analysis.json` 与 `reference-analysis.md`",
		"写出 `image-plan.md`",
		"写出 `image-prompts.md`",
		"写入 `image-review.md`",
		"写出 `reference-usage-summary.json`",
	}
	required := []string{
		"此阶段不得先分析图片",
		"动态编写该图片独有的 `analyze_image` prompt",
		"每张可用图片都必须分析",
		"单张最多 3 次理解尝试",
		"每张输入图最多 3 次理解尝试",
		"不得向用户发起中途确认",
		"同产品/系列/型号",
		"新旧包装",
		"角度",
		"冲突分析",
		"不得请求用户决定",
		"保留已生成文件和 trace artifacts",
	}
	forbidden := []string{
		"向用户展示候选让其选择",
		"必须向用户展示所有可选项目",
		"封面失败两次后请求用户协助",
		"封面生成失败 | 重试两次，仍失败则请求用户协助",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body := readRepoFile(t, path)
			for _, artifact := range artifacts {
				if !strings.Contains(body, artifact) {
					t.Fatalf("%s missing required trace artifact %q", path, artifact)
				}
			}
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing automatic reference workflow term %q", path, term)
				}
			}
			previous := -1
			for _, phrase := range ordered {
				at := strings.Index(body, phrase)
				if at < 0 {
					t.Fatalf("%s missing ordered workflow phrase %q", path, phrase)
				}
				if at <= previous {
					t.Fatalf("%s has workflow phrase %q out of order", path, phrase)
				}
				previous = at
			}
			for _, phrase := range forbidden {
				if strings.Contains(body, phrase) {
					t.Fatalf("%s still requests a mid-run user decision via %q", path, phrase)
				}
			}
		})
	}
}

func TestSeednoteVisualWorkflowSelectsReferencesAndReviewsOutputsIndependently(t *testing.T) {
	root := repoRoot(t)
	visualSkills := []string{
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
	}
	contentReferences := []string{
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "references", "content.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "references", "content.md"),
	}
	selectionRule := "封面、内容图和尾图均不预设是否使用参考素材。每页根据 `image-plan.md` 独立选择 0、1 或多张原图；没有相关参考时使用纯文生图。项目级品牌参考图仍可作为旧数据来源，但不得覆盖本次输入附件中更具体、更新的产品事实。"

	for _, path := range visualSkills {
		t.Run(path, func(t *testing.T) {
			body := readRepoFile(t, path)
			for _, term := range []string{
				selectionRule,
				"对每张输出图独立决定使用 0、1 或多张附件",
				"不得把所有素材传给所有页面",
				"只传当前输出图相关的原始路径",
				"数组顺序必须与 prompt 中“参考图 1、参考图 2”一致",
				"图片生成成功后单独调用 `analyze_image`",
				"不能阻止继续生成后续计划图片",
				"可调整参考组合/顺序和创作 prompt 后重新生成",
				"单张最多 3 次",
			} {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing per-output reference workflow term %q", path, term)
				}
			}
		})
	}

	for _, path := range append(visualSkills, contentReferences...) {
		t.Run(path+"/removed-bans", func(t *testing.T) {
			body := readRepoFile(t, path)
			if !strings.Contains(body, selectionRule) {
				t.Fatalf("%s missing neutral per-page reference selection rule", path)
			}
			for _, forbidden := range []string{
				"内容图/尾图各自独立文生图（不传 ref_image_path）",
				"不传 ref_image_path（纯文生图）",
				"尾图 prompt 未沿用统一风格 | 尾图沿用共享「风格延续：{style}」块（不传参考图）",
				"调用 `generate_image`，不传参考图",
				"默认不传",
				"不使用封面作为参考图",
				"不使用参考图，避免 Seedream",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still contains unconditional reference ban %q", path, forbidden)
				}
			}
		})
	}
}

func TestSeednoteAgentsShareReferenceContract(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}

	var reference string
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			contract := extractSeednoteReferenceContract(t, readRepoFile(t, path))
			if strings.Contains(contract, "mcp__") {
				t.Fatalf("%s reference contract must use bare tool names", path)
			}
			if reference == "" {
				reference = contract
				return
			}
			if contract != reference {
				t.Fatalf("%s Seednote reference contract differs from the first runtime distribution", path)
			}
		})
	}
}

func TestSeednoteWorkflowDocumentsReferenceUsageSchemaAndFailurePolicy(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}
	required := []string{
		`"version": "1.0"`,
		`"attachment_index"`,
		`"file_name"`,
		`"url"`,
		`"instruction"`,
		`"status"`,
		`"decision_summary"`,
		`"analysis_attempts"`,
		`"warnings"`,
		`"references"`,
		`"purpose"`,
		`"quality_status"`,
		`"quality_notes"`,
		"唯一产品身份、Logo、包装、型号或核心结构证据不可用",
		"身份或结构幻觉",
		"冲突版本融合",
		"禁止内容",
		"页面无法履行职责",
		"非关键氛围或轻微构图问题",
		"保留已生成文件和 trace artifacts",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body := readRepoFile(t, path)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing reference summary schema/failure term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteVisualDesignSkillKeepsImageRelevanceContract(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
	}

	required := []string{
		"image-prompts.md",
		"image-review.md",
		"简体中文",
		"禁止英文",
		"伪词",
		"春日饮茶指南",
		"茉莉花茶",
		"白牡丹白茶",
		"85-90°C",
		"10秒出汤",
		"焖泡10秒",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read skill: %v", err)
			}
			body := string(data)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing required term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteVisualMethodologyIsDistributed(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
	}

	required := []string{
		"Seednote 视觉方法论",
		"内容蒸馏",
		"视觉策略",
		"Prompt 蓝图",
		"generate_image",
		"image-prompts.md",
		"image-review.md",
		"质量复盘",
		"editorial 信息层级",
		"Swiss/magazine 秩序感",
		"图文节奏",
		"analyze_image",
		"可见主体、文字、构图和合规",
		"不能阻止继续生成后续计划图片",
		"failure-state.json",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing Seednote visual methodology term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteAgentsTreatImageFailuresAsRecoverableFailedState(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}

	required := []string{
		"generate_image",
		"image-prompts.md",
		"image-review.md",
		"可见内容质量观察",
		"不能阻止继续生成后续计划图片",
		"failure-state.json",
		"停止在图片阶段",
		"不得提前删除",
	}
	forbidden := []string{
		"archive_workspace",
		"单张内容图失败时重试一次，仍失败则跳过",
		"单张内容图生成失败 | 重试一次，仍失败则跳过",
		"封面失败两次后请求用户协助",
		"封面生成失败 | 重试两次，仍失败则请求用户协助",
		"自动重试 + 降级，不中断流程",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body := readRepoFile(t, path)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing recoverable image failure term %q", path, term)
				}
			}
			for _, term := range forbidden {
				if strings.Contains(body, term) {
					t.Fatalf("%s still contains stale image fallback term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteWritingSkillKeepsUserInputLocking(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "skills", "seednote-writing", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-writing", "SKILL.md"),
	}

	required := []string{
		"用户输入锁定规则",
		"封面标题",
		"笔记正文",
		"话题标签",
		"不能覆盖用户指定标题",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read skill: %v", err)
			}
			body := string(data)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing required term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteSkillContracts_RuntimeImageMode(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "references", "content.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-visual-design", "references", "content.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"seednote_image_mode",
				"cover_only",
				"cover_content",
				"cover_tail",
				"full",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing seednote runtime image mode term %q", path, term)
				}
			}
			for _, stale := range []string{
				"图片构成要求",
				"user prompt 指令",
				"禁止生成尾图",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale seednote image-control phrase %q", path, stale)
				}
			}
		})
	}
}

func TestSeednoteAgentUsesAgentReachForExternalXHSData(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)

			for _, want := range []string{
				"agent-reach",
				"Agent-Reach",
				"agent-reach doctor --json",
				`xiaohongshu.status == "ok"`,
				"active_backend",
				"唯一外部数据入口",
				"backend 顺序和可用性完全由 Agent-Reach 决定",
				"原创模式不得",
				"账号画像",
				"不得生成虚构热门数据",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Agent-Reach contract term %q", path, want)
				}
			}

			for _, forbidden := range []string{
				"opencli xiaohongshu publish",
				"opencli xiaohongshu delete-note",
				"opencli xiaohongshu follow",
				"opencli xiaohongshu unfollow",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s must not include write-operation command %q", path, forbidden)
				}
			}
		})
	}
}

func TestAgentReachSkillsTreatUnavailableBackendAsOptionalForOriginalResearch(t *testing.T) {
	root := articleContractRepoRoot(t)
	canonicalPath := filepath.Join(root, "plugins", "skills", "agent-reach", "SKILL.md")
	canonicalData, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read %s: %v", canonicalPath, err)
	}
	canonical := string(canonicalData)
	for _, want := range []string{
		`xiaohongshu.status == "ok"`,
		"`active_backend` alone never proves usability",
		"OpenCLI",
		"xiaohongshu-mcp",
		"xhs-cli (xiaohongshu-cli)",
		"optional enhancement for original Seednote research",
		"must not create `failure-state.json`",
		"source content can be resolved",
		"Do not run `pip`, `pipx`, `npm`, `agent-reach install`",
		"channel_status",
	} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("%s missing managed Agent-Reach contract %q", canonicalPath, want)
		}
	}
	for _, distro := range []string{"plugins"} {
		path := filepath.Join(root, distro, "skills", "agent-reach", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(data) != canonical {
			t.Fatalf("%s must match %s", path, canonicalPath)
		}
	}
}

func TestSeednoteResearchSkillsUseAgentReachOnlyForExternalXHSData(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "skills", "seednote-research", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "seednote-research", "SKILL.md"),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)
			for _, want := range []string{
				"Agent-Reach",
				"agent-reach doctor --json",
				`status == "ok"`,
				"active_backend",
				"channel_status=<ok|warn|off|error|missing>",
				"xhs-cli (xiaohongshu-cli)",
				"data_source=<agent-reach|task_topic|topic_pool|project_context>",
				"backend_command_family",
				"token_source",
				"missing_fields",
				"fallback_reason",
				"不能凭空构造",
				"只读",
				"不要在 Anban 内自行判断",
				"实际可用性、安装、登录和 fallback 顺序由 Agent-Reach 决定",
				"只作为 legacy/server/internal fallback，不进入新 seednote 研究主路径",
				"原创模式不得失败、不得写 `failure-state.json`",
				"missing_fields=external_hot_data",
				"无外部数据时不得套用 CES",
				"这条失败规则不适用于原创模式",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Agent-Reach research contract term %q", path, want)
				}
			}
			for _, forbidden := range []string{
				"opencli xiaohongshu publish",
				"opencli xiaohongshu delete-note",
				"opencli xiaohongshu follow",
				"opencli xiaohongshu unfollow",
				"opencli xiaohongshu like",
				"opencli xiaohongshu favorite",
				"mcporter call 'xiaohongshu.publish",
				"mcporter call 'xiaohongshu.delete",
				"mcporter call 'xiaohongshu.follow",
				"mcporter call 'xiaohongshu.like",
				"mcporter call 'xiaohongshu.collect",
				"xhs publish",
				"xhs delete",
				"xhs follow",
				"xhs like",
				"xhs favorite",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s must not include write-operation command %q", path, forbidden)
				}
			}
		})
	}
}

func TestSeednoteAgentsDoNotUseLegacyXHSMCPAsMainPath(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)
			for _, want := range []string{
				"seednote-research",
				"Agent-Reach",
				"原创模式不得因此写",
				"failure-state.json",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing seednote Agent-Reach handoff term %q", path, want)
				}
			}
			for _, forbidden := range []string{
				"list_project_topics(",
				"MCP `get_feed_detail",
				"先获取 xsec_token，再调用 MCP",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still uses legacy XHS MCP main path %q", path, forbidden)
				}
			}
		})
	}
}
