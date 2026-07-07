package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/app/converter"
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
// model, including the Kimi thinking-disabled tweak for speed.
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
	result, err := c.CompleteResult(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (c *openaiLLMClient) CompleteResult(ctx context.Context, systemPrompt, userPrompt string) (*LLMResult, error) {
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
		return nil, fmt.Errorf("llm completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)",
			c.model, resp.ID, resp.Model)
	}

	return &LLMResult{Text: resp.Choices[0].Message.Content, Model: responseModel(resp.Model, c.model), Usage: chatCompletionUsage(resp.Usage)}, nil
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

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// ConvertMarkdown converts Markdown to WeChat-compatible HTML using the
// converter package.
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
