package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/app/converter"
	"github.com/anbanai/anban-creator/app/writer"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
)

// ---------------------------------------------------------------------------
// LLMClient interface
// ---------------------------------------------------------------------------

// LLMClient abstracts the LLM API call for writing operations.
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	CompleteWithImage(ctx context.Context, systemPrompt, userPrompt, imageURL string) (string, error)
}

type LLMResult struct {
	Text  string
	Model string
	Usage srvconfig.TokenUsage
}

type usageImageLLMClient interface {
	CompleteWithImageResult(ctx context.Context, systemPrompt, userPrompt, imageURL string) (*LLMResult, error)
}

// streamingLLMClient is the OPTIONAL streaming capability of an LLMClient. Real
// OpenAI-compatible clients implement it; test fakes need not — callers detect
// support via a type assertion and fall back to the blocking Complete path. Used
// by write_article so a long generation streams progress to the client instead
// of blocking on a single deadline.
type streamingLLMClient interface {
	CompleteStream(ctx context.Context, systemPrompt, userPrompt string, onDelta func(string)) (string, error)
}

type videoURLLLMClient interface {
	CompleteWithVideoURL(ctx context.Context, systemPrompt, userPrompt, videoURL string) (string, error)
}

type usageVideoURLLLMClient interface {
	CompleteWithVideoURLResult(ctx context.Context, systemPrompt, userPrompt, videoURL string) (*LLMResult, error)
}

// ---------------------------------------------------------------------------
// OpenAI-compatible LLM client
// ---------------------------------------------------------------------------

// openaiLLMClient wraps the openai-go SDK for chat completions.
type openaiLLMClient struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

// NewOpenAILLMClient creates an LLMClient backed by an OpenAI-compatible API.
func NewOpenAILLMClient(baseURL, apiKey, modelName string, timeout time.Duration) LLMClient {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &openaiLLMClient{client: client, model: modelName, timeout: timeout}
}

// chatCompletionParams assembles the system+user chat params for this client's
// model. Shared by the blocking Complete and the streaming CompleteStream so the
// two stay in lockstep (incl. the Kimi thinking-disabled tweak for speed).
func (c *openaiLLMClient) chatCompletionParams(systemPrompt, userPrompt string) openai.ChatCompletionNewParams {
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
	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.model),
	}
	// Kimi K2.6/K2.5 thinking models default to thinking:enabled which causes
	// slow responses and 504 timeouts on non-reasoning tasks (e.g. HTML conversion).
	if isKimiThinkingModel(c.model) {
		params.SetExtraFields(map[string]any{
			"thinking": map[string]string{"type": "disabled"},
		})
	}
	return params
}

// Complete sends a system + user message to the configured model and returns the
// assistant's text content.
func (c *openaiLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.timeout > 0 {
		// Only add timeout if the context doesn't already have a deadline.
		// Callers (e.g. ConvertMarkdown) may set a longer convert-specific timeout.
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, c.timeout)
			defer cancel()
		}
	}

	resp, err := c.client.Chat.Completions.New(ctx, c.chatCompletionParams(systemPrompt, userPrompt))
	if err != nil {
		return "", fmt.Errorf("llm completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)",
			c.model, resp.ID, resp.Model)
	}

	return resp.Choices[0].Message.Content, nil
}

// CompleteStream sends a system + user message and returns the accumulated
// assistant text, invoking onDelta for each content chunk as it arrives. It is
// the streaming capability used by write_article: a slow generation streams
// progress to the client instead of blocking on a single deadline. If onDelta is
// nil it behaves like Complete minus the per-call timeout.
//
// Streaming intentionally does NOT apply c.timeout: the whole point is that a
// long generation stays alive chunk-by-chunk, bounded only by the caller's
// context (the MCP client/session budget) rather than being cut mid-generation
// at a fixed wall-clock deadline.
func (c *openaiLLMClient) CompleteStream(ctx context.Context, systemPrompt, userPrompt string, onDelta func(string)) (string, error) {
	stream := c.client.Chat.Completions.NewStreaming(ctx, c.chatCompletionParams(systemPrompt, userPrompt))
	// The stream owns the underlying HTTP response body; Close returns it to the
	// transport pool. Idempotent (no-op once the decoder is drained), so defer is
	// safe on every return path — without it every write_article leaks a conn.
	defer stream.Close()
	var b strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		for i := range chunk.Choices {
			delta := chunk.Choices[i].Delta.Content
			if delta == "" {
				continue
			}
			b.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return b.String(), fmt.Errorf("llm stream completion: %w", err)
	}
	return b.String(), nil
}

// CompleteWithImage sends a system + user message with an image to the configured
// model and returns the assistant's text content. The imageURL can be an HTTP URL
// or a base64 data URL (data:image/...;base64,...).
func (c *openaiLLMClient) CompleteWithImage(ctx context.Context, systemPrompt, userPrompt, imageURL string) (string, error) {
	result, err := c.CompleteWithImageResult(ctx, systemPrompt, userPrompt, imageURL)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (c *openaiLLMClient) CompleteWithImageResult(ctx context.Context, systemPrompt, userPrompt, imageURL string) (*LLMResult, error) {
	if c.timeout > 0 {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, c.timeout)
			defer cancel()
		}
	}

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
				OfArrayOfContentParts: []openai.ChatCompletionContentPartUnionParam{
					{
						OfImageURL: &openai.ChatCompletionContentPartImageParam{
							ImageURL: openai.ChatCompletionContentPartImageImageURLParam{
								URL:    imageURL,
								Detail: "low",
							},
						},
					},
					{
						OfText: &openai.ChatCompletionContentPartTextParam{
							Text: userPrompt,
						},
					},
				},
			},
		},
	})

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.model),
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm vision completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)",
			c.model, resp.ID, resp.Model)
	}

	return &LLMResult{Text: resp.Choices[0].Message.Content, Model: responseModel(resp.Model, c.model), Usage: chatCompletionUsage(resp.Usage)}, nil
}

// CompleteWithVideoURL sends a non-standard but common OpenAI-compatible
// video_url content part. Providers/gateways that support native video
// understanding consume the original OSS/CDN video URL; unsupported providers
// fail immediately.
func (c *openaiLLMClient) CompleteWithVideoURL(ctx context.Context, systemPrompt, userPrompt, videoURL string) (string, error) {
	result, err := c.CompleteWithVideoURLResult(ctx, systemPrompt, userPrompt, videoURL)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (c *openaiLLMClient) CompleteWithVideoURLResult(ctx context.Context, systemPrompt, userPrompt, videoURL string) (*LLMResult, error) {
	if c.timeout > 0 {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, c.timeout)
			defer cancel()
		}
	}

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

	videoPart := param.Override[openai.ChatCompletionContentPartUnionParam](map[string]any{
		"type":      "video_url",
		"video_url": map[string]any{"url": videoURL},
	})
	messages = append(messages, openai.ChatCompletionMessageParamUnion{
		OfUser: &openai.ChatCompletionUserMessageParam{
			Content: openai.ChatCompletionUserMessageParamContentUnion{
				OfArrayOfContentParts: []openai.ChatCompletionContentPartUnionParam{
					videoPart,
					{OfText: &openai.ChatCompletionContentPartTextParam{Text: userPrompt}},
				},
			},
		},
	})

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.model),
	}
	if isKimiThinkingModel(c.model) {
		params.SetExtraFields(map[string]any{
			"thinking": map[string]string{"type": "disabled"},
		})
	}
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm video completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)",
			c.model, resp.ID, resp.Model)
	}
	return &LLMResult{Text: resp.Choices[0].Message.Content, Model: responseModel(resp.Model, c.model), Usage: chatCompletionUsage(resp.Usage)}, nil
}

func responseModel(respModel, fallback string) string {
	if strings.TrimSpace(respModel) != "" {
		return respModel
	}
	return fallback
}

func chatCompletionUsage(usage openai.CompletionUsage) srvconfig.TokenUsage {
	return srvconfig.TokenUsage{
		InputTokens:       usage.PromptTokens,
		CachedInputTokens: usage.PromptTokensDetails.CachedTokens,
		OutputTokens:      usage.CompletionTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

// isKimiThinkingModel returns true for Kimi K2.6/K2.5 models that default to
// thinking:enabled. These models need thinking:disabled for fast, non-reasoning tasks.
func isKimiThinkingModel(model string) bool {
	switch model {
	case "kimi-k2.6", "kimi-k2.5", "kimi-k2-thinking", "kimi-k2-thinking-turbo",
		"kimi-k2-0905-preview", "kimi-k2-turbo-preview":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// WritingService
// ---------------------------------------------------------------------------

// WritingService wraps prompt assembly packages (writer, converter)
// and calls an LLM for actual generation.
type WritingService struct {
	repo                     repository.Repository
	llmClient                LLMClient
	llmTimeout               time.Duration
	modelConfigSvc           *ModelConfigService
	writersDir               string
	logger                   *zerolog.Logger
	visionClient             LLMClient // legacy test helper; use image/video understanding clients in production.
	imageUnderstandingClient LLMClient
	videoUnderstandingClient LLMClient
}

// NewWritingService creates a new WritingService.
func NewWritingService(repo repository.Repository, llmClient LLMClient, writersDir string, llmTimeout time.Duration, logger *zerolog.Logger) *WritingService {
	return &WritingService{
		repo:       repo,
		llmClient:  llmClient,
		llmTimeout: llmTimeout,
		writersDir: writersDir,
		logger:     logger,
	}
}

// SetModelConfigService sets the model config service for per-user AI model overrides.
func (s *WritingService) SetModelConfigService(svc *ModelConfigService) {
	s.modelConfigSvc = svc
}

// SetVisionClient sets a dedicated LLM client for vision/image analysis.
// If not called, AnalyzeImage falls back to the default writing LLM client.
func (s *WritingService) SetVisionClient(client LLMClient) {
	s.visionClient = client
	s.imageUnderstandingClient = client
	s.videoUnderstandingClient = client
}

func (s *WritingService) SetImageUnderstandingClient(client LLMClient) {
	s.imageUnderstandingClient = client
}

func (s *WritingService) SetVideoUnderstandingClient(client LLMClient) {
	s.videoUnderstandingClient = client
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
			return NewOpenAILLMClient(baseURL, key, model, s.llmTimeout)
		}
	}
	return s.llmClient
}

// AnalyzeImage sends an image to a vision LLM with the user's prompt and returns
// the analysis text. Uses the dedicated vision client if configured, otherwise
// falls back to the writing LLM client.
func (s *WritingService) AnalyzeImage(ctx context.Context, userID, imageSource, prompt string) (string, error) {
	result, err := s.AnalyzeImageDetailed(ctx, userID, imageSource, prompt)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (s *WritingService) AnalyzeImageDetailed(ctx context.Context, userID, imageSource, prompt string) (*LLMResult, error) {
	llm := s.imageUnderstandingClient
	if llm == nil {
		llm = s.visionClient
	}
	if llm == nil {
		return nil, fmt.Errorf("LLM service is not configured")
	}

	systemPrompt := "You are a precise visual analysis assistant. Describe exactly what you see in the image. Be specific and detailed."
	if usageLLM, ok := llm.(usageImageLLMClient); ok {
		result, err := usageLLM.CompleteWithImageResult(ctx, systemPrompt, prompt, imageSource)
		if err != nil {
			return nil, fmt.Errorf("image analysis: %w", err)
		}
		result.Text = strings.TrimSpace(result.Text)
		return result, nil
	}
	text, err := llm.CompleteWithImage(ctx, systemPrompt, prompt, imageSource)
	if err != nil {
		return nil, fmt.Errorf("image analysis: %w", err)
	}
	return &LLMResult{Text: strings.TrimSpace(text)}, nil
}

// AnalyzeVideoURL sends an OSS/CDN video URL to the configured video
// understanding client using a native video content part. If the configured
// client or provider does not support that shape, the call fails immediately.
func (s *WritingService) AnalyzeVideoURL(ctx context.Context, userID, videoURL, prompt string) (string, error) {
	result, err := s.AnalyzeVideoURLDetailed(ctx, userID, videoURL, prompt)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (s *WritingService) AnalyzeVideoURLDetailed(ctx context.Context, userID, videoURL, prompt string) (*LLMResult, error) {
	llm := s.videoUnderstandingClient
	if llm == nil {
		llm = s.visionClient
	}
	if llm == nil {
		return nil, fmt.Errorf("LLM service is not configured")
	}
	if usageVideoLLM, ok := llm.(usageVideoURLLLMClient); ok {
		systemPrompt := "You are a precise video analysis assistant. Describe exactly what you see across the whole video. Be specific and detailed."
		result, err := usageVideoLLM.CompleteWithVideoURLResult(ctx, systemPrompt, prompt, videoURL)
		if err != nil {
			return nil, fmt.Errorf("video analysis: %w", err)
		}
		result.Text = strings.TrimSpace(result.Text)
		return result, nil
	}
	videoLLM, ok := llm.(videoURLLLMClient)
	if !ok {
		return nil, fmt.Errorf("configured LLM client does not support native video URL input")
	}

	systemPrompt := "You are a precise video analysis assistant. Describe exactly what you see across the whole video. Be specific and detailed."
	text, err := videoLLM.CompleteWithVideoURL(ctx, systemPrompt, prompt, videoURL)
	if err != nil {
		return nil, fmt.Errorf("video analysis: %w", err)
	}
	return &LLMResult{Text: strings.TrimSpace(text)}, nil
}

// resolveWriter returns the 写作者 (writer resource key) for a writing
// operation, resolving two-layer via the single ResolveStyle primitive:
// task.Overrides.Writer wins, else project.Writer, with the article platform
// default applied when still empty. It NEVER reads the visual-style dimension —
// writer key and visual style are orthogonal and must not leak into each other.
func (s *WritingService) resolveWriter(ctx context.Context, taskID string, ch *model.Project) string {
	var task *model.Task
	if taskID != "" {
		if t, terr := s.repo.Tasks().FindByID(ctx, taskID); terr == nil {
			task = t
		}
	}
	return ResolveStyle(ch, task).Writer
}

// resolveEffectiveTheme returns the 排版样式 (theme resource key) for a render
// operation, resolving two-layer via ResolveStyle (task.Overrides.Theme ??
// project.Theme). Falls back to the platform default "autumn-warm" when the
// resolved theme is empty (ResolveStyle applies no theme default itself).
func (s *WritingService) resolveEffectiveTheme(ctx context.Context, taskID string, ch *model.Project) string {
	var task *model.Task
	if taskID != "" {
		if t, terr := s.repo.Tasks().FindByID(ctx, taskID); terr == nil {
			task = t
		}
	}
	theme := ResolveStyle(ch, task).Theme
	if theme == "" {
		theme = "autumn-warm"
	}
	return theme
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

// ResearchTopicsResult contains generated topic suggestions.
type ResearchTopicsResult struct {
	Topics []TopicSuggestion `json:"topics"`
}

// TopicSuggestion represents a single topic suggestion from the LLM.
type TopicSuggestion struct {
	Topic      string   `json:"topic"`
	Angle      string   `json:"angle"`
	ViralScore int      `json:"viral_score"`
	Reason     string   `json:"reason"`
	Keywords   []string `json:"keywords"`
	Template   string   `json:"template"`
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
// taskID optionally resolves the 写作风格 from the task (task > project); empty
// falls back to the project's writer.
func (s *WritingService) WriteArticle(
	ctx context.Context,
	userID, projectID, topic, inputType, articleType, length, taskID string,
) (*WriteArticleResult, error) {
	return s.WriteArticleStream(ctx, userID, projectID, topic, inputType, articleType, length, taskID, nil)
}

// WriteArticleStream is the streaming variant of WriteArticle: it invokes onDelta
// for each generated content chunk, so the MCP handler can relay real-time
// progress to the client. A nil onDelta makes it identical to WriteArticle. When
// the resolved LLM client does not support streaming, it falls back to the
// blocking Complete path (test fakes, future non-OpenAI clients).
func (s *WritingService) WriteArticleStream(
	ctx context.Context,
	userID, projectID, topic, inputType, articleType, length, taskID string,
	onDelta func(string),
) (*WriteArticleResult, error) {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	assistant := writer.NewAssistant()
	if s.writersDir != "" {
		assistant.SetWritersDir(s.writersDir)
	}
	styleName := s.resolveWriter(ctx, taskID, ch)

	req := &writer.WriteRequest{
		Input:       topic,
		InputType:   writer.InputType(inputType),
		ArticleType: writer.ArticleType(articleType),
		Length:      writer.Length(length),
		StyleName:   styleName,
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

	client := s.getLLMClient(ctx, userID)
	var article string
	if onDelta != nil {
		if sc, ok := client.(streamingLLMClient); ok {
			article, err = sc.CompleteStream(ctx, "", result.Prompt, onDelta)
		} else {
			article, err = client.Complete(ctx, "", result.Prompt)
		}
	} else {
		article, err = client.Complete(ctx, "", result.Prompt)
	}
	if err != nil {
		return nil, fmt.Errorf("llm generate article: %w", err)
	}

	// Truncation guard: a stream can end "successfully" (nil error, clean EOF)
	// yet emit only a partial article — e.g. a reasoning model emits thinking
	// tokens then stops, or the provider cuts a long generation mid-paragraph.
	// Without this check a half-finished article flows into humanizer →
	// converter → publish as if complete. Reject anything below half the
	// requested length's lower bound; an unrecognized length skips the guard.
	wordCount := utf8.RuneCountInString(article)
	if min := minExpectedArticleWords(length); min > 0 && wordCount < min/2 {
		return nil, fmt.Errorf("article appears truncated: %d runes generated, expected at least %d for length %q (stream ended early)", wordCount, min/2, length)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("project_id", projectID).
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

// minExpectedArticleWords returns the lower-bound word count for a requested
// article length (short/medium/long), used by WriteArticleStream's truncation
// guard. Bounds mirror app/writer.Length doc comments (short 800-1200,
// medium 1500-2500, long 3000-5000). Returns 0 for an unrecognized length so the
// guard is skipped (no reliable target).
func minExpectedArticleWords(length string) int {
	switch writer.Length(length) {
	case writer.LengthShort:
		return 800
	case writer.LengthMedium:
		return 1500
	case writer.LengthLong:
		return 3000
	default:
		return 0
	}
}

// ConvertMarkdown converts Markdown to WeChat-compatible HTML using the
// converter package and LLM.
func (s *WritingService) ConvertMarkdown(
	ctx context.Context,
	userID, projectID, markdown, theme, taskID string,
) (*ConvertMarkdownResult, error) {
	if markdown == "" {
		return nil, fmt.Errorf("markdown content is required")
	}

	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	// Resolve 排版样式: explicit caller theme wins, else the task's resolved
	// theme (task > project), else the platform default.
	if theme == "" {
		theme = s.resolveEffectiveTheme(ctx, taskID, ch)
	}

	// Deterministic render (goldmark + structured theme). No LLM, no timeout —
	// errors surface directly rather than silently degrading to hand-written HTML.
	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))

	convReq := &converter.ConvertRequest{
		Markdown: markdown,
		Theme:    theme,
	}

	convResult := cvt.Convert(convReq)
	if !convResult.Success {
		return nil, fmt.Errorf("convert error: %s", convResult.Error)
	}

	imageDTOs := make([]ImageRefDTO, 0, len(convResult.Images))
	for _, img := range convResult.Images {
		imageDTOs = append(imageDTOs, ImageRefDTO{
			Index:       img.Index,
			Original:    img.Original,
			Placeholder: img.Placeholder,
		})
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("project_id", projectID).
		Str("theme", theme).
		Int("image_count", len(imageDTOs)).
		Msg("markdown converted")

	return &ConvertMarkdownResult{
		HTML:   convResult.HTML,
		Images: imageDTOs,
	}, nil
}

// ResearchTopics generates topic suggestions based on a project's instructions positioning.
func (s *WritingService) ResearchTopics(
	ctx context.Context,
	userID, projectID string,
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

	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	// Fall back to project keywords if none provided.
	if len(keywords) == 0 {
		for _, kw := range strings.Split(ch.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				keywords = append(keywords, kw)
			}
		}
	}

	positioning := ch.Instructions
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
	userID, projectID, content, title string,
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
	userID, projectID, topic, template, style, taskID string,
) (*OutlineResult, error) {
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if template == "" {
		template = "authoritative"
	}

	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	// Resolve 写作风格: explicit caller style wins, else the task's resolved
	// writer (task > project). Never read ch.Style (that is the visual dimension).
	if style == "" {
		style = s.resolveWriter(ctx, taskID, ch)
	}

	// Extract keywords from the project.
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
		Str("project_id", projectID).
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
			"## 输出格式\n"+
			"严格输出 JSON 数组，不要包含 markdown 代码块标记，不要有尾随逗号。格式如下：\n"+
			"[\n"+
			"  {\n"+
			"    \"topic\": \"话题标题\",\n"+
			"    \"angle\": \"切入角度\",\n"+
			"    \"viral_score\": 85,\n"+
			"    \"reason\": \"推荐理由\",\n"+
			"    \"keywords\": [\"关键词1\", \"关键词2\"],\n"+
			"    \"template\": \"authoritative\"\n"+
			"  }\n"+
			"]\n\n"+
			"## 要求\n"+
			"- viral_score 范围 0-100，基于话题热度和目标受众匹配度评估\n"+
			"- 话题要有传播潜力，角度新颖\n"+
			"- keywords 为字符串数组，包含 3-5 个相关关键词",
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

// trailingCommaRe matches trailing commas before closing brackets/braces —
// a common JSON syntax error in LLM output.
var trailingCommaRe = regexp.MustCompile(`,\s*([\]})])`)

// extractJSONValue extracts a JSON value (array or object) from raw LLM output.
// It handles UTF-8 BOM, markdown code fences, and preamble text before the JSON.
// openBrace must be '[' or '{'. It returns the extracted JSON string.
func extractJSONValue(raw string, openBrace byte) (string, error) {
	closeBrace := byte(']')
	if openBrace == '{' {
		closeBrace = byte('}')
	}

	// Strip UTF-8 BOM
	raw = strings.TrimPrefix(raw, "\xEF\xBB\xBF")
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Find the opening bracket, skipping brackets inside quoted strings
	start := -1
	inStr := false
	esc := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if !inStr && ch == openBrace {
			start = i
			break
		}
	}
	if start == -1 {
		return "", fmt.Errorf("no JSON %c found in response", openBrace)
	}

	// Find matching closing bracket, respecting strings and nesting
	depth := 0
	end := -1
	inString := false
	escape := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == openBrace {
			depth++
		}
		if ch == closeBrace {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}

	if end == -1 {
		return extractJSONValueLenient(raw, openBrace)
	}

	extracted := raw[start:end]
	// Repair trailing commas (common LLM JSON issue)
	extracted = trailingCommaRe.ReplaceAllString(extracted, "$1")
	return extracted, nil
}

// repairMalformedDoubleQuote fixes the common LLM JSON malformation
// where an extra quote appears before a colon: "angle"":value -> "angle":"value".
var repairMalformedDoubleQuote = strings.NewReplacer(`"":`, `":`)

// repairMalformedJSON attempts to fix common LLM JSON issues.
func repairMalformedJSON(s string) string {
	return repairMalformedDoubleQuote.Replace(s)
}

// extractJSONValueLenient is a fallback when strict bracket matching fails
// due to malformed JSON from LLM output. It finds the first opening bracket
// and last closing bracket, repairs common malformations, then validates.
func extractJSONValueLenient(raw string, openBrace byte) (string, error) {
	closeBrace := byte(']')
	if openBrace == '{' {
		closeBrace = byte('}')
	}

	start := strings.IndexByte(raw, openBrace)
	if start == -1 {
		return "", fmt.Errorf("no JSON %c found in response", openBrace)
	}
	end := strings.LastIndexByte(raw, closeBrace)
	if end == -1 || end <= start {
		return "", fmt.Errorf("unmatched JSON %c bracket", openBrace)
	}

	candidate := raw[start : end+1]
	candidate = trailingCommaRe.ReplaceAllString(candidate, "$1")
	candidate = repairMalformedJSON(candidate)

	var js json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &js); err != nil {
		return "", fmt.Errorf("unmatched JSON %c bracket (lenient parse also failed: %v)", openBrace, err)
	}
	return candidate, nil
}

// extractJSONArray extracts a JSON array from raw LLM output.
func extractJSONArray(raw string) (string, error) {
	return extractJSONValue(raw, '[')
}

// extractJSONObject extracts a JSON object from raw LLM output.
func extractJSONObject(raw string) (string, error) {
	return extractJSONValue(raw, '{')
}

// parseTopicsResponse parses the LLM response as a JSON array of topic objects.
func (s *WritingService) parseTopicsResponse(raw string) ([]TopicSuggestion, error) {
	extracted, err := extractJSONArray(raw)
	if err != nil {
		s.logger.Warn().Str("raw", raw).Err(err).Msg("failed to extract topics JSON from LLM response")
		return nil, fmt.Errorf("extract topics JSON: %w", err)
	}

	var topics []TopicSuggestion
	if err := json.Unmarshal([]byte(extracted), &topics); err != nil {
		return nil, fmt.Errorf("unmarshal topics: %w", err)
	}

	return topics, nil
}

// parseSEOResponse parses the LLM response as an SEO optimization JSON object.
func (s *WritingService) parseSEOResponse(raw string) *OptimizeSEOResult {
	extracted, err := extractJSONObject(raw)
	if err != nil {
		s.logger.Warn().Err(err).Str("raw", raw).Msg("failed to extract SEO JSON from LLM response, returning raw as title")
		result := OptimizeSEOResult{OptimizedTitle: raw}
		return &result
	}

	var result OptimizeSEOResult
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		s.logger.Warn().Err(err).Str("extracted", extracted).Msg("failed to parse SEO JSON, returning raw as title")
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
	extracted, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract outline JSON: %w", err)
	}

	var result OutlineResult
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		return nil, fmt.Errorf("unmarshal outline: %w", err)
	}

	return &result, nil
}
