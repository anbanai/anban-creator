package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

// LLMClient is the common text and image surface exposed by model clients.
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	CompleteWithImage(ctx context.Context, systemPrompt, userPrompt, imageURL string) (string, error)
}

// ResultLLMClient exposes provider usage for callers that must record cost.
type ResultLLMClient interface {
	CompleteResult(ctx context.Context, systemPrompt, userPrompt string) (*LLMResult, error)
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

type openaiLLMClient struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

// NewOpenAILLMClient creates a model client backed by an OpenAI-compatible API.
func NewOpenAILLMClient(baseURL, apiKey, modelName string, timeout time.Duration) LLMClient {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &openaiLLMClient{client: client, model: modelName, timeout: timeout}
}

func (c *openaiLLMClient) chatCompletionParams(systemPrompt, userPrompt string) openai.ChatCompletionNewParams {
	messages := []openai.ChatCompletionMessageParamUnion{}
	if systemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{OfString: openai.String(systemPrompt)},
			},
		})
	}
	messages = append(messages, openai.ChatCompletionMessageParamUnion{
		OfUser: &openai.ChatCompletionUserMessageParam{
			Content: openai.ChatCompletionUserMessageParamContentUnion{OfString: openai.String(userPrompt)},
		},
	})
	params := openai.ChatCompletionNewParams{Messages: messages, Model: shared.ChatModel(c.model)}
	if isKimiThinkingModel(c.model) {
		params.SetExtraFields(map[string]any{"thinking": map[string]string{"type": "disabled"}})
	}
	return params
}

func (c *openaiLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	result, err := c.CompleteResult(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (c *openaiLLMClient) CompleteResult(ctx context.Context, systemPrompt, userPrompt string) (*LLMResult, error) {
	if c.timeout > 0 {
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
		return nil, fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)", c.model, resp.ID, resp.Model)
	}
	return &LLMResult{Text: resp.Choices[0].Message.Content, Model: responseModel(resp.Model, c.model), Usage: chatCompletionUsage(resp.Usage)}, nil
}

func (c *openaiLLMClient) CompleteWithImage(ctx context.Context, systemPrompt, userPrompt, imageURL string) (string, error) {
	result, err := c.CompleteWithImageResult(ctx, systemPrompt, userPrompt, imageURL)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (c *openaiLLMClient) CompleteWithImageResult(ctx context.Context, systemPrompt, userPrompt, imageURL string) (*LLMResult, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	messages := []openai.ChatCompletionMessageParamUnion{}
	if systemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{OfString: openai.String(systemPrompt)},
			},
		})
	}
	messages = append(messages, openai.ChatCompletionMessageParamUnion{
		OfUser: &openai.ChatCompletionUserMessageParam{
			Content: openai.ChatCompletionUserMessageParamContentUnion{
				OfArrayOfContentParts: []openai.ChatCompletionContentPartUnionParam{
					{OfImageURL: &openai.ChatCompletionContentPartImageParam{ImageURL: openai.ChatCompletionContentPartImageImageURLParam{URL: imageURL, Detail: "low"}}},
					{OfText: &openai.ChatCompletionContentPartTextParam{Text: userPrompt}},
				},
			},
		},
	})
	var httpResponse *http.Response
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{Messages: messages, Model: shared.ChatModel(c.model)}, option.WithResponseInto(&httpResponse))
	if err != nil {
		return nil, newImageUnderstandingProviderError(err, httpResponse)
	}
	if len(resp.Choices) == 0 {
		return nil, newImageUnderstandingProviderError(errors.New("llm returned no choices"), httpResponse)
	}
	return &LLMResult{Text: resp.Choices[0].Message.Content, Model: responseModel(resp.Model, c.model), Usage: chatCompletionUsage(resp.Usage)}, nil
}

// Keep response bodies, request credentials and internal endpoints out of errors returned to agents.
// Unwrap preserves SDK API status and context cancellation for server callers.
type imageUnderstandingProviderError struct {
	Code        string
	StatusCode  int
	ContentType string
	Endpoint    string
	RequestID   string
	cause       error
}

func (e *imageUnderstandingProviderError) Error() string {
	return fmt.Sprintf("llm vision completion: %s (status=%d, content_type=%q, request_id=%q)", e.Code, e.StatusCode, e.ContentType, e.RequestID)
}

func (e *imageUnderstandingProviderError) Unwrap() error { return e.cause }

func newImageUnderstandingProviderError(err error, response *http.Response) *imageUnderstandingProviderError {
	result := &imageUnderstandingProviderError{Code: "provider_request_error", cause: err}
	if response != nil {
		result.StatusCode = response.StatusCode
		result.ContentType, _, _ = mime.ParseMediaType(response.Header.Get("Content-Type"))
		if response.Request != nil && response.Request.URL != nil {
			result.Endpoint = response.Request.URL.Host + response.Request.URL.EscapedPath()
		}
		result.RequestID = strings.TrimSpace(response.Header.Get("X-Request-ID"))
		if len(result.RequestID) > 256 || strings.ContainsAny(result.RequestID, "\r\n") {
			result.RequestID = ""
		}
		var apiError *openai.Error
		isJSON := result.ContentType == "application/json" || strings.HasSuffix(result.ContentType, "+json")
		if isJSON && errors.As(err, &apiError) {
			result.Code = "provider_api_error"
		} else {
			result.Code = "provider_protocol_error"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.Code = "provider_timeout"
	} else if errors.Is(err, context.Canceled) {
		result.Code = "provider_canceled"
	}
	return result
}

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
				Content: openai.ChatCompletionSystemMessageParamContentUnion{OfString: openai.String(systemPrompt)},
			},
		})
	}
	videoPart := param.Override[openai.ChatCompletionContentPartUnionParam](map[string]any{
		"type": "video_url", "video_url": map[string]any{"url": videoURL},
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
	params := openai.ChatCompletionNewParams{Messages: messages, Model: shared.ChatModel(c.model)}
	if isKimiThinkingModel(c.model) {
		params.SetExtraFields(map[string]any{"thinking": map[string]string{"type": "disabled"}})
	}
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm video completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices (model=%s, resp_id=%s, resp_model=%s)", c.model, resp.ID, resp.Model)
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
	uncachedPromptTokens := usage.PromptTokens - usage.PromptTokensDetails.CachedTokens
	if uncachedPromptTokens < 0 {
		uncachedPromptTokens = 0
	}
	return srvconfig.TokenUsage{
		InputTokens:       uncachedPromptTokens,
		CachedInputTokens: usage.PromptTokensDetails.CachedTokens,
		OutputTokens:      usage.CompletionTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func isKimiThinkingModel(model string) bool {
	switch model {
	case "kimi-k2.6", "kimi-k2.5", "kimi-k2-thinking", "kimi-k2-thinking-turbo",
		"kimi-k2-0905-preview", "kimi-k2-turbo-preview":
		return true
	default:
		return false
	}
}
