package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/redis/go-redis/v9"
)

const portraitTestRequest = `{"messages":[{"id":"u-1","role":"user","text":"我叫小林，开花店，面向上班族，做公众号。"}]}`
const portraitTestCandidate = `{"reply":"想先聊哪一个选题？","name":"小林","summary":"花店主","facets":{"identity":{"text":"花店主","certainty":"stated","evidence":[{"messageId":"u-1","quote":"开花店"}]},"audience":null,"style":null,"platforms":null,"preferences":null,"experience":null},"creationIdea":null}`

func TestPortraitContractRejectsUntrustedEvidence(t *testing.T) {
	request, err := ParsePortraitRequest([]byte(portraitTestRequest))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", portraitTestCandidate, true},
		{"fabricated quote", strings.ReplaceAll(portraitTestCandidate, "开花店", "开茶店"), false},
		{"assistant quote", strings.ReplaceAll(portraitTestCandidate, "u-1", "a-1"), false},
		{"invented name", strings.ReplaceAll(portraitTestCandidate, "小林", "小王"), false},
		{"missing facet", strings.ReplaceAll(portraitTestCandidate, `"style":null,`, ""), false},
		{"missing nullable", strings.ReplaceAll(portraitTestCandidate, `"creationIdea":null`, `"extra":null`), false},
		{"unknown field", strings.ReplaceAll(portraitTestCandidate, `"certainty":"stated"`, `"certainty":"stated","secret":1`), false},
		{"trailing JSON", portraitTestCandidate + `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidatePortraitCandidate([]byte(tc.raw), request.Messages)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	for _, raw := range []string{`{"messages":[]}`, strings.ReplaceAll(portraitTestRequest, `"role":"user"`, `"role":"assistant"`), strings.ReplaceAll(portraitTestRequest, `"id":"u-1"`, `"id":"bad id"`), portraitTestRequest + `{}`} {
		if _, err := ParsePortraitRequest([]byte(raw)); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
}

func portraitTestService(t *testing.T, handler http.HandlerFunc) (*PortraitOnboardingService, *miniredis.Miniredis) {
	t.Helper()
	upstream := httptest.NewTLSServer(handler)
	t.Cleanup(upstream.Close)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	p := config.OnboardingProvider{BaseURL: upstream.URL, APIKey: "test-secret", Model: "test-model"}
	s := NewPortraitOnboardingService(config.OnboardingConfig{Enabled: true, Chat: p, Speech: p, DailyUserLimit: 3, DailyGlobalLimit: 10}, rdb)
	s.client = upstream.Client()
	s.client.Timeout = time.Second
	s.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return s, mr
}
func TestPortraitChatAndDistributedBudget(t *testing.T) {
	calls := 0
	s, mr := portraitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("provider request mismatch")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["max_tokens"] != float64(5000) {
			t.Error("missing output budget")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": portraitTestCandidate}}}})
	})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.Chat(ctx, "user", []byte(portraitTestRequest)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Chat(ctx, "user", []byte(portraitTestRequest)); !errors.Is(err, ErrPortraitBusy) {
		t.Fatal("quota bypass", err)
	}
	if calls != 3 {
		t.Fatal("unexpected paid call", calls)
	}
	// A second replica sees the same budget and in-flight lock.
	replica := NewPortraitOnboardingService(s.cfg, s.redis)
	replica.client = s.client
	if _, err := replica.Chat(ctx, "user", []byte(portraitTestRequest)); !errors.Is(err, ErrPortraitBusy) {
		t.Fatal(err)
	}
	if err := mr.Set("onboarding:{portrait}:lock:other", "in-flight"); err != nil {
		t.Fatal(err)
	}
	if _, err := replica.Chat(ctx, "other", []byte(portraitTestRequest)); !errors.Is(err, ErrPortraitBusy) {
		t.Fatal(err)
	}
	mr.Close()
	if _, err := s.Chat(ctx, "third", []byte(portraitTestRequest)); !errors.Is(err, ErrPortraitUnavailable) {
		t.Fatal("must fail closed", err)
	}
}
func TestPortraitSpeechMultipartAndNoLeak(t *testing.T) {
	calls := 0
	s, _ := portraitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/audio/transcriptions" {
			t.Error("wrong path")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "test-model" || r.FormValue("language") != "zh" {
			t.Error("missing fields")
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if h.Filename != "recording.webm" || string(data) != "audio" {
			t.Error("wrong audio")
		}
		_, _ = io.WriteString(w, `{"text":"你好，小林"}`)
	})
	text, err := s.Transcribe(context.Background(), "user", "audio/webm;codecs=opus", []byte("audio"))
	if err != nil || text != "你好，小林" {
		t.Fatal(text, err)
	}
	for _, tc := range []struct {
		mime string
		body []byte
	}{{"text/html", []byte("bad")}, {"audio/webm", nil}, {"audio/webm", make([]byte, 8*1024*1024+1)}} {
		if _, err := s.Transcribe(context.Background(), "user", tc.mime, tc.body); !errors.Is(err, ErrPortraitAudio) {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("invalid audio sent upstream")
	}
}
func TestPortraitProviderFailuresDoNotEscapeOrRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"provider error", 401, `secret diagnostic`}, {"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`}, {"oversized", 200, strings.Repeat("x", 160001)}, {"invalid", 200, `{broken`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s, _ := portraitTestService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			if _, err := s.Chat(context.Background(), "user", []byte(portraitTestRequest)); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("unexpected retry")
			}
		})
	}
}

func TestPortraitGlobalQuotaAndMalformedSpeech(t *testing.T) {
	calls := 0
	s, _ := portraitTestService(t, func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, `{}`) })
	s.cfg.DailyGlobalLimit = 1
	if _, err := s.Transcribe(context.Background(), "first", "audio/webm", []byte("audio")); !errors.Is(err, ErrPortraitProvider) {
		t.Fatal("must reject missing text", err)
	}
	if _, err := s.Transcribe(context.Background(), "second", "audio/webm", []byte("audio")); !errors.Is(err, ErrPortraitBusy) {
		t.Fatal("global quota bypass", err)
	}
	if calls != 1 {
		t.Fatal("unexpected provider calls", calls)
	}
}

func TestPortraitRequestReservesReplyBudget(t *testing.T) {
	messages := make([]PortraitMessage, 7)
	for i := range messages {
		messages[i] = PortraitMessage{ID: string(rune('a' + i)), Role: "user", Text: strings.Repeat("长", 6000)}
	}
	messages[6].Text = strings.Repeat("长", 2200)
	raw, _ := json.Marshal(PortraitRequest{Messages: messages})
	if _, err := ParsePortraitRequest(raw); err != nil {
		t.Fatal(err)
	}
	messages[6].Text += "长"
	raw, _ = json.Marshal(PortraitRequest{Messages: messages})
	if _, err := ParsePortraitRequest(raw); !errors.Is(err, ErrPortraitInput) {
		t.Fatal("did not reserve reply room", err)
	}
}
