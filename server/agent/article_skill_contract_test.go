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
			path: filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				`size="4:3"`,
				`size="1:1"`,
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"不依赖项目级/任务级 image ratio",
			},
		},
		{
			name: "codex article visual skill",
			path: filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				`size="4:3"`,
				`size="1:1"`,
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"不依赖项目级/任务级 image ratio",
			},
		},
		{
			name: "claudecode cover skill",
			path: filepath.Join(root, "plugins", "skills", "article-cover-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
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

func TestArticleSkillContracts_NoUnconditionalImageRequirements(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
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
				`size="4:3"`,
				`size="1:1"`,
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
				"所有配图使用 `ref_image_path=\"$DIR/cover.png\"` 保持风格一致",
				"将文件内容作为 `markdown` 参数传给 `convert_markdown",
				"`$DIR/images.json` 仅作为审计记录，`convert_markdown` 不会读取该文件",
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

func TestArticleSkillContracts_ContentOnlyDoesNotRequireCoverReference(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"封面关·配图开",
				"不传",
				"链到首张已生成图",
				"不存在的 `$DIR/cover.png`",
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
		filepath.Join(root, "plugins", "skills", "content-writing", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "content-writing", "SKILL.md"),
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
				"主路径",
				"convert_markdown` 只用于旧版 server 兼容降级",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-writing render contract term %q", path, term)
				}
			}
			for _, stale := range []string{
				"Markdown 转微信 HTML：调用 `convert_markdown` MCP 工具",
				"排版模块使用标准 Markdown 语法，由 `convert_markdown` 工具中的 LLM 自动渲染",
				"保存为 `$DIR/05-article.html`。",
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
		filepath.Join(root, "plugins", "skills", "content-writing", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "content-writing", "SKILL.md"),
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
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "agents", "article.toml"),
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
		filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-cover-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-cover-design", "SKILL.md"),
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
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "agents", "article.toml"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
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
		filepath.Join(root, "plugins", "skills", "article-cover-design", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article-cover-design", "SKILL.md"),
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
		filepath.Join(root, "plugins", "skills", "article-visual-design", "references", "cover.md"),
		filepath.Join(root, "plugins", "skills", "article-visual-design", "references", "cover.md"),
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
		filepath.Join(root, "plugins", "skills", "article-visual-design", "references", "content.md"),
		filepath.Join(root, "plugins", "skills", "article-visual-design", "references", "content.md"),
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
		filepath.Join(root, "plugins", "skills", "article-cover-design", "references", "examples.md"),
		filepath.Join(root, "plugins", "skills", "article-cover-design", "references", "examples.md"),
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

	for _, plugin := range []string{"plugins"} {
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
				"缺 `viral-audit.md` 不得发布",
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
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "agents", "article.toml"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "article", "SKILL.md"),
	} {
		t.Run(path, func(t *testing.T) {
			text := readArticleContractFile(t, path)
			for _, term := range []string{
				"cover_effectiveness_scorecard",
				"cover_quality_gate",
				"visual_quality_scorecard",
				"viral-audit.md",
				"缺 `viral-audit.md` 不得发布",
				"仅有旧的 6 维视觉评分全为 high 不得通过",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing hard publish gate term %q", path, term)
				}
			}
		})
	}

	viral := readArticleContractFile(t, filepath.Join(root, "plugins", "skills", "article-viral-strategy", "references", "viral-audit.md"))
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
		filepath.Join(root, "server", "mcp", "writing_tools.go"),
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
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "agents", "article.toml"),
	}
	for _, plugin := range []string{"plugins"} {
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

	for _, plugin := range []string{"plugins"} {
		content := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "content-writing", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" content-writing", content, append(requiredSections,
			"get_project_profile",
			"list_resources(category=\"writers\")",
			"get_resource(category=\"writers\"",
			"render_template",
			"convert_markdown",
			"$DIR/03-article.md",
		)...)

		topic := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "topic-research", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" topic-research", topic, append(requiredSections,
			"claim_topic",
			"list_project_titles",
			"list_drafts",
			"list_published_articles",
			"$DIR/01-research.md",
			"$DIR/02-outline.md",
		)...)

		seo := readArticleContractFile(t, filepath.Join(root, plugin, "skills", "seo-optimization", "SKILL.md"))
		assertArticleContractContainsAll(t, plugin+" seo-optimization", seo, append(requiredSections,
			"MCP is not used for SEO generation",
			"$DIR/seo-result.md",
			"CTR",
		)...)
	}
}

func TestArticleAgentsRouteCreativeGenerationToSkills(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, file := range []string{
		filepath.Join(root, "plugins", "agents", "article.md"),
		filepath.Join(root, "plugins", "agents", "article.toml"),
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
