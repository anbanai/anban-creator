package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedance20SkillFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	var firstBody string
	for _, plugin := range []string{"claudecode", "codex", "openclaw"} {
		skillDir := filepath.Join(root, plugin, "skills", "seedance-20")
		skillPath := filepath.Join(skillDir, "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("%s seedance-20 SKILL.md missing: %v", plugin, err)
		}
		body := string(raw)
		requireValidSkillFrontmatter(t, plugin, "seedance-20", body)
		for _, want := range []string{
			"name: seedance-20",
			"Seedance 2.0 Skill OS",
			"Emily2040/seedance-2.0",
			"songguoxs/seedance-prompt-skill",
			"ZeroLu/awesome-seedance",
			"即梦",
			"Seedance",
			"种草",
			"带货",
			"获客",
			"推广",
			"register_video_reference",
			"analyze_video_reference",
			"get_project_profile",
			"validate_video_generation_params",
			"build_video_generation_plan",
			"create_video_generation_job",
			"query_video_generation_job",
			"download_video_generation_results",
			"compose_video_segments",
			"validate_video_delivery",
			"reference-anchors.md",
			"creative-brief.md",
			"video-understanding.json",
			"script.md",
			"shot-plan.md",
			"quality-review.md",
			"anchor-strategy.md",
			"visual-anchor-pack.md",
			"visual-anchors/",
			"references/methodology.md",
			"references/stability.md",
			"references/prompt-templates.md",
			"references/mcp-contract.md",
			"reference_role",
			"目标成片时长",
			"单次生成片段",
			"参考视频时长",
			`agent_name="videocreator"`,
			"project video profile",
			"agent_brief",
			"video.model_catalog",
			"estimated dynamic credits",
			"OSS-backed task file",
			"个人 IP",
			"高效段子",
			"主体一致性",
			"黄金三秒",
			"完整视频创作流程",
			"generate_image",
			"verify_with_vision",
			"最多自动 3 张",
			"Fast Lane",
			"Mode Gate",
			"Reference map",
			"Sequence Gate",
			"Project State Capsule",
			"Retake Protocol",
			"one-variable",
			"Director Formula",
			"anti-slop",
			"Delivery And QC",
			"首尾帧",
			"视频延长",
			"动态海报",
			"穿搭变装",
			"直播带货",
			"广告复刻",
			"真人短剧",
			"AI漫剧",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s SKILL.md missing %q", plugin, want)
			}
		}
		var firstRefs map[string]string
		if plugin == "claudecode" {
			firstRefs = map[string]string{}
		}
		for _, ref := range []string{"methodology.md", "stability.md", "prompt-templates.md", "mcp-contract.md", "sequence-workflow.md", "business-playbooks.md", "delivery-qc.md"} {
			refPath := filepath.Join(skillDir, "references", ref)
			refRaw, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("%s reference %s missing: %v", plugin, ref, err)
			}
			refBody := string(refRaw)
			if len(strings.TrimSpace(refBody)) < 300 {
				t.Fatalf("%s reference %s is too thin", plugin, ref)
			}
			if plugin == "claudecode" {
				firstRefs[ref] = refBody
			} else {
				firstRefPath := filepath.Join(root, "claudecode", "skills", "seedance-20", "references", ref)
				firstRefRaw, err := os.ReadFile(firstRefPath)
				if err != nil {
					t.Fatalf("claudecode reference %s missing: %v", ref, err)
				}
				if refBody != string(firstRefRaw) {
					t.Fatalf("seedance-20 reference %s differs between plugins", ref)
				}
			}
			switch ref {
			case "methodology.md":
				for _, want := range []string{"素材角色分配", "主体身份", "产品外观", "场景背景", "首帧", "尾帧", "运镜", "节奏", "音色", "BGM", "字体/文字风格", "个人 IP", "高效段子", "创作定位", "参考视频复刻", "视觉锚定图", "0 张", "1 张", "2 张", "3 张", "Director Formula", "one visible beat", "one primary camera move", "anti-slop", "fidelity budget"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			case "prompt-templates.md":
				for _, want := range []string{"reference_role", "audio_cue", "transition_or_effect", "主体 + 场景 + 动作 + 运镜 + 分时段 + 转场/特效 + 音频 + 风格", "产品 360", "产品拆解", "短剧式", "音乐卡点", "个人 IP 种草", "高效段子", "参考视频复刻", "anchor-strategy.md", "visual-anchor-pack.md", "subject-anchor-01.png", "Shot 1", "【时间轴】", "Final Prompt Template", "compact prompt"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			case "stability.md":
				for _, want := range []string{"引用模糊", "镜头指令冲突", "短时长内容过载", "素材无归属", "忽视音频", "复杂度与时长不匹配", "推镜头", "拉镜头", "摇镜", "跟拍", "环绕", "俯拍", "仰拍", "特写", "中景", "全景", "ref_image_path", "0.75", "主锚定图"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
			if ref == "sequence-workflow.md" {
				for _, want := range []string{"project-state.json", "Project State Capsule", "accepted observed state", "extension_depth", "completed beats", "reserved future beats", "prompt compiler", "Retake Protocol", "Keep", "Fix in post", "Edit, don't regenerate", "Re-roll", "Rewrite"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
			if ref == "business-playbooks.md" {
				for _, want := range []string{"追热点", "商业广告", "品牌宣传", "穿搭变装", "直播带货", "动态海报", "广告复刻", "真人短剧", "AI漫剧", "product_demo", "brand_promo", "ecommerce", "lead_gen"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
			if ref == "delivery-qc.md" {
				for _, want := range []string{"Picture", "Color", "Audio", "Text", "Continuity", "Rights", "Metadata", "Versioning", "Human QC", "quality-review.md"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
			if ref == "mcp-contract.md" {
				for _, want := range []string{"get_project_profile", "resolved_profile", "agent_brief", "video.model_catalog", "analyze_video_reference", "analysis_mode", "native_video", "model_routes.video_understanding", "require_native_video=true", "require_usage=true", "usage", "credits_charged", "validate_video_generation_params", "model_prices.video_generation", "billing.credits_per_cny", "estimated_credits", "pricing_breakdown", "task_file_id", "file_path", "ark_url", "OSS/CDN", "Provider raw URLs", "server-measured input video duration", "Do not trust agent-supplied input video duration", "visual_anchor_generation", "generate_image", "register_video_reference"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
		}
		for _, banned := range []string{
			"curl https://ark.cn-beijing.volces.com",
			"VOLCENGINE_ARK_API_KEY",
			"直接调用火山",
			"dreamina text2video",
			"dreamina image2video",
			"@图片1 作为首帧",
			"dreamina CLI",
			"credits: 3000",
			"Duration: 15 seconds.",
			"Ratio/resolution: `9:16` and `1080p`.",
			"get_project_video_profile",
			"0-3s / 3-10s / 10-13s / 13-15s",
			"4-5 shots for 15s by default",
		} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s SKILL.md should not mention %q", plugin, banned)
			}
		}
		if firstBody == "" {
			firstBody = body
		} else if body != firstBody {
			t.Fatalf("seedance-20 SKILL.md differs between plugins")
		}
	}
}

func TestDreaminaVideoSkillIsCompatibilityAlias(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	var firstBody string
	for _, plugin := range []string{"claudecode", "codex", "openclaw"} {
		skillPath := filepath.Join(root, plugin, "skills", "dreamina-video", "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("%s dreamina-video compatibility SKILL.md missing: %v", plugin, err)
		}
		body := string(raw)
		requireValidSkillFrontmatter(t, plugin, "dreamina-video", body)
		for _, want := range []string{
			"name: dreamina-video",
			"compatibility",
			"seedance-20",
			"Do not maintain separate Dreamina-only workflow logic here",
			"agent_name=\"videocreator\"",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s dreamina-video alias missing %q", plugin, want)
			}
		}
		if strings.Contains(body, "## Workflow") || strings.Contains(body, "完整视频创作流程") {
			t.Fatalf("%s dreamina-video alias should not keep a separate full workflow", plugin)
		}
		if firstBody == "" {
			firstBody = body
		} else if body != firstBody {
			t.Fatalf("dreamina-video compatibility SKILL.md differs between plugins")
		}
	}
}
