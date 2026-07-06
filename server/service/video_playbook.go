package service

import "strings"

const (
	VideoProductionModeFastLane = "fast_lane"
	VideoProductionModeGuided   = "guided"
	VideoProductionModeSequence = "sequence"
	VideoProductionModeRemake   = "remake"
)

// VideoPlaybookSpec is a product-facing scenario template for Studio and agents.
// It intentionally stores Anban-owned guidance, not third-party prompt text.
type VideoPlaybookSpec struct {
	Key                    string   `json:"key"`
	Label                  string   `json:"label"`
	CreativeType           string   `json:"creative_type"`
	Purpose                string   `json:"purpose"`
	RequiredReferenceRoles []string `json:"required_reference_roles"`
	DefaultRatio           string   `json:"default_ratio"`
	PromptScaffold         string   `json:"prompt_scaffold"`
	QCFocus                []string `json:"qc_focus"`
	RiskNotes              []string `json:"risk_notes"`
	AgentBrief             string   `json:"agent_brief,omitempty"`
}

func DefaultVideoPlaybooks() []VideoPlaybookSpec {
	return []VideoPlaybookSpec{
		{
			Key:                    "trend_remix",
			Label:                  "追热点",
			CreativeType:           VideoCreativeTypeBrandPromo,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"camera movement", "rhythm", "subject identity"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "拆出热点结构、镜头节奏或视觉隐喻，用原创主体重建同款记忆点。",
			QCFocus:                []string{"原创替换", "节奏相似度", "权利风险"},
			RiskNotes:              []string{"不要复制真人、logo、台词、音乐或原场景所有权。"},
			AgentBrief:             "识别热点的可迁移结构，只继承节奏、构图或转场机制；主体、场景、台词、音乐和品牌资产必须原创替换。",
		},
		{
			Key:                    "commercial_ad",
			Label:                  "商业广告",
			CreativeType:           VideoCreativeTypeProductDemo,
			Purpose:                VideoPurposeEcommerce,
			RequiredReferenceRoles: []string{"product appearance", "scene background"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "围绕一个卖点，安排问题、可见证明、结果和 CTA。",
			QCFocus:                []string{"产品保真", "卖点可见", "CTA"},
			RiskNotes:              []string{"不要生成无法验证的功效、资质、价格或成分声明。"},
			AgentBrief:             "把用户 brief 中的一个真实卖点转成可见证明链路，优先保证产品外观、使用动作和 CTA 清晰。",
		},
		{
			Key:                    "brand_promo",
			Label:                  "品牌宣传",
			CreativeType:           VideoCreativeTypeBrandPromo,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"product appearance", "scene background"},
			DefaultRatio:           "16:9",
			PromptScaffold:         "只保留一个品牌记忆点，用场景和最后品牌/产品帧完成识别。",
			QCFocus:                []string{"品牌记忆", "调性一致", "结尾识别"},
			RiskNotes:              []string{"文字标语建议后期添加，避免模型生成乱码。"},
			AgentBrief:             "先定义品牌记忆点，再让镜头、场景、光线和结尾产品帧共同服务同一个识别目标。",
		},
		{
			Key:                    "outfit_transition",
			Label:                  "穿搭变装",
			CreativeType:           VideoCreativeTypeProductDemo,
			Purpose:                VideoPurposePlanting,
			RequiredReferenceRoles: []string{"product appearance", "rhythm", "subject identity"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "按节奏展示每套穿搭，交替半身动作和全身 reveal。",
			QCFocus:                []string{"服装完整度", "主体一致性", "卡点节奏"},
			RiskNotes:              []string{"服装过多时拆分成多段，避免细节丢失。"},
			AgentBrief:             "锁定人物身份和服装细节，把每次变化拆成短动作和清晰 reveal，必要时分段维持一致性。",
		},
		{
			Key:                    "live_selling",
			Label:                  "直播带货",
			CreativeType:           VideoCreativeTypeProductDemo,
			Purpose:                VideoPurposeEcommerce,
			RequiredReferenceRoles: []string{"product appearance", "action", "voice tone"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "黄金三秒开场、一个核心卖点、亲手展示或使用、特写证明、明确 CTA。",
			QCFocus:                []string{"产品保真", "口播可信", "动作证明", "CTA"},
			RiskNotes:              []string{"卖点必须来自用户 brief；不要自动编造功效、成分或优惠。"},
			AgentBrief:             "按直播间转化逻辑组织：开场抓手、真实卖点、手部展示、近景证明和收尾 CTA，所有主张来自用户 brief。",
		},
		{
			Key:                    "dynamic_poster",
			Label:                  "动态海报",
			CreativeType:           VideoCreativeTypeBrandPromo,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"first frame", "rhythm"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "固定机位，按海报元素顺序做层次显现，文字交给后期。",
			QCFocus:                []string{"版式稳定", "显现顺序", "文字风险"},
			RiskNotes:              []string{"若顺序精度要求高，先生成分镜锚定图。"},
			AgentBrief:             "把静态海报拆成元素显现顺序，保持机位和版式稳定，避免让模型直接生成密集文字。",
		},
		{
			Key:                    "ad_remake",
			Label:                  "广告复刻",
			CreativeType:           VideoCreativeTypeProductDemo,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"product appearance", "camera movement", "rhythm"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "参考视频只控制分镜、节奏或运镜，产品图控制产品身份。",
			QCFocus:                []string{"角色隔离", "产品替换", "权利清洁"},
			RiskNotes:              []string{"明确不要转移原人物、logo、环境、音乐或旁白。"},
			AgentBrief:             "将参考视频限定为镜头结构、节奏和运动参考，用新产品与新主体重建，不迁移原人物、场景、logo 或音频。",
		},
		{
			Key:                    "short_drama",
			Label:                  "真人短剧",
			CreativeType:           VideoCreativeTypePersonalIP,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"subject identity", "scene background"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "冲突开场、压力或揭示、反转、结尾钩子；台词短句。",
			QCFocus:                []string{"角色一致", "反转节奏", "口型/字幕风险"},
			RiskNotes:              []string{"复杂台词和字幕建议后期处理。"},
			AgentBrief:             "先写清人物关系和冲突，再把剧情压缩为单一动作链；复杂台词、字幕和口播留给后期。",
		},
		{
			Key:                    "anime_story",
			Label:                  "AI 漫剧",
			CreativeType:           VideoCreativeTypeCustom,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"style", "action", "subject identity"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "参考风格只定义媒介语法；每个镜头只放一个动作和一个特效载体。",
			QCFocus:                []string{"风格一致", "动作清晰", "连续性"},
			RiskNotes:              []string{"避免指定受保护 IP 角色或画面。"},
			AgentBrief:             "把风格参考限定为媒介语法和动效节奏，角色与剧情原创；每个镜头只承载一个清晰动作。",
		},
		{
			Key:                    "cinematic",
			Label:                  "电影质感",
			CreativeType:           VideoCreativeTypeCustom,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"scene background"},
			DefaultRatio:           "16:9",
			PromptScaffold:         "先命名场景意图，再让镜头、光线、表演和声音服务同一个意图。",
			QCFocus:                []string{"导演意图", "运镜动机", "光线来源"},
			RiskNotes:              []string{"少用空泛的 cinematic 形容词，优先写可见动作和镜头终点。"},
			AgentBrief:             "用导演 brief 固定情绪和镜头终点，描述可见动作、光线来源和运镜动机，减少空泛形容词。",
		},
		{
			Key:                    "ugc",
			Label:                  "UGC 真实感",
			CreativeType:           VideoCreativeTypePersonalIP,
			Purpose:                VideoPurposePlanting,
			RequiredReferenceRoles: []string{"subject identity", "voice tone"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "像真实创作者随手记录：一个生活痛点、自然动作、可见结果。",
			QCFocus:                []string{"自然可信", "人物稳定", "平台语气"},
			RiskNotes:              []string{"避免过度广告腔和过密字幕。"},
			AgentBrief:             "保持真实创作者语气，用生活场景、自然手持动作和可见结果表达一个种草理由。",
		},
		{
			Key:                    "animation",
			Label:                  "动画 / MG",
			CreativeType:           VideoCreativeTypeBrandPromo,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"style", "rhythm"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "用图形节奏解释一个概念或产品利益点，文字和 logo 由后期加。",
			QCFocus:                []string{"节奏", "信息层级", "文本风险"},
			RiskNotes:              []string{"精确排版和密集文字不要交给生成模型。"},
			AgentBrief:             "用形状、颜色、运动节奏解释一个信息层级；需要精确的文字、logo 和版式交由后期处理。",
		},
		{
			Key:                    "vfx",
			Label:                  "VFX / 实验视觉",
			CreativeType:           VideoCreativeTypeCustom,
			Purpose:                VideoPurposePromotion,
			RequiredReferenceRoles: []string{"first frame", "last frame", "action"},
			DefaultRatio:           "9:16",
			PromptScaffold:         "锁定起点和终点，只描述一个转化机制和一个视觉能量来源。",
			QCFocus:                []string{"起止状态", "物理连续", "主体不变"},
			RiskNotes:              []string{"复杂转场失败时拆成首尾帧或更短片段。"},
			AgentBrief:             "先锁定首尾状态和主体身份，再描述单一转化机制；失败风险高时拆短片段或改用首尾帧锚定。",
		},
	}
}

func FindVideoPlaybook(key string) (VideoPlaybookSpec, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return VideoPlaybookSpec{}, false
	}
	for _, item := range DefaultVideoPlaybooks() {
		if item.Key == key {
			return item, true
		}
	}
	return VideoPlaybookSpec{}, false
}

func MissingVideoReferenceRoles(refs []VideoReferenceInput, playbook VideoPlaybookSpec) []string {
	if len(playbook.RequiredReferenceRoles) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		role := strings.ToLower(strings.TrimSpace(ref.ReferenceRole))
		if role != "" {
			seen[role] = true
		}
	}
	missing := make([]string, 0, len(playbook.RequiredReferenceRoles))
	for _, role := range playbook.RequiredReferenceRoles {
		if !seen[strings.ToLower(role)] {
			missing = append(missing, role)
		}
	}
	return missing
}

func VideoProductionArtifactNames() []string {
	return []string{
		"creative-brief.md",
		"reference-anchors.md",
		"script.md",
		"shot-plan.md",
		"generation-plan.json",
		"project-state.json",
		"take-log.md",
		"quality-review.md",
		"delivery-manifest.json",
	}
}

func VideoRetakeActions() []string {
	return []string{"keep", "fix_in_post", "edit", "re_roll", "rewrite"}
}

func VideoNextActions() []string {
	return []string{"continue_editing", "generate_cover", "export_capcut_draft"}
}
