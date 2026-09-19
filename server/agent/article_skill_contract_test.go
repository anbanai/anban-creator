package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArticleSkillContracts_ImageControlsSizesAndTextPolicy(t *testing.T) {
	root := articleContractRepoRoot(t)
	cases := []struct {
		name     string
		path     string
		required []string
	}{
		{
			name: "claudecode article visual skill",
			path: filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"显式传 `aspect_ratio`",
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"智能适配",
			},
		},
		{
			name: "codex article visual skill",
			path: filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"显式传 `aspect_ratio`",
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"智能适配",
			},
		},
		{
			name: "claudecode cover skill",
			path: filepath.Join(root, "harness", "skills", "article-cover-design", "SKILL.md"),
			required: []string{
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"crop_image(",
				"受控文字策略",
				"article_image_mode",
				"content_only",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			text := string(data)
			for _, term := range tc.required {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing required article image contract term %q", tc.path, term)
				}
			}
		})
	}
}

func TestArticleIdentityFailuresAreNonRetryableAndRecoverable(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"execution_identity_required", "execution_identity_mismatch", "不可重试",
				"execution_identity_unavailable", "resume_from=image_generation",
				"保留", "不得包含令牌、密钥或完整环境变量", "继续生成核心 HTML",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing execution identity recovery contract %q", path, term)
				}
			}
		})
	}
}

func TestArticleClaudeAgentUsesDynamicLifecycleTasks(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "packs", "article", "agent.claude.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{"set_task_progress_plan", "2-7", "anban_stage_id", "TaskCreate", "TaskUpdate"} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing dynamic lifecycle contract %q", path, term)
				}
			}
			for _, stale := range []string{"anban_progress_stage", "research、writing、delivery"} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains fixed lifecycle instruction %q", path, stale)
				}
			}
		})
	}
}

func TestArticleCodexAgentUsesDynamicLifecycleTools(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "packs", "article", "agent.codex.toml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{"set_task_progress_plan", "update_task_progress", "2-7", "state=\"active\"", "state=\"complete\""} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing Codex dynamic lifecycle contract %q", path, term)
				}
			}
			for _, stale := range []string{"anban_progress_stage", "research、writing、delivery"} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s contains incompatible Codex progress instruction %q", path, stale)
				}
			}
		})
	}
}

func TestArticleAgentsBindMarketingScanToFinalMarkdown(t *testing.T) {
	root := articleContractRepoRoot(t)
	cases := []struct {
		path        string
		scannerPath string
	}{
		{filepath.Join(root, "harness", "packs", "article", "agent.claude.md"), "$CLAUDE_PLUGIN_ROOT/skills/content-writing/scripts/scan-article-marketing.mjs"},
		{filepath.Join(root, "harness", "packs", "article", "agent.codex.toml"), "__PLUGIN_ROOT__/skills/content-writing/scripts/scan-article-marketing.mjs"},
		{filepath.Join(root, "harness", "packs", "article", "agent.dsh.yml"), "$DSH_HOME/.agent-presets/article/skills/content-writing/scripts/scan-article-marketing.mjs"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			text := readArticleContractFile(t, tc.path)
			if strings.Count(text, tc.scannerPath) < 2 {
				t.Fatalf("%s must invoke its host scanner for initial and final scans", tc.path)
			}
			if strings.Count(text, "node ") < 2 {
				t.Fatalf("%s must execute its pure ESM marketing scanner with node", tc.path)
			}
			if strings.Count(text, "output/04-article-final.md output/marketing-scan.json --fix") != 1 {
				t.Fatalf("%s must allow exactly one deterministic auto-revision pass", tc.path)
			}
			for _, required := range []string{"最终 Markdown 每次修改后", "重新扫描并重新渲染", "content_hash"} {
				if !strings.Contains(text, required) {
					t.Fatalf("%s missing final scan contract %q", tc.path, required)
				}
			}
			lastScan := strings.LastIndex(text, tc.scannerPath)
			renderAfterScan := strings.Index(text[lastScan:], "render_template(")
			if lastScan < 0 || renderAfterScan < 0 {
				t.Fatalf("%s must run its authoritative scan before final rendering", tc.path)
			}
		})
	}

	skill := readArticleContractFile(t, filepath.Join(root, "harness", "skills", "content-writing", "SKILL.md"))
	for _, forbidden := range []string{"$CLAUDE_PLUGIN_ROOT", "$DSH_HOME"} {
		if strings.Contains(skill, forbidden) {
			t.Fatalf("content-writing skill contains host-specific path %q", forbidden)
		}
	}
}

func TestArticleSkillContracts_NoUnconditionalImageRequirements(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"图片运行控制前置",
				"article_image_mode",
				"仅在对应图片模式开启该产物时",
				"纯文字文章",
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"aspect_ratio=$EFFECTIVE_ASPECT_RATIO",
				"render_template",
				"正文配图开启时",
				"封面开关开启时",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing conditional image requirement term %q", path, term)
				}
			}
			for _, stale := range []string{
				"任一硬性项失败时停止发布：内容不贴题、缺少封面 `media_id`、章节缺图",
				"图文并茂**：每个 `##` 章节至少一张配图",
				"图片生成要求",
				"所有配图使用 `ref_image_path=\"output/cover.png\"` 保持风格一致",
				"将文件内容作为 `markdown` 参数传给 `convert_markdown",
				"`output/images.json` 仅作为审计记录，`convert_markdown` 不会读取该文件",
				"| 6 | `article-visual-design` | `cover.png`, `image-plan.md` |",
				"| 7 | `article-visual-design` | `images.json`, 更新 `04-article-final.md` |",
				"| 8 | `content-writing` | `05-article.html` |",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale unconditional image requirement %q", path, stale)
				}
			}
		})
	}
}

func TestArticleSkillUsesRuntimeOwnedOutputDirectory(t *testing.T) {
	path := filepath.Join(articleContractRepoRoot(t), "harness", "skills", "article", "SKILL.md")
	text := readArticleContractFile(t, path)
	for _, term := range []string{"runtime", "`output/`", "提供"} {
		if !strings.Contains(text, term) {
			t.Fatalf("%s missing runtime-owned output contract %q", path, term)
		}
	}
	if strings.Contains(text, "步骤 1 创建") {
		t.Fatalf("%s still assigns output directory creation to the workflow", path)
	}
}

func TestArticleSkillContracts_ContentOnlyDoesNotRequireCoverReference(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"封面关闭",
				"人物参考启用",
				"不传",
				"文本风格块",
				"不存在的 `output/cover.png`",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-only ref_image_path guard term %q", path, term)
				}
			}
		})
	}
}

func TestContentWritingSkillContracts_RenderTemplateMainPath(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "skills", "content-writing", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"render_template",
				"article_templates",
				"唯一主路径",
				"不得使用 `convert_markdown`",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-writing render contract term %q", path, term)
				}
			}
			for _, stale := range []string{
				"Markdown 转微信 HTML：调用 `convert_markdown` MCP 工具",
				"排版模块使用标准 Markdown 语法，由 `convert_markdown` 工具中的 LLM 自动渲染",
				"保存为 `output/05-article.html`。",
				"inspect_article",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale content-writing render path %q", path, stale)
				}
			}
		})
	}
}

func TestArticleSkillContracts_WechatPreflightLivesInSkills(t *testing.T) {
	root := articleContractRepoRoot(t)
	contentWritingPaths := []string{
		filepath.Join(root, "harness", "skills", "content-writing", "SKILL.md"),
	}
	for _, path := range contentWritingPaths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"公众号文章预检",
				"导流风险",
				"内容完整性",
				"标题摘要一致性",
				"审阅未通过",
				"自动调整",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing skill-owned preflight term %q", path, term)
				}
			}
			if strings.Contains(text, "inspect_article") {
				t.Fatalf("%s must not delegate article preflight to inspect_article", path)
			}
		})
	}

	articlePaths := []string{
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
	}
	for _, path := range articlePaths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"导流风险",
				"审阅未通过",
				"待调整",
				"自动",
				"content-quality-report.md",
				"final-review.md",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing article preflight workflow term %q", path, term)
				}
			}
		})
	}
}

func TestArticleSkillContracts_WechatVisualsForbidDiversionCues(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-cover-design", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"二维码",
				"联系方式",
				"外链 URL",
				"扫码提示",
				"加群",
				"加微信",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing anti-diversion visual term %q", path, term)
				}
			}
		})
	}
}

func TestArticleSkillContracts_WechatVisualQualityGate(t *testing.T) {
	root := articleContractRepoRoot(t)

	for _, path := range []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			if strings.Contains(path, "/agents/") {
				if !strings.Contains(text, "article-cover-design") {
					t.Fatal("Agent must delegate cover rules")
				}
				text += readArticleContractFile(t, filepath.Join(root, "harness/skills/article-cover-design/SKILL.md"))
			}
			for _, term := range []string{
				"cover_strategy",
				"target_reader",
				"reader_pain_or_job",
				"article_promise",
				"content_proof_points",
				"click_trigger",
				"cover_concept_candidates",
				"selected_cover_concept",
				"cover_hook",
				"visual_metaphor",
				"thumbnail_strategy",
				"anti_generic_constraints",
				"cover_effectiveness_scorecard",
				"generic_swap_test",
				"promise_proof_test",
				"audience_motivation_test",
				"cover_quality_gate",
				"cover-prompt.md",
				"final-review.md",
				"viral-audit.md",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing visual quality gate term %q", path, term)
				}
			}
		})
	}

	for _, path := range []string{
		filepath.Join(root, "harness", "skills", "article-cover-design", "SKILL.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"final_title",
				"digest_hook",
				"cover_strategy",
				"target_reader",
				"reader_pain_or_job",
				"article_promise",
				"content_proof_points",
				"click_trigger",
				"cover_concept_candidates",
				"selected_cover_concept",
				"generic_swap_test",
				"promise_proof_test",
				"audience_motivation_test",
				"cover_hook",
				"thumbnail_strategy",
				"anti_generic_constraints",
				"visual_quality_scorecard",
				"cover_effectiveness_scorecard",
				"information_scent_alignment",
				"audience_motivation",
				"content_specificity",
				"thumbnail_attention",
				"truthfulness_not_clickbait",
				"brand_style_fit",
				"visual_distinctiveness",
				"safe_zone_text_policy",
				"title_cover_digest_alignment",
				"thumbnail_readability",
				"contrast_focus",
				"specificity_not_generic",
				"series_distinctiveness",
				"hard_no_forbidden_cues",
				"通用养生水墨背景",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing cover quality scorecard term %q", path, term)
				}
			}
		})
	}

	for _, path := range []string{
		filepath.Join(root, "harness", "skills", "article-visual-design", "references", "cover.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			if !strings.Contains(text, "受控文字策略") {
				t.Fatalf("%s must point cover prompts at the controlled text policy", path)
			}
			if strings.Contains(text, "no text overlays") {
				t.Fatalf("%s must not retain stale unconditional no-text prompt language", path)
			}
		})
	}

	for _, path := range []string{
		filepath.Join(root, "harness", "skills", "article-visual-design", "references", "content.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"反同质化",
				"连续 3 张",
				"ref_image_path 只传递\"风格语言\"",
				"不得复刻封面主体",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content visual diversity term %q", path, term)
				}
			}
		})
	}

	for _, path := range []string{
		filepath.Join(root, "harness", "skills", "article-cover-design", "references", "examples.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"泛水墨模板感",
				"三伏贴重复",
				"绿豆汤主题不清",
				"黑白人像风格漂移",
				"企业做 AI 内容，别从买工具开始",
				"静水孤舟",
				"散落 AI 工具年卡",
				"空白选题库",
				"SOP 流程线",
				"阅读对折",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing screenshot-derived cover example %q", path, term)
				}
			}
		})
	}
}

func TestArticleSkillContracts_WechatCoverEffectivenessReference(t *testing.T) {
	root := articleContractRepoRoot(t)

	for _, plugin := range []string{"harness"} {
		t.Run(plugin, func(t *testing.T) {
			main := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "article-cover-design", "SKILL.md"))
			if !strings.Contains(main, "references/cover-effectiveness.md") {
				t.Fatalf("%s article-cover-design must link cover-effectiveness reference", plugin)
			}
			ref := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "article-cover-design", "references", "cover-effectiveness.md"))
			for _, term := range []string{
				"information scent",
				"ABCD",
				"准确但有吸引力",
				"明确受众",
				"cover_strategy",
				"cover_concept_candidates",
				"cover_effectiveness_scorecard",
				"generic_swap_test",
				"promise_proof_test",
				"audience_motivation_test",
				"仅有旧的 6 维视觉评分全为 high 不得通过",
				"缺 `viral-audit.md` 不得交付",
			} {
				if !strings.Contains(ref, term) {
					t.Fatalf("%s cover-effectiveness reference missing %q", plugin, term)
				}
			}
		})
	}
}

func TestArticleSkillContracts_WechatPublishGateRequiresViralAuditAndCoverEffectiveness(t *testing.T) {
	root := articleContractRepoRoot(t)

	for _, path := range []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"cover_effectiveness_scorecard",
				"cover_quality_gate",
				"visual_quality_scorecard",
				"viral-audit.md",
				"缺 `viral-audit.md` 不得标记 ready",
				"仅有旧的 6 维视觉评分全为 high 不得通过",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing hard publish gate term %q", path, term)
				}
			}
		})
	}

	viral := readArticleContractFile(t, filepath.Join(root, "harness", "skills", "article-viral-strategy", "references", "viral-audit.md"))
	for _, term := range []string{
		"cover_effectiveness_scorecard",
		"information_scent_alignment",
		"audience_motivation",
		"content_specificity",
		"不得只凭\"风格统一\"给高分",
	} {
		if !strings.Contains(viral, term) {
			t.Fatalf("article-viral-strategy viral-audit reference missing %q", term)
		}
	}
}

func TestArticleImageReferencesUseIndependentCapabilitiesAndAgentJudgment(t *testing.T) {
	root := articleContractRepoRoot(t)
	capabilityPaths := []string{
		filepath.Join(root, "harness", "skills", "article-visual-design", "references", "cover.md"),
		filepath.Join(root, "harness", "skills", "article-cover-design", "references", "examples.md"),
	}
	for _, path := range capabilityPaths {
		body := readArticleContractFile(t, path)
		for _, required := range []string{
			"generate_image",
			"analyze_image",
			"upload_image",
			"可见内容质量结论",
			"上传失败只重试上传，不重新生成",
			"failure-state.json",
			"不得请求用户中途协助",
		} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing independent article image reference term %q", path, required)
			}
		}
	}

	reviewPaths := append(capabilityPaths,
		filepath.Join(root, "harness", "skills", "article-viral-strategy", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-viral-strategy", "references", "viral-audit.md"),
	)
	for _, path := range reviewPaths {
		lower := strings.ToLower(readArticleContractFile(t, path))
		for _, forbidden := range []string{"vision", "verification", "vision-score.md", "请求用户协助"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s still contains stale article image review term %q", path, forbidden)
			}
		}
	}

	for _, path := range reviewPaths[len(capabilityPaths):] {
		body := readArticleContractFile(t, path)
		for _, required := range []string{"Agent 自主完成可见内容审查", "可见内容质量评分表"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing Agent-owned visible-content review term %q", path, required)
			}
		}
	}
}

func TestArticleAgentsSeparateCoreFailuresFromVisualWarnings(t *testing.T) {
	root := articleContractRepoRoot(t)
	coreCases := []struct {
		path       string
		errorCodes []string
	}{
		{filepath.Join(root, "harness", "agents", "article.md"), []string{"article_image_mode_missing", "article_mcp_call_failed"}},
		{filepath.Join(root, "harness", "agents", "article.toml"), []string{"article_image_mode_missing", "article_mcp_call_failed"}},
		{filepath.Join(root, "harness", "skills", "article", "SKILL.md"), []string{"article_image_mode_missing", "article_mcp_call_failed"}},
	}
	for _, tc := range coreCases {
		body := readArticleContractFile(t, tc.path)
		for _, required := range []string{
			"failure-state.json",
			`"status":"recoverable_failure"`,
			`"resume_from"`,
			"不得请求用户协助",
		} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing managed zero-interaction failure term %q", tc.path, required)
			}
		}
		for _, errorCode := range tc.errorCodes {
			if !strings.Contains(body, errorCode) {
				t.Fatalf("%s missing managed zero-interaction error code %q", tc.path, errorCode)
			}
		}
		for _, forbidden := range []string{
			"暂停流程请求用户协助",
			"暂停流程，请求用户协助",
			"仍失败则请求用户协助",
			"暂停流程，分析原因",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still contains mid-run user assistance path %q", tc.path, forbidden)
			}
		}
	}

	for _, path := range []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-cover-design", "SKILL.md"),
		filepath.Join(root, "harness", "skills", "article-visual-design", "SKILL.md"),
	} {
		body := readArticleContractFile(t, path)
		for _, required := range []string{"warning", "视觉失败不得阻止核心 Markdown 与 HTML 继续生成"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing visual warning contract %q", path, required)
			}
		}
	}
}

func articleConvertMarkdownFallbackScopes(body string) []string {
	var scopes []string
	for _, paragraph := range strings.Split(body, "\n\n") {
		lower := strings.ToLower(paragraph)
		if !strings.Contains(lower, "render_template") || !strings.Contains(lower, "convert_markdown") {
			continue
		}

		deniesFallback := false
		for _, denial := range []string{
			"不得改用 `convert_markdown`", "不得改用 convert_markdown",
			"禁止改用 `convert_markdown`", "禁止改用 convert_markdown",
			"不得用 `convert_markdown`", "不得用 convert_markdown",
			"禁止使用 `convert_markdown`", "禁止使用 convert_markdown",
		} {
			if strings.Contains(lower, strings.ToLower(denial)) {
				deniesFallback = true
				break
			}
		}
		if deniesFallback {
			continue
		}

		failureContext := false
		for _, marker := range []string{"不可用", "调用失败", "旧版 server", "旧服务器"} {
			if strings.Contains(lower, marker) {
				failureContext = true
				break
			}
		}
		fallbackAlternative := false
		for _, marker := range []string{"兼容降级", "降级路径", "备用路径", "替代路径", "fallback"} {
			if strings.Contains(lower, marker) {
				fallbackAlternative = true
				break
			}
		}
		if failureContext && fallbackAlternative {
			scopes = append(scopes, paragraph)
		}
	}
	return scopes
}

func TestArticleRenderTemplateFailureHasNoConvertMarkdownFallback(t *testing.T) {
	stale := "`render_template` 是主路径；`convert_markdown` 仅作旧版 server 兼容降级，不得作主渲染路径。"
	if scopes := articleConvertMarkdownFallbackScopes(stale); len(scopes) != 1 {
		t.Fatalf("stale render fallback must be detected, got %d scopes", len(scopes))
	}

	legitimateDistinction := "`render_template` 负责结构化渲染；`convert_markdown` 处理普通 Markdown 转换，二者职责不同。"
	if scopes := articleConvertMarkdownFallbackScopes(legitimateDistinction); len(scopes) != 0 {
		t.Fatalf("tool responsibility distinction must remain allowed, got %d scopes", len(scopes))
	}

	failClosed := "`render_template` 不可用或调用失败时写 failure-state；不得改用 `convert_markdown`。"
	if scopes := articleConvertMarkdownFallbackScopes(failClosed); len(scopes) != 0 {
		t.Fatalf("fail-closed render policy must remain allowed, got %d scopes", len(scopes))
	}

	root := articleContractRepoRoot(t)
	for _, path := range []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
	} {
		if scopes := articleConvertMarkdownFallbackScopes(readArticleContractFile(t, path)); len(scopes) != 0 {
			t.Fatalf("%s still permits convert_markdown fallback when render_template is unavailable", path)
		}
	}
}

func TestArticleManagedRuntimeFailsClosedOnProjectResolutionAndMCPCalls(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
		filepath.Join(root, "harness", "skills", "article", "SKILL.md"),
	}

	for _, path := range paths {
		body := readArticleContractFile(t, path)
		normalizedBody := strings.ReplaceAll(body, "`", "")
		for _, required := range []string{
			"托管上下文提供的项目 ID",
			"恰好一个归属当前用户的 Article 项目",
			"零个或多个归属当前用户的 Article 项目",
			"不得让用户选择",
			"article_project_resolution_failed",
			`"stage":"project_resolution"`,
			`"resume_from":"project_resolution"`,
			"必需 MCP 能力调用不可用或失败",
			"article_mcp_call_failed",
			"调用成功但缺少可选语义配置",
			"Agent 默认值",
			"只有这种成功响应中的可选字段缺失",
			"upload_image 调用失败时只重试上传",
			"article_image_upload_failed",
			"analyze_image 的传输或运行时失败",
			"记录为警告",
			"不得阻塞后续已规划的图片生成",
			"最终质量判断由 Agent 负责",
		} {
			if !strings.Contains(normalizedBody, required) {
				t.Fatalf("%s missing managed Article resolution term %q", path, required)
			}
		}

		for _, forbidden := range []string{
			"向用户展示所有可选项目",
			"项目选择是唯一例外",
			"如返回错误可忽略，用空列表继续",
			"如果 MCP 工具因配置问题失败，直接报告错误信息并继续流程",
			"自动重试 + 降级，非关键步骤跳过继续",
			"如果 MCP 工具不可用或调用失败，立即停止并报告错误",
			"`render_template` 不可用的旧版 server 上作为兼容降级路径",
			"render_template 不可用的旧版 server 上作为兼容降级路径",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still contains contradictory managed Article clause %q", path, forbidden)
			}
		}
	}
}

func TestArticleSkillContracts_InspectArticleMCPRemoved(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, path := range []string{
		filepath.Join(root, "server", "service", "inspect_article.go"),
		filepath.Join(root, "server", "service", "inspect_article_test.go"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s must be removed with inspect_article service preflight", path)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "server", "mcp", "content_render_tools.go"),
	} {
		text := readArticleContractFile(t, path)
		if strings.Contains(text, `"inspect_article"`) {
			t.Fatalf("%s must not register or expect inspect_article", path)
		}
	}
}

func TestArticleSkillsDoNotReferenceRemovedGenerationMCPTools(t *testing.T) {
	root := articleContractRepoRoot(t)
	removed := []string{"write_article", "research_topics", "optimize_seo", "generate_outline"}
	files := []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
	}
	for _, plugin := range []string{"harness"} {
		for _, skill := range []string{"content-writing", "topic-research", "seo-optimization"} {
			files = append(files, filepath.Join(root, plugin, "skills", skill, "SKILL.md"))
		}
	}

	for _, file := range files {
		body := readArticleContractFile(t, file)
		for _, name := range removed {
			if strings.Contains(body, name) {
				t.Fatalf("%s must not reference removed MCP tool %q", file, name)
			}
		}
	}
}

func TestArticleSkillsDeclareSkillOwnedGenerationAndServerDiscoveryTools(t *testing.T) {
	root := articleContractRepoRoot(t)
	requiredSections := []string{"## Intent Routing", "## Discovery First", "## Configuration Boundaries", "## Output Contract", "## Failure Handling"}

	for _, plugin := range []string{"harness"} {
		content := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "content-writing", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" content-writing", content, append(requiredSections,
			"get_project_profile",
			"list_resources(category=\"writers\")",
			"get_resource(category=\"writers\"",
			"render_template",
			"convert_markdown",
			"output/03-article.md",
		)...)

		topic := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "topic-research", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" topic-research", topic, append(requiredSections,
			"claim_topic",
			"list_project_titles",
			"list_drafts",
			"list_published_articles",
			"output/01-research.md",
			"output/02-outline.md",
		)...)

		seo := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "seo-optimization", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" seo-optimization", seo, append(requiredSections,
			"MCP is not used for SEO generation",
			"output/seo-result.md",
			"CTR",
		)...)
	}
}

func TestArticleAgentsRouteCreativeGenerationToSkills(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, file := range []string{
		filepath.Join(root, "harness", "agents", "article.md"),
		filepath.Join(root, "harness", "agents", "article.toml"),
	} {
		body := readArticleContractFile(t, file)
		assertArticleContractContainsAll(t, file, body,
			"topic-research",
			"content-writing",
			"seo-optimization",
			"Skills 内部完成",
			"不要调用或等待任何生成类 MCP 工具",
			"render_template",
		)
	}
}

func articleContractRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readArticleContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertArticleContractContainsAll(t *testing.T, label, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", label, want)
		}
	}
}
