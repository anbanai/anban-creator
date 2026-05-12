package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/platform"
)

type fakeChannelLLM struct {
	response string
	err      error
	prompt   string
}

func (f *fakeChannelLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.prompt = userPrompt
	return f.response, f.err
}

func (f *fakeChannelLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func TestChannelFetchProfileAIAnalysisMergesFields(t *testing.T) {
	llm := &fakeChannelLLM{response: `{
		"positioning": "面向职场人的高效生活方式账号",
		"keywords": ["职场", "效率", "生活方式"],
		"style": "清爽明亮的实拍封面，搭配高对比标题字",
		"content_summary": "围绕职场效率和日常习惯做可执行分享"
	}`}
	h := NewChannelHandler(nil, testChannelLogger(t))
	h.SetLLMClient(llm, 0)
	profile := &platform.PlatformProfile{
		Name:        "测试账号",
		Positioning: "原始简介",
		RawData: map[string]any{
			"top_posts": []platform.RednotePost{
				{Title: "爆款选题", LikeCount: 12000, CommentCount: 200, EngagementScore: 12200},
			},
		},
	}

	h.enrichRednoteProfileWithAI(context.Background(), "user-1", profile)

	if profile.Positioning != "面向职场人的高效生活方式账号" {
		t.Fatalf("Positioning = %q", profile.Positioning)
	}
	if profile.Keywords != "职场, 效率, 生活方式" {
		t.Fatalf("Keywords = %q", profile.Keywords)
	}
	if profile.Style != "清爽明亮的实拍封面，搭配高对比标题字" {
		t.Fatalf("Style = %q", profile.Style)
	}
	if llm.prompt == "" || !containsAll(llm.prompt, "爆款选题", "原始简介") {
		t.Fatalf("prompt missing profile/post context: %q", llm.prompt)
	}
	analysis, ok := profile.RawData["analysis"].(rednoteProfileAnalysis)
	if !ok {
		t.Fatalf("RawData[analysis] type = %T", profile.RawData["analysis"])
	}
	if analysis.ContentSummary == "" {
		t.Fatal("analysis.ContentSummary is empty")
	}
}

func TestChannelFetchProfileAIAnalysisFallbackOnInvalidJSON(t *testing.T) {
	llm := &fakeChannelLLM{response: `not json`}
	h := NewChannelHandler(nil, testChannelLogger(t))
	h.SetLLMClient(llm, 0)
	profile := &platform.PlatformProfile{
		Name:        "测试账号",
		Positioning: "原始简介",
		RawData:     map[string]any{},
	}

	h.enrichRednoteProfileWithAI(context.Background(), "user-1", profile)

	if profile.Positioning != "原始简介" {
		t.Fatalf("Positioning = %q, want 原始简介", profile.Positioning)
	}
	if profile.Keywords != "" {
		t.Fatalf("Keywords = %q, want empty", profile.Keywords)
	}
}

func TestChannelFetchProfileRejectsNonRednoteAutoFetch(t *testing.T) {
	h := NewChannelHandler(nil, testChannelLogger(t))
	app := fiber.New()
	app.Post("/fetch", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/fetch", `{"platform":"article","profile_url":"https://mp.weixin.qq.com/test"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func httptestJSON(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func testChannelLogger(t *testing.T) *zerolog.Logger {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	return &logger
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
