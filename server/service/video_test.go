package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/royalrick/anbanwriter/server/config"
)

func TestVideoServiceCreateMapsReferencesAndParameters(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/contents/generations/tasks" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "cgt-test-id"})
	}))
	defer srv.Close()

	watermark := false
	cfg := config.VideoAPIConfig{
		Key:     "test-key",
		BaseURL: srv.URL,
		Model:   "doubao-seedance-2-0-test",
		Timeout: time.Second,
		Defaults: config.VideoDefaultsConfig{
			Resolution: "1080p",
			Ratio:      "9:16",
			Duration:   15,
			Watermark:  &watermark,
		},
	}
	svc := NewVideoService(&cfg)

	seed := int64(42)
	cameraFixed := true
	result, err := svc.CreateTask(context.Background(), VideoGenerationRequest{
		Prompt:       "生成一条产品种草短视频",
		Purpose:      VideoPurposePlanting,
		Resolution:   "720p",
		Ratio:        "16:9",
		Duration:     10,
		Seed:         &seed,
		CameraFixed:  &cameraFixed,
		ServiceTier:  "default",
		SafetyID:     "user-1",
		ReferenceSet: []VideoReferenceInput{{Type: "image_url", URL: "https://example.com/a.png"}, {Type: "audio_url", URL: "https://example.com/a.mp3"}, {Type: "video_url", URL: "https://example.com/a.mp4"}, {Type: "text", Text: "产品是便携咖啡杯"}},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if result.VideoTaskID != "cgt-test-id" {
		t.Fatalf("VideoTaskID = %q", result.VideoTaskID)
	}
	if captured["model"] != "doubao-seedance-2-0-test" {
		t.Fatalf("model = %#v", captured["model"])
	}
	for _, key := range []string{"resolution", "ratio", "duration", "seed", "camera_fixed", "watermark", "service_tier", "safety_identifier"} {
		if _, ok := captured[key]; !ok {
			t.Fatalf("request missing %q: %#v", key, captured)
		}
	}
	content, ok := captured["content"].([]any)
	if !ok || len(content) != 5 {
		t.Fatalf("content = %#v", captured["content"])
	}
	if content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("first content item should be prompt text: %#v", content[0])
	}
	types := []string{}
	for _, item := range content {
		types = append(types, item.(map[string]any)["type"].(string))
	}
	gotTypes := strings.Join(types, ",")
	if gotTypes != "text,image_url,audio_url,video_url,text" {
		t.Fatalf("content types = %s", gotTypes)
	}
}

func TestVideoServiceCreateRejectsInaccessibleReferenceURL(t *testing.T) {
	cfg := config.VideoAPIConfig{Key: "test-key", BaseURL: "https://example.com", Model: "m", Defaults: config.VideoDefaultsConfig{Resolution: "1080p", Ratio: "9:16", Duration: 15}}
	svc := NewVideoService(&cfg)

	_, err := svc.CreateTask(context.Background(), VideoGenerationRequest{
		Prompt:       "生成视频",
		ReferenceSet: []VideoReferenceInput{{Type: "image_url", URL: "http://localhost:8080/files/a.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "publicly accessible HTTPS") {
		t.Fatalf("CreateTask() error = %v, want publicly accessible HTTPS hint", err)
	}
}

func TestVideoServiceQueryMapsStatusAndURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/contents/generations/tasks/cgt-test-id" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "cgt-test-id",
			"model":          "doubao-seedance-2-0-test",
			"status":         "succeeded",
			"content":        map[string]any{"video_url": "https://example.com/out.mp4", "last_frame_url": "https://example.com/last.png", "file_url": "https://example.com/file.mp4"},
			"resolution":     "1080p",
			"ratio":          "9:16",
			"duration":       15,
			"seed":           42,
			"revised_prompt": "优化后的提示词",
		})
	}))
	defer srv.Close()

	cfg := config.VideoAPIConfig{Key: "test-key", BaseURL: srv.URL, Model: "m", Timeout: time.Second}
	svc := NewVideoService(&cfg)
	result, err := svc.QueryTask(context.Background(), "cgt-test-id")
	if err != nil {
		t.Fatalf("QueryTask() error = %v", err)
	}
	if result.Status != "succeeded" || result.VideoURL != "https://example.com/out.mp4" || result.FileURL == "" || result.LastFrameURL == "" {
		t.Fatalf("result = %#v", result)
	}
	if result.Seed == nil || *result.Seed != 42 {
		t.Fatalf("seed = %#v", result.Seed)
	}
}
