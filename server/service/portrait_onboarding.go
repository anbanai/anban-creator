package service

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

//go:embed portrait_prompt.txt
var portraitPrompt string

var ErrPortraitUnavailable = errors.New("访谈服务暂未开放，请联系管理员或使用项目页创建。")
var ErrPortraitBusy = errors.New("请求较多或已有请求正在处理，请稍后再试。")
var ErrPortraitProvider = errors.New("服务暂时无法完成处理，原有文字与画像已保留，请稍后重试。")
var ErrPortraitAudio = errors.New("录音格式不支持或超过 8 MB，请缩短后重试。")

type PortraitOnboardingService struct {
	cfg    config.OnboardingConfig
	redis  *redis.Client
	client *http.Client
}

func NewPortraitOnboardingService(cfg config.OnboardingConfig, rdb *redis.Client) *PortraitOnboardingService {
	return &PortraitOnboardingService{cfg: cfg, redis: rdb, client: &http.Client{Timeout: 100 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (s *PortraitOnboardingService) Available() bool {
	return s.cfg.Enabled && s.cfg.Chat.Configured() && s.redis != nil
}
func (s *PortraitOnboardingService) SpeechAvailable() bool {
	return s.Available() && s.cfg.Speech.Configured()
}

// Shared Redis budgets work across replicas; failure closes paid provider access.
// Counts attempts (including failed provider calls), not tokens or user credits.
var portraitBudget = redis.NewScript(`
local a=tonumber(redis.call('GET',KEYS[1]) or '0')
local b=tonumber(redis.call('GET',KEYS[2]) or '0')
if a>=tonumber(ARGV[1]) or b>=tonumber(ARGV[2]) then return 0 end
redis.call('INCR',KEYS[1]); redis.call('EXPIRE',KEYS[1],172800)
redis.call('INCR',KEYS[2]); redis.call('EXPIRE',KEYS[2],172800)
return 1`)
var portraitUnlock = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`)

func (s *PortraitOnboardingService) acquire(ctx context.Context, user string) (func(), error) {
	if !s.Available() || user == "" {
		return nil, ErrPortraitUnavailable
	}
	key := "onboarding:{portrait}:lock:" + user
	token := uuid.NewString()
	ok, err := s.redis.SetNX(ctx, key, token, 110*time.Second).Result()
	if err != nil {
		return nil, ErrPortraitUnavailable
	}
	if !ok {
		return nil, ErrPortraitBusy
	}
	release := func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = portraitUnlock.Run(c, s.redis, []string{key}, token).Err()
	}
	day := time.Now().UTC().Format("2006-01-02")
	allowed, err := portraitBudget.Run(ctx, s.redis, []string{"onboarding:{portrait}:day:" + day + ":" + user, "onboarding:{portrait}:day:" + day}, s.cfg.DailyUserLimit, s.cfg.DailyGlobalLimit).Int()
	if err != nil {
		release()
		return nil, ErrPortraitUnavailable
	}
	if allowed != 1 {
		release()
		return nil, ErrPortraitBusy
	}
	return release, nil
}
func (s *PortraitOnboardingService) call(ctx context.Context, p config.OnboardingProvider, path, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+path, body)
	if err != nil {
		return nil, ErrPortraitProvider
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ErrPortraitProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrPortraitProvider
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 160001))
	if err != nil || len(data) > 160000 {
		return nil, ErrPortraitProvider
	}
	return data, nil
}
func (s *PortraitOnboardingService) Chat(ctx context.Context, user string, raw []byte) (*PortraitCandidate, error) {
	r, err := ParsePortraitRequest(raw)
	if err != nil {
		return nil, err
	}
	release, err := s.acquire(ctx, user)
	if err != nil {
		return nil, err
	}
	defer release()
	conversation, _ := json.Marshal(map[string]any{"conversation": r.Messages})
	payload := map[string]any{"model": s.cfg.Chat.Model, "stream": false, "max_tokens": 5000, "temperature": 0.2, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": portraitPrompt}, {"role": "user", "content": string(conversation)}}}
	if strings.HasPrefix(s.cfg.Chat.Model, "deepseek") {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}
	body, _ := json.Marshal(payload)
	data, err := s.call(ctx, s.cfg.Chat, "/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Choices) != 1 || result.Choices[0].FinishReason != "stop" {
		return nil, ErrPortraitOutput
	}
	return ValidatePortraitCandidate([]byte(result.Choices[0].Message.Content), r.Messages)
}
func (s *PortraitOnboardingService) Transcribe(ctx context.Context, user, contentType string, audio []byte) (string, error) {
	if !s.SpeechAvailable() {
		return "", ErrPortraitUnavailable
	}
	media, _, err := mime.ParseMediaType(contentType)
	ext := map[string]string{"audio/webm": "webm", "audio/ogg": "ogg", "audio/mp4": "mp4", "audio/wav": "wav", "audio/x-wav": "wav"}[media]
	if err != nil || ext == "" || len(audio) == 0 || len(audio) > 8*1024*1024 {
		return "", ErrPortraitAudio
	}
	release, err := s.acquire(ctx, user)
	if err != nil {
		return "", err
	}
	defer release()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	file, err := w.CreateFormFile("file", "recording."+ext)
	if err != nil {
		return "", ErrPortraitProvider
	}
	if _, err = file.Write(audio); err != nil {
		return "", ErrPortraitProvider
	}
	for k, v := range map[string]string{"model": s.cfg.Speech.Model, "language": "zh", "response_format": "json"} {
		if err = w.WriteField(k, v); err != nil {
			return "", ErrPortraitProvider
		}
	}
	if err = w.Close(); err != nil {
		return "", ErrPortraitProvider
	}
	data, err := s.call(ctx, s.cfg.Speech, "/audio/transcriptions", w.FormDataContentType(), &buf)
	if err != nil {
		return "", err
	}
	var result struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &result) != nil || result.Text == nil || portraitLength(*result.Text) > 6000 {
		return "", ErrPortraitProvider
	}
	return strings.TrimSpace(*result.Text), nil
}
