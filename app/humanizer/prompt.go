// Package humanizer provides AI writing trace removal functionality
package humanizer

import (
	"fmt"
	"strings"
)

// humanizerSystemPrompt 是 humanizer-zh 的核心指令
const humanizerSystemPrompt = `# Humanizer-zh: 去除 AI 写作痕迹

你是一位文字编辑，专门识别和去除 AI 生成文本的痕迹，使文字听起来更自然、更有人味。本指南基于维基百科的"AI 写作特征"页面，由 WikiProject AI Cleanup 维护。

## 你的任务

当收到需要人性化处理的文本时：

1. **识别 AI 模式** - 扫描下面列出的模式
2. **重写问题片段** - 用自然的替代方案替换 AI 痕迹
3. **保留含义** - 保持核心信息完整
4. **维持语调** - 匹配预期的语气（正式、随意、技术等）
5. **注入灵魂** - 不仅要去除不良模式，还要注入真实的个性

## 核心规则速查

在处理文本时，牢记这 5 条核心原则：

1. **删除填充短语** - 去除开场白和强调性拐杖词
2. **打破公式结构** - 避免二元对比、戏剧性分段、修辞性设置
3. **变化节奏** - 混合句子长度。两项优于三项。段落结尾要多样化
4. **信任读者** - 直接陈述事实，跳过软化、辩解和手把手引导
5. **删除金句** - 如果听起来像可引用的语句，重写它

## 需要检测和处理的模式

### 内容模式 (Content Patterns)

1. **过度强调意义** - 词汇如：标志着、见证了、是…的体现、凸显了、象征着、为…奠定基础、关键转折点
2. **过度强调知名度** - 反复强调媒体报道、专家观点而不提供具体来源
3. **以 -ing 结尾的肤浅分析** - 突出/强调/确保…、反映/象征…、为…做出贡献
4. **宣传和广告式语言** - 拥有、充满活力的、丰富的、深刻的、令人叹为观止的、必游之地
5. **模糊归因和含糊措辞** - 行业报告显示、观察者指出、专家认为（无具体来源）
6. **公式化的"挑战与未来展望"** - 尽管存在这些挑战、未来展望

### 语言和语法模式

7. **过度使用的"AI 词汇"** - 此外、与…保持一致、至关重要、深入探讨、强调、持久的、增强、培养、获得、突出、复杂、关键、格局、展示、织锦
8. **避免使用"是"** - 作为/代表/标志着/充当、拥有/设有/提供
9. **否定式排比** - "不仅…而且…"、"这不仅仅是…而是…"
10. **三段式法则过度使用** - 强行将想法分成三组以显得全面
11. **刻意换词（同义词循环）** - 主人公 → 主要角色 → 中心人物 → 英雄
12. **虚假范围** - "从 X 到 Y" 但 X 和 Y 不在同一尺度上

### 风格模式

13. **破折号过度使用** - 使用破折号（—）比人类更频繁
14. **粗体过度使用** - 机械地用粗体强调短语
15. **内联标题垂直列表** - 项目以粗体标题开头，后跟冒号
16. **表情符号** - 用表情符号装饰标题或项目符号

### 填充词和回避

17. **填充短语** - 为了实现这一目标、由于下雨的事实、在这个时间点
18. **过度限定** - 可以潜在地可能被认为该政策可能会
19. **通用积极结论** - 未来看起来光明、激动人心的时代即将到来

### 交流痕迹

20. **协作交流痕迹** - 希望这对您有帮助、当然！、您想要…
21. **知识截止日期免责声明** - 截至 [日期]、根据我最后的训练更新
22. **谄媚/卑躬屈膝的语气** - 过于积极、讨好的语言

## 个性与灵魂

避免 AI 模式只是工作的一半。好的写作需要有真实的人：

- **有观点** - 对事实做出反应，不只是中立报道
- **变化节奏** - 短促有力的句子 + 需要时间展开的长句
- **承认复杂性** - 真实的人有复杂的感受
- **适当使用"我"** - 第一人称是诚实的
- **允许一些混乱** - 完美的结构感觉像算法
- **对感受要具体** - 不是"令人担忧"，而是具体的场景描述

## 处理流程

1. 仔细阅读输入文本
2. 识别上述所有模式的实例
3. 重写每个有问题的部分
4. 确保修订后的文本：大声朗读时听起来自然、变化句子结构、使用具体细节
5. 呈现人性化版本
`

// getOutputFormatTemplate 输出格式模板
func getOutputFormatTemplate() string {
	return `

## 输出格式

请按以下格式输出：

# 人性化后的文本

[这里是重写后的文本，去除 AI 痕迹，注入人味]

# 修改说明

[简要说明做了哪些主要修改，如果需要详细对比请说明]

# 质量评分

| 维度 | 得分 | 说明 |
|------|------|------|
| 直接性 | x/10 | [说明] |
| 节奏 | x/10 | [说明] |
| 信任度 | x/10 | [说明] |
| 真实性 | x/10 | [说明] |
| 精炼度 | x/10 | [说明] |
| **总分** | **x/50** | [评级] |

## 最终要求

1. 输出完整的重写后文本
`
}

// BuildPrompt 构建给 Claude 的提示词
func BuildPrompt(req *HumanizeRequest) string {
	var prompt strings.Builder

	// 基础指令
	prompt.WriteString(humanizerSystemPrompt)

	// 添加处理强度说明
	prompt.WriteString("\n\n## 处理强度\n\n")
	switch req.Intensity {
	case IntensityGentle:
		prompt.WriteString("**温和模式**：只处理最明显、最确定的问题。保留大部分原文结构，只修改明显的 AI 痕迹如填充短语、过度强调的连接词等。适合已经比较自然的文本。")
	case IntensityAggressive:
		prompt.WriteString("**激进模式**：深度审查，最大化去除 AI 痕迹。大幅改写句式结构，注入更强的个性和观点。适合 AI 味很重的文本。")
	default:
		prompt.WriteString("**中等模式**（默认）：平衡处理。去除明显的 AI 痕迹，同时保留合理的表达。适合大多数场景。")
	}

	// 添加风格保护规则（风格优先原则）
	if req.PreserveStyle && req.OriginalStyle != "" {
		prompt.WriteString(fmt.Sprintf("\n\n## 风格保护\n\n"))
		prompt.WriteString(fmt.Sprintf("原文采用「%s」写作风格，请保留该风格的核心特征。\n\n", req.OriginalStyle))
		prompt.WriteString("**重要原则**：\n")
		prompt.WriteString("- 如果某种模式是该风格刻意为之（如使用破折号制造停顿），请保留\n")
		prompt.WriteString("- 只去除无意的 AI 痕迹\n")
		prompt.WriteString("- 保持风格的一致性\n")
	}

	// 添加聚焦模式
	if len(req.FocusOn) > 0 {
		prompt.WriteString("\n\n## 重点处理模式\n\n")
		prompt.WriteString("请重点关注以下类型的模式：\n")
		for _, p := range req.FocusOn {
			switch p {
			case PatternContent:
				prompt.WriteString("- **内容模式**：过度强调、夸大意义、宣传语言、模糊归因\n")
			case PatternLanguage:
				prompt.WriteString("- **语言语法**：AI 词汇、否定排比、三段式、同义词循环\n")
			case PatternStyle:
				prompt.WriteString("- **风格模式**：破折号过度、粗体滥用、表情符号\n")
			case PatternFiller:
				prompt.WriteString("- **填充词回避**：填充短语、过度限定、通用结论\n")
			case PatternCollaboration:
				prompt.WriteString("- **协作痕迹**：对话式填充、知识截止免责声明\n")
			}
		}
	}

	// 添加源信息提示
	if req.SourceHint != "" {
		prompt.WriteString(fmt.Sprintf("\n\n## 源信息\n\n文本来源: %s\n", req.SourceHint))
	}

	// 添加输出格式要求
	prompt.WriteString(getOutputFormatTemplate())

	if req.ShowChanges {
		prompt.WriteString("2. 提供修改说明和主要变更点\n")
	}
	if req.IncludeScore {
		prompt.WriteString("3. 按 5 维度给出质量评分\n")
	}
	prompt.WriteString("4. 只返回上述格式的内容，不需要其他解释\n")

	// 待处理文本
	prompt.WriteString(fmt.Sprintf("\n\n# 待处理文本\n\n%s", req.Content))

	return prompt.String()
}

// BuildAIRequest 构建 AI 转换请求（用于与 writer 模块兼容）
func BuildAIRequest(req *HumanizeRequest) *AIConvertRequest {
	return &AIConvertRequest{
		Content: req.Content,
		Prompt:  BuildPrompt(req),
		Settings: HumanizeSettings{
			Intensity:     req.Intensity,
			FocusOn:       req.FocusOn,
			PreserveStyle: req.PreserveStyle,
			OriginalStyle: req.OriginalStyle,
			ShowChanges:   req.ShowChanges,
			IncludeScore:  req.IncludeScore,
		},
	}
}

// ParseIntensity 从字符串解析强度
func ParseIntensity(s string) HumanizeIntensity {
	switch strings.ToLower(s) {
	case "gentle", "light", "温和", "轻度":
		return IntensityGentle
	case "aggressive", "heavy", "激进", "深度":
		return IntensityAggressive
	case "medium", "normal", "中等", "标准", "":
		return IntensityMedium
	default:
		return IntensityMedium
	}
}

// ParseFocusPattern 从字符串切片解析聚焦模式
func ParseFocusPattern(patterns []string) []FocusPattern {
	var result []FocusPattern
	patternMap := map[string]FocusPattern{
		"content":       PatternContent,
		"language":      PatternLanguage,
		"style":         PatternStyle,
		"filler":        PatternFiller,
		"collaboration": PatternCollaboration,
		// 中文别名
		"内容": PatternContent,
		"语言": PatternLanguage,
		"风格": PatternStyle,
		"填充": PatternFiller,
		"协作": PatternCollaboration,
	}

	for _, p := range patterns {
		lower := strings.ToLower(strings.TrimSpace(p))
		if fp, ok := patternMap[lower]; ok {
			result = append(result, fp)
		}
	}
	return result
}
