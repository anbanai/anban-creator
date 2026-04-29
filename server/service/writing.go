package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/converter"
	"github.com/royalrick/anbanwriter/app/humanizer"
	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/repository"
)

// ---------------------------------------------------------------------------
// LLMClient interface
// ---------------------------------------------------------------------------

// LLMClient abstracts the LLM API call for writing operations.
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// ---------------------------------------------------------------------------
// OpenAI-compatible LLM client
// ---------------------------------------------------------------------------

// openaiLLMClient wraps the openai-go SDK for chat completions.
type openaiLLMClient struct {
	client openai.Client
	model  string
}

// NewOpenAILLMClient creates an LLMClient backed by an OpenAI-compatible API.
func NewOpenAILLMClient(baseURL, apiKey, modelName string) LLMClient {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &openaiLLMClient{client: client, model: modelName}
}

// Complete sends a system + user message to the configured model and returns the
// assistant's text content.
func (c *openaiLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []openai.ChatCompletionMessageParamUnion{}

	if systemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String(systemPrompt),
				},
			},
		})
	}

	messages = append(messages, openai.ChatCompletionMessageParamUnion{
		OfUser: &openai.ChatCompletionUserMessageParam{
			Content: openai.ChatCompletionUserMessageParamContentUnion{
				OfString: openai.String(userPrompt),
			},
		},
	})

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.model),
	})
	if err != nil {
		return "", fmt.Errorf("llm completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}

// ---------------------------------------------------------------------------
// WritingService
// ---------------------------------------------------------------------------

// WritingService wraps prompt assembly packages (writer, converter, humanizer)
// and calls an LLM for actual generation.
type WritingService struct {
	repo           repository.Repository
	llmClient      LLMClient
	modelConfigSvc *ModelConfigService
	logger         *zerolog.Logger
}

// NewWritingService creates a new WritingService.
func NewWritingService(repo repository.Repository, llmClient LLMClient, logger *zerolog.Logger) *WritingService {
	return &WritingService{
		repo:      repo,
		llmClient: llmClient,
		logger:    logger,
	}
}

// SetModelConfigService sets the model config service for per-user AI model overrides.
func (s *WritingService) SetModelConfigService(svc *ModelConfigService) {
	s.modelConfigSvc = svc
}

// getLLMClient returns the default or per-user LLM client for the given user.
func (s *WritingService) getLLMClient(ctx context.Context, userID string) LLMClient {
	if s.modelConfigSvc != nil {
		if baseURL, key, model, ok := s.modelConfigSvc.GetEffectiveWritingConfig(ctx, userID); ok {
			s.logger.Info().
				Str("user_id", userID).
				Str("endpoint", baseURL).
				Str("model", model).
				Msg("using user custom model for writing")
			return NewOpenAILLMClient(baseURL, key, model)
		}
	}
	return s.llmClient
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// WriteArticleResult contains the generated article and metadata.
type WriteArticleResult struct {
	Article   string   `json:"article"`
	Title     string   `json:"title,omitempty"`
	Quotes    []string `json:"quotes,omitempty"`
	Prompt    string   `json:"prompt,omitempty"`
	WordCount int      `json:"word_count"`
}

// ConvertMarkdownResult contains the converted HTML and image references.
type ConvertMarkdownResult struct {
	HTML   string        `json:"html"`
	Images []ImageRefDTO `json:"images,omitempty"`
}

// ImageRefDTO is a serialisable image reference extracted from markdown.
type ImageRefDTO struct {
	Index       int    `json:"index"`
	Original    string `json:"original"`
	Placeholder string `json:"placeholder,omitempty"`
}

// HumanizeArticleResult contains the humanized content and optional quality score.
type HumanizeArticleResult struct {
	Content string               `json:"content"`
	Score    *HumanizeScoreResult `json:"score,omitempty"`
}

// HumanizeScoreResult contains the 5-dimension quality score.
type HumanizeScoreResult struct {
	Total        int    `json:"total"`
	Directness   int    `json:"directness"`
	Rhythm       int    `json:"rhythm"`
	Trust        int    `json:"trust"`
	Authenticity int    `json:"authenticity"`
	Conciseness  int    `json:"conciseness"`
	Rating       string `json:"rating"`
}

// ResearchTopicsResult contains generated topic suggestions.
type ResearchTopicsResult struct {
	Topics []TopicSuggestion `json:"topics"`
}

// TopicSuggestion represents a single topic suggestion from the LLM.
type TopicSuggestion struct {
	Topic      string `json:"topic"`
	Angle      string `json:"angle"`
	ViralScore int    `json:"viral_score"`
	Reason     string `json:"reason"`
	Keywords   string `json:"keywords"`
	Template   string `json:"template"`
}

// OptimizeSEOResult contains SEO optimization output.
type OptimizeSEOResult struct {
	OptimizedTitle string `json:"optimized_title"`
	Keywords       string `json:"keywords"`
	Summary        string `json:"summary"`
}

// OutlineSection represents a single section in a generated outline.
type OutlineSection struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	KeyPoints  []string `json:"key_points"`
	Engagement string   `json:"engagement"`
}

// OutlineResult contains a generated article outline.
type OutlineResult struct {
	Title         string           `json:"title"`
	Subtitle      string           `json:"subtitle,omitempty"`
	Hook          string           `json:"hook"`
	Sections      []OutlineSection `json:"sections"`
	KeyPoints     []string         `json:"key_points"`
	CallToAction  string           `json:"call_to_action,omitempty"`
	ViralElements []string         `json:"viral_elements,omitempty"`
	SEOKeywords   []string         `json:"seo_keywords,omitempty"`
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// WriteArticle generates an article using the writer assistant and LLM.
func (s *WritingService) WriteArticle(
	ctx context.Context,
	userID, channelID, topic, inputType, articleType, length string,
) (*WriteArticleResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}

	assistant := writer.NewAssistant()

	req := &writer.WriteRequest{
		Input:       topic,
		InputType:   writer.InputType(inputType),
		ArticleType: writer.ArticleType(articleType),
		Length:      writer.Length(length),
		StyleName:   ch.Style,
	}

	result := assistant.Write(req)
	if !result.Success {
		return nil, fmt.Errorf("writer assembly: %s", result.Error)
	}
	if !result.IsAIRequest || result.Prompt == "" {
		// If the writer produced content without AI (unlikely but handle it).
		return &WriteArticleResult{
			Article:   result.Article,
			Title:     result.Title,
			Quotes:    result.Quotes,
			WordCount: utf8.RuneCountInString(result.Article),
		}, nil
	}

	article, err := s.getLLMClient(ctx, userID).Complete(ctx, "", result.Prompt)
	if err != nil {
		return nil, fmt.Errorf("llm generate article: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("channel_id", channelID).
		Int("word_count", utf8.RuneCountInString(article)).
		Msg("article generated")

	return &WriteArticleResult{
		Article:   article,
		Title:     result.Title,
		Quotes:    result.Quotes,
		Prompt:    result.Prompt,
		WordCount: utf8.RuneCountInString(article),
	}, nil
}

// ConvertMarkdown converts Markdown to WeChat-compatible HTML using the
// converter package and LLM.
func (s *WritingService) ConvertMarkdown(
	ctx context.Context,
	userID, channelID, markdown, theme string,
) (*ConvertMarkdownResult, error) {
	if markdown == "" {
		return nil, fmt.Errorf("markdown content is required")
	}

	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}

	// Use channel theme if not specified.
	if theme == "" {
		theme = ch.Theme
	}
	if theme == "" {
		theme = "default"
	}

	// Build the converter prompt via the writer package's converter.
	// We use a no-op logger since we do our own logging via zerolog.
	nopLog := zerolog.Nop()
	cvt := converter.NewConverter(&nopLog)

	convReq := &converter.ConvertRequest{
		Markdown: markdown,
		Theme:    theme,
	}

	// Convert triggers the AI path which returns the assembled prompt in the
	// error field as a sentinel value.
	convResult := cvt.Convert(convReq)

	prompt, images, ok := converter.GetAIRequestInfo(convResult)
	if !ok {
		// If not an AI request, the converter may have returned an actual error.
		if convResult.Error != "" {
			return nil, fmt.Errorf("convert error: %s", convResult.Error)
		}
		// Non-AI path (shouldn't happen in practice).
		return &ConvertMarkdownResult{
			HTML: convResult.HTML,
		}, nil
	}

	// Call LLM with the assembled prompt.
	html, err := s.getLLMClient(ctx, userID).Complete(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm convert markdown: %w", err)
	}
	imageDTOs := make([]ImageRefDTO, 0, len(images))
	for _, img := range images {
		imageDTOs = append(imageDTOs, ImageRefDTO{
			Index:       img.Index,
			Original:    img.Original,
			Placeholder: img.Placeholder,
		})
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("channel_id", channelID).
		Str("theme", theme).
		Int("image_count", len(imageDTOs)).
		Msg("markdown converted")

	return &ConvertMarkdownResult{
		HTML:   html,
		Images: imageDTOs,
	}, nil
}

// HumanizeArticle removes AI-generated writing traces from content.
// Uses the full 24-pattern humanizer prompt system for comprehensive AI trace removal.
func (s *WritingService) HumanizeArticle(
	ctx context.Context,
	userID, channelID, content, intensity string,
) (*HumanizeArticleResult, error) {
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	humanizeIntensity := humanizer.ParseIntensity(intensity)

	req := &humanizer.HumanizeRequest{
		Content:  content,
		Intensity: humanizeIntensity,
		IncludeScore: true,
	}

	prompt := humanizer.BuildPrompt(req)

	raw, err := s.getLLMClient(ctx, userID).Complete(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm humanize: %w", err)
	}

	// Parse the structured response using the humanizer package.
	h := humanizer.NewHumanizer()
	parsed := h.ParseAIResponse(raw, req)

	s.logger.Info().
		Str("user_id", userID).
		Str("intensity", intensity).
		Bool("scored", parsed.Score != nil).
		Msg("article humanized")

	result := &HumanizeArticleResult{
		Content: parsed.Content,
	}

	// Include quality score if available.
	if parsed.Score != nil {
		result.Score = &HumanizeScoreResult{
			Total:        parsed.Score.Total,
			Directness:   parsed.Score.Directness,
			Rhythm:       parsed.Score.Rhythm,
			Trust:        parsed.Score.Trust,
			Authenticity: parsed.Score.Authenticity,
			Conciseness:  parsed.Score.Conciseness,
			Rating:       parsed.Score.Rating(),
		}
	}

	return result, nil
}

// ResearchTopics generates topic suggestions based on a channel's positioning.
func (s *WritingService) ResearchTopics(
	ctx context.Context,
	userID, channelID string,
	keywords []string,
	domain string,
	count int,
) (*ResearchTopicsResult, error) {
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}

	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}

	// Fall back to channel keywords if none provided.
	if len(keywords) == 0 {
		for _, kw := range strings.Split(ch.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				keywords = append(keywords, kw)
			}
		}
	}

	positioning := ch.Positioning
	if positioning == "" {
		positioning = "未设定"
	}
	if domain != "" {
		positioning = domain + " - " + positioning
	}

	prompt := s.buildTopicsPrompt(positioning, keywords, count)

	raw, err := s.getLLMClient(ctx, userID).Complete(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm research topics: %w", err)
	}

	topics, err := s.parseTopicsResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse topics response: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Int("topic_count", len(topics)).
		Msg("topics researched")

	return &ResearchTopicsResult{
		Topics: topics,
	}, nil
}

// OptimizeSEO optimizes a title and keywords for search ranking.
func (s *WritingService) OptimizeSEO(
	ctx context.Context,
	userID, channelID, content, title string,
	keywords []string,
) (*OptimizeSEOResult, error) {
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	prompt := s.buildSEOPrompt(title, keywords, content)

	raw, err := s.getLLMClient(ctx, userID).Complete(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm seo optimize: %w", err)
	}

	result := s.parseSEOResponse(raw)

	s.logger.Info().
		Str("user_id", userID).
		Msg("seo optimization completed")

	return result, nil
}

// GenerateOutline generates a structured article outline using an LLM.
func (s *WritingService) GenerateOutline(
	ctx context.Context,
	userID, channelID, topic, template, style string,
) (*OutlineResult, error) {
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if template == "" {
		template = "authoritative"
	}

	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}

	// Use channel style if not overridden.
	if style == "" {
		style = ch.Style
	}
	if style == "" {
		style = "dan-koe"
	}

	// Extract keywords from the channel.
	var keywords []string
	for _, kw := range strings.Split(ch.Keywords, ",") {
		kw = strings.TrimSpace(kw)
		if kw != "" {
			keywords = append(keywords, kw)
		}
	}

	prompt := buildOutlinePrompt(topic, template, style, keywords)

	raw, err := s.getLLMClient(ctx, userID).Complete(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm generate outline: %w", err)
	}

	result, err := parseOutlineResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse outline response: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("channel_id", channelID).
		Str("topic", topic).
		Int("section_count", len(result.Sections)).
		Msg("outline generated")

	return result, nil
}

// ---------------------------------------------------------------------------
// Private prompt builders
// ---------------------------------------------------------------------------

// buildTopicsPrompt constructs a topic research prompt.
func (s *WritingService) buildTopicsPrompt(positioning string, keywords []string, count int) string {
	keywordStr := strings.Join(keywords, ", ")
	if keywordStr == "" {
		keywordStr = "通用"
	}

	return fmt.Sprintf(
		"你是一个专业的内容策划师。基于以下账号画像，生成 %d 个高传播潜力的话题建议。\n\n"+
			"## 账号画像\n"+
			"- 定位: %s\n"+
			"- 关键词: %s\n\n"+
			"## 要求\n"+
			"- 每个话题包含: topic, angle, viral_score(0-100), reason, keywords, template\n"+
			"- 输出 JSON 数组格式，不要包含 markdown 代码块标记\n"+
			"- 话题要有传播潜力，角度新颖\n"+
			"- viral_score 基于话题热度和目标受众匹配度评估",
		count, positioning, keywordStr,
	)
}

// buildSEOPrompt constructs an SEO optimization prompt.
func (s *WritingService) buildSEOPrompt(title string, keywords []string, content string) string {
	keywordStr := strings.Join(keywords, ", ")
	if keywordStr == "" {
		keywordStr = "未指定"
	}

	// Truncate content to keep the prompt manageable.
	displayContent := content
	if utf8.RuneCountInString(displayContent) > 3000 {
		displayContent = string([]rune(displayContent)[:3000]) + "\n...(已截断)"
	}

	return fmt.Sprintf(
		"你是一个 SEO 专家。请优化以下内容的标题和关键词，使其在搜索引擎中获得更好的排名。\n\n"+
			"## 原始标题\n%s\n\n"+
			"## 关键词\n%s\n\n"+
			"## 内容\n%s\n\n"+
			"## 输出要求\n"+
			"请以 JSON 格式输出（不要包含 markdown 代码块标记）：\n"+
			"{\n"+
			"  \"optimized_title\": \"优化后的标题\",\n"+
			"  \"keywords\": \"优化后的关键词（逗号分隔）\",\n"+
			"  \"summary\": \"优化后的内容摘要（50-100字）\"\n"+
			"}",
		title, keywordStr, displayContent,
	)
}

// ---------------------------------------------------------------------------
// Private response parsers
// ---------------------------------------------------------------------------

// parseTopicsResponse parses the LLM response as a JSON array of topic objects.
func (s *WritingService) parseTopicsResponse(raw string) ([]TopicSuggestion, error) {
	// Strip markdown code fences if present.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var topics []TopicSuggestion
	if err := json.Unmarshal([]byte(raw), &topics); err != nil {
		return nil, fmt.Errorf("unmarshal topics: %w", err)
	}

	return topics, nil
}

// parseSEOResponse parses the LLM response as an SEO optimization JSON object.
func (s *WritingService) parseSEOResponse(raw string) *OptimizeSEOResult {
	// Strip markdown code fences if present.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result OptimizeSEOResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		s.logger.Warn().Err(err).Str("raw", raw).Msg("failed to parse SEO response, returning raw as title")
		result.OptimizedTitle = raw
		return &result
	}

	return &result
}

// ---------------------------------------------------------------------------
// Outline helpers (package-level, not methods)
// ---------------------------------------------------------------------------

// buildOutlinePrompt constructs the outline generation prompt.
func buildOutlinePrompt(topic, templateType, style string, keywords []string) string {
	kwStr := strings.Join(keywords, "、")
	if kwStr == "" {
		kwStr = "（无）"
	}

	return fmt.Sprintf(`请为以下微信公众号文章生成一个详细的内容框架，以 JSON 格式输出。

话题: %s
模板类型: %s
写作风格: %s
关键词: %s

请输出严格的 JSON 格式，结构如下（不要包含任何额外文字）:
{
  "title": "吸引眼球的文章标题",
  "subtitle": "副标题或引导语",
  "hook": "开头钩子句（吸引读者继续阅读）",
  "sections": [
    {
      "title": "1. 节标题",
      "content": "该节的核心内容描述（2-3句话）",
      "key_points": ["要点一", "要点二", "要点三"],
      "engagement": "该节的互动引导语"
    }
  ],
  "key_points": ["全文核心要点一", "全文核心要点二", "全文核心要点三"],
  "call_to_action": "结尾行动号召",
  "viral_elements": ["传播元素一", "传播元素二"],
  "seo_keywords": ["SEO关键词一", "SEO关键词二", "SEO关键词三"]
}

要求：
- 标题要有冲击力和好奇心驱动
- 钩子要能在3秒内抓住读者注意力
- 各节内容要具体，紧扣关键词
- 结构要符合%s模板类型的逻辑`, topic, templateType, style, kwStr, templateType)
}

// parseOutlineResponse parses the LLM response as an outline JSON object.
func parseOutlineResponse(raw string) (*OutlineResult, error) {
	// Strip markdown code fences if present.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result OutlineResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("unmarshal outline: %w", err)
	}

	return &result, nil
}
