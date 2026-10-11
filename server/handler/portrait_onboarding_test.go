package handler

import (
	"context"
	"errors"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type portraitStub struct {
	calls int
	user  string
	err   error
}

func (s *portraitStub) Available() bool       { return true }
func (s *portraitStub) SpeechAvailable() bool { return true }
func (s *portraitStub) Chat(_ context.Context, user string, _ []byte) (*service.PortraitCandidate, error) {
	s.calls++
	s.user = user
	return &service.PortraitCandidate{}, s.err
}
func (s *portraitStub) Transcribe(_ context.Context, user, _ string, _ []byte) (string, error) {
	s.calls++
	s.user = user
	return "识别结果", s.err
}
func TestPortraitAuthenticationLimitsAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, path, user, body string
		err                    error
		status                 int
		calls                  int
	}{
		{"unauthenticated chat", "chat", "", "{}", nil, 401, 0},
		{"unauthenticated voice", "transcribe", "", "audio", nil, 401, 0},
		{"oversized", "chat", "user", strings.Repeat("x", 180001), nil, 413, 0},
		{"chat", "chat", "user", "{}", nil, 200, 1},
		{"voice", "transcribe", "user", "audio", nil, 200, 1},
		{"quota", "chat", "user", "{}", service.ErrPortraitBusy, 429, 1},
		{"disabled", "chat", "user", "{}", service.ErrPortraitUnavailable, 503, 1},
		{"redact", "chat", "user", "{}", errors.New("secret internal failure"), 502, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &portraitStub{err: tc.err}
			h := NewPortraitOnboardingHandler(stub)
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				if tc.user != "" {
					c.Locals("user_id", tc.user)
				}
				return c.Next()
			})
			app.Post("/chat", h.Chat)
			app.Post("/transcribe", h.Transcribe)
			req := httptest.NewRequest("POST", "/"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.status || stub.calls != tc.calls || strings.Contains(string(body), "secret") {
				t.Fatalf("status %d calls %d body %s", resp.StatusCode, stub.calls, body)
			}
			if stub.calls > 0 && stub.user != tc.user {
				t.Fatal("lost owner")
			}
		})
	}
}
