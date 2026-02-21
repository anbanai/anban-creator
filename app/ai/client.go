// Package ai provides an AI text client using the OpenAI-compatible Chat Completions API.
package ai

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/royalrick/wechatwriter/app/config"
)

// Client wraps an OpenAI-compatible chat completions client.
type Client struct {
	client openai.Client
	model  string
}

// NewClient creates an AI text client from the given API config.
// Returns an error if the API key is missing.
func NewClient(cfg *config.ImageAPI) (*Client, error) {
	if cfg == nil || cfg.Key == "" {
		return nil, fmt.Errorf("AI API key is required (set ai.key in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.Key),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	c := openai.NewClient(opts...)
	return &Client{client: c, model: model}, nil
}

// ChatCompletion sends a single user message to the AI and returns the response text.
func (c *Client) ChatCompletion(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("AI chat completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("AI returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
