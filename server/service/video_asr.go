package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"resty.dev/v3"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	videoAudioKeyPrefix = "uploads/video-audio/"
	funASRDefaultModel  = "fun-asr"
)

// AudioASRClient is the direct adapter boundary for Aliyun Fun-ASR calls.
type AudioASRClient interface {
	Transcribe(ctx context.Context, req AudioASRTaskRequest) (*AudioASRTaskResult, error)
}

// VideoASRClient is kept as a compatibility alias for existing video-use callers.
type VideoASRClient = AudioASRClient

// AudioASRService coordinates storage and provider-side word-level ASR.
type AudioASRService struct {
	client AudioASRClient
	store  storage.Provider
	logger *zerolog.Logger
	mu     sync.RWMutex
	cache  map[string]*AudioASRTaskResult
}

// VideoASRService is kept as a compatibility alias for existing video-use callers.
type VideoASRService = AudioASRService

func NewAudioASRService(cfg config.FunASRConfig, store storage.Provider, logger *zerolog.Logger) (*AudioASRService, error) {
	client, err := NewFunASRHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	svc := NewAudioASRServiceWithClient(client, store)
	svc.logger = logger
	return svc, nil
}

func NewVideoASRService(cfg config.FunASRConfig, store storage.Provider, logger *zerolog.Logger) (*VideoASRService, error) {
	return NewAudioASRService(cfg, store, logger)
}

func NewAudioASRServiceWithClient(client AudioASRClient, store storage.Provider) *AudioASRService {
	return &AudioASRService{client: client, store: store, cache: map[string]*AudioASRTaskResult{}}
}

func NewVideoASRServiceWithClient(client VideoASRClient, store storage.Provider) *VideoASRService {
	return NewAudioASRServiceWithClient(client, store)
}

type AudioASRTaskRequest struct {
	FilePath     string `json:"file_path,omitempty"`
	AudioKey     string `json:"audio_key,omitempty"`
	AudioURL     string `json:"audio_url,omitempty"`
	Audio        io.Reader
	Filename     string `json:"-"`
	ContentType  string `json:"-"`
	LanguageHint string `json:"language_hint,omitempty"`
	SpeakerCount int    `json:"speaker_count,omitempty"`
}

type VideoASRTaskRequest = AudioASRTaskRequest

type AudioASRTaskResult struct {
	TaskID           string           `json:"task_id"`
	Status           string           `json:"status"`
	TranscriptionURL string           `json:"transcription_url,omitempty"`
	Transcript       *VideoTranscript `json:"transcript,omitempty"`
	Error            string           `json:"error,omitempty"`
}

type VideoASRTaskResult = AudioASRTaskResult

type VideoTranscript struct {
	Words    []VideoTranscriptWord      `json:"words"`
	Phrases  []VideoTranscriptPhrase    `json:"phrases,omitempty"`
	Metadata map[string]any             `json:"metadata,omitempty"`
	Raw      map[string]json.RawMessage `json:"-"`
}

type VideoTranscriptWord struct {
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	Start     float64 `json:"start,omitempty"`
	End       float64 `json:"end,omitempty"`
	SpeakerID string  `json:"speaker_id,omitempty"`
}

type VideoTranscriptPhrase struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	SpeakerID string  `json:"speaker_id,omitempty"`
}

func (s *AudioASRService) CreateTask(ctx context.Context, req AudioASRTaskRequest) (*AudioASRTaskResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("FunASR client is not configured")
	}
	if strings.TrimSpace(req.FilePath) != "" {
		return nil, fmt.Errorf("file_path is no longer supported; use prepare_file_upload and pass audio_key")
	}
	audioKey := strings.TrimSpace(req.AudioKey)
	audioURL := strings.TrimSpace(req.AudioURL)
	if audioKey == "" && audioURL == "" {
		return nil, fmt.Errorf("audio_key or audio_url is required")
	}
	if audioKey != "" {
		if !strings.HasPrefix(audioKey, videoAudioKeyPrefix) {
			return nil, fmt.Errorf("audio_key must be under %s", videoAudioKeyPrefix)
		}
		if s.store == nil {
			return nil, fmt.Errorf("storage provider is not available")
		}
		if s.store.Name() != "oss" {
			return nil, fmt.Errorf("audio_key requires OSS storage")
		}
		resolved, err := s.resolveAudioASRSourceURL(ctx, audioKey, "")
		if err != nil {
			return nil, fmt.Errorf("resolve audio object: %w", err)
		}
		req.AudioURL = resolved.URL
		req.Filename = resolved.Filename
		req.ContentType = resolved.ContentType
	} else {
		resolved, err := s.resolveAudioASRSourceURL(ctx, "", audioURL)
		if err != nil {
			return nil, fmt.Errorf("resolve audio URL: %w", err)
		}
		req.AudioURL = resolved.URL
		req.Filename = resolved.Filename
		req.ContentType = resolved.ContentType
	}
	result, err := s.client.Transcribe(ctx, req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("FunASR transcription returned no result")
	}
	if result.TaskID == "" {
		result.TaskID = "asr-" + uuid.NewString()
	}
	if result.Status == "" {
		result.Status = "SUCCEEDED"
	}
	s.mu.Lock()
	s.cache[result.TaskID] = result
	s.mu.Unlock()
	return result, nil
}

func (s *AudioASRService) resolveAudioASRSourceURL(ctx context.Context, key, rawURL string) (*MediaSource, error) {
	if strings.TrimSpace(key) != "" {
		if s.store == nil {
			return nil, fmt.Errorf("storage provider is not available")
		}
		url, err := s.store.DownloadURL(ctx, key, defaultMediaSourceTTL)
		if err != nil {
			return nil, fmt.Errorf("create signed audio URL: %w", err)
		}
		return &MediaSource{
			Key:         key,
			URL:         url,
			Filename:    filepath.Base(key),
			ContentType: audioContentType(strings.ToLower(filepath.Ext(key))),
		}, nil
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("audio URL is required")
	}
	if s.store != nil && s.store.IsOwnedURL(rawURL) {
		key, ok := storage.StorageKeyFromURL(rawURL)
		if !ok {
			return nil, fmt.Errorf("owned audio URL has no storage key")
		}
		return s.resolveAudioASRSourceURL(ctx, key, "")
	}
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("external media URL must be an HTTPS URL")
	}
	return &MediaSource{
		URL:         rawURL,
		Filename:    mediaSourceFilename(rawURL),
		ContentType: audioContentType(strings.ToLower(filepath.Ext(mediaSourceFilename(rawURL)))),
		External:    true,
	}, nil
}

func (s *AudioASRService) QueryTask(ctx context.Context, taskID string) (*AudioASRTaskResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("FunASR client is not configured")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	s.mu.RLock()
	result := s.cache[taskID]
	s.mu.RUnlock()
	if result == nil {
		return nil, fmt.Errorf("audio ASR task %q was not found; create_video_asr_task transcribes synchronously and only completed results can be queried", taskID)
	}
	return result, nil
}

// FunASRHTTPClient calls Aliyun Fun-ASR recorded speech recognition HTTP APIs.
type FunASRHTTPClient struct {
	baseURL      string
	model        string
	client       *resty.Client
	download     *resty.Client
	pollInterval time.Duration
	timeout      time.Duration
}

func NewFunASRHTTPClient(cfg config.FunASRConfig) (*FunASRHTTPClient, error) {
	if cfg.Empty() {
		return nil, nil
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("incomplete FunASR config: base_url and api_key are required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = funASRDefaultModel
	}
	client := resty.New().
		SetBaseURL(strings.TrimRight(cfg.BaseURL, "/")).
		SetAuthToken(strings.TrimSpace(cfg.APIKey)).
		SetHeader("Accept", "application/json").
		SetTimeout(timeout)
	download := resty.New().
		SetHeader("Accept", "application/json").
		SetTimeout(timeout)
	return &FunASRHTTPClient{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		model:        model,
		client:       client,
		download:     download,
		pollInterval: 2 * time.Second,
		timeout:      timeout,
	}, nil
}

func (c *FunASRHTTPClient) Transcribe(ctx context.Context, req AudioASRTaskRequest) (*AudioASRTaskResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("FunASR client is not configured")
	}
	audioURL := strings.TrimSpace(req.AudioURL)
	if audioURL == "" {
		return nil, fmt.Errorf("audio_url is required")
	}

	runCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	submit, err := c.submitTask(runCtx, audioURL)
	if err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(submit.Output.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("FunASR submit task returned no task id")
	}

	query, result, err := c.pollTask(runCtx, taskID)
	if err != nil {
		return nil, err
	}
	raw, err := c.downloadTranscript(runCtx, result.TranscriptionURL)
	if err != nil {
		return nil, err
	}
	transcript, err := NormalizeFunASRRecordedTranscript(raw)
	if err != nil {
		return nil, err
	}
	if transcript.Metadata == nil {
		transcript.Metadata = map[string]any{}
	}
	transcript.Metadata["provider"] = "aliyun-fun-asr-http"
	transcript.Metadata["model"] = c.model
	transcript.Metadata["task_id"] = taskID
	transcript.Metadata["file_url"] = result.FileURL
	transcript.Metadata["transcription_url"] = result.TranscriptionURL
	if req.AudioKey != "" {
		transcript.Metadata["audio_key"] = req.AudioKey
	}
	if req.AudioURL != "" {
		transcript.Metadata["audio_url"] = req.AudioURL
	}
	return &AudioASRTaskResult{
		TaskID:           taskID,
		Status:           query.Output.TaskStatus,
		TranscriptionURL: result.TranscriptionURL,
		Transcript:       transcript,
	}, nil
}

func (c *FunASRHTTPClient) submitTask(ctx context.Context, audioURL string) (*funASRTaskResponse, error) {
	body := map[string]any{
		"model": c.model,
		"input": map[string]any{
			"file_urls": []string{audioURL},
		},
		"parameters": map[string]any{
			"channel_id": []int{0},
		},
	}
	var out funASRTaskResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("X-DashScope-Async", "enable").
		SetBody(body).
		SetResult(&out).
		Post("/api/v1/services/audio/asr/transcription")
	if err != nil {
		return nil, fmt.Errorf("submit FunASR task: %w", err)
	}
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("submit FunASR task failed: status %d: %s", resp.StatusCode(), strings.TrimSpace(resp.String()))
	}
	if strings.TrimSpace(out.Output.TaskID) == "" {
		return nil, fmt.Errorf("FunASR submit task returned no task id")
	}
	return &out, nil
}

func (c *FunASRHTTPClient) pollTask(ctx context.Context, taskID string) (*funASRTaskResponse, funASRTaskResult, error) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		query, err := c.queryTask(ctx, taskID)
		if err != nil {
			return nil, funASRTaskResult{}, err
		}
		switch strings.ToUpper(strings.TrimSpace(query.Output.TaskStatus)) {
		case "SUCCEEDED":
			for _, result := range query.Output.Results {
				if strings.EqualFold(result.SubtaskStatus, "SUCCEEDED") && strings.TrimSpace(result.TranscriptionURL) != "" {
					return query, result, nil
				}
			}
			return nil, funASRTaskResult{}, fmt.Errorf("FunASR task %s succeeded but returned no successful transcription_url", taskID)
		case "FAILED", "CANCELED":
			return nil, funASRTaskResult{}, fmt.Errorf("FunASR task %s %s: %s", taskID, query.Output.TaskStatus, query.failureSummary())
		case "", "PENDING", "RUNNING":
		default:
			return nil, funASRTaskResult{}, fmt.Errorf("FunASR task %s returned unknown status %q", taskID, query.Output.TaskStatus)
		}

		select {
		case <-ctx.Done():
			return nil, funASRTaskResult{}, fmt.Errorf("poll FunASR task %s: %w", taskID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *FunASRHTTPClient) queryTask(ctx context.Context, taskID string) (*funASRTaskResponse, error) {
	var out funASRTaskResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&out).
		Get("/api/v1/tasks/" + taskID)
	if err != nil {
		return nil, fmt.Errorf("query FunASR task %s: %w", taskID, err)
	}
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("query FunASR task %s failed: status %d: %s", taskID, resp.StatusCode(), strings.TrimSpace(resp.String()))
	}
	return &out, nil
}

func (c *FunASRHTTPClient) downloadTranscript(ctx context.Context, rawURL string) ([]byte, error) {
	download := c.download
	if download == nil {
		download = resty.New().SetTimeout(c.timeout)
	}
	resp, err := download.R().
		SetContext(ctx).
		Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download FunASR transcript: %w", err)
	}
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("download FunASR transcript failed: status %d: %s", resp.StatusCode(), strings.TrimSpace(resp.String()))
	}
	return append([]byte(nil), resp.Bytes()...), nil
}

type funASRTaskResponse struct {
	RequestID string           `json:"request_id"`
	Output    funASRTaskOutput `json:"output"`
}

type funASRTaskOutput struct {
	TaskID     string             `json:"task_id"`
	TaskStatus string             `json:"task_status"`
	Results    []funASRTaskResult `json:"results"`
}

type funASRTaskResult struct {
	FileURL          string `json:"file_url"`
	TranscriptionURL string `json:"transcription_url"`
	SubtaskStatus    string `json:"subtask_status"`
	Code             string `json:"code"`
	Message          string `json:"message"`
}

func (r funASRTaskResponse) failureSummary() string {
	parts := make([]string, 0, len(r.Output.Results))
	for _, result := range r.Output.Results {
		if result.Code != "" || result.Message != "" || result.SubtaskStatus != "" {
			parts = append(parts, strings.TrimSpace(strings.Join([]string{result.SubtaskStatus, result.Code, result.Message}, " ")))
		}
	}
	if len(parts) == 0 {
		return "no result details"
	}
	return strings.Join(parts, "; ")
}

type funASRRecordedTranscript struct {
	FileURL     string                     `json:"file_url"`
	Properties  funASRRecordedProperties   `json:"properties"`
	Transcripts []funASRRecordedChannel    `json:"transcripts"`
	Raw         map[string]json.RawMessage `json:"-"`
}

type funASRRecordedProperties struct {
	OriginalDurationInMilliseconds int64 `json:"original_duration_in_milliseconds"`
	OriginalSamplingRate           int64 `json:"original_sampling_rate"`
}

type funASRRecordedChannel struct {
	ChannelID int                      `json:"channel_id"`
	Text      string                   `json:"text"`
	Sentences []funASRRecordedSentence `json:"sentences"`
}

type funASRRecordedSentence struct {
	BeginTime  int64                `json:"begin_time"`
	EndTime    int64                `json:"end_time"`
	Text       string               `json:"text"`
	SentenceID int64                `json:"sentence_id"`
	SpeakerID  any                  `json:"speaker_id"`
	Words      []funASRRecordedWord `json:"words"`
}

type funASRRecordedWord struct {
	BeginTime   int64  `json:"begin_time"`
	EndTime     int64  `json:"end_time"`
	Text        string `json:"text"`
	Punctuation string `json:"punctuation"`
}

func NormalizeFunASRRecordedTranscript(raw []byte) (*VideoTranscript, error) {
	var parsed funASRRecordedTranscript
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode FunASR transcript: %w", err)
	}
	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(raw, &rawMap)
	out := &VideoTranscript{
		Words:    []VideoTranscriptWord{},
		Phrases:  []VideoTranscriptPhrase{},
		Metadata: map[string]any{"provider": "aliyun-fun-asr-http"},
		Raw:      rawMap,
	}
	if parsed.FileURL != "" {
		out.Metadata["file_url"] = parsed.FileURL
	}
	if parsed.Properties.OriginalDurationInMilliseconds > 0 {
		out.Metadata["duration"] = millisToSeconds(float64(parsed.Properties.OriginalDurationInMilliseconds))
	}
	if parsed.Properties.OriginalSamplingRate > 0 {
		out.Metadata["sample_rate"] = parsed.Properties.OriginalSamplingRate
	}

	var prevEnd *float64
	for _, transcript := range parsed.Transcripts {
		for _, sentence := range transcript.Sentences {
			speaker := normalizeSpeakerID(anyASRString(sentence.SpeakerID))
			start := millisToSeconds(float64(sentence.BeginTime))
			end := millisToSeconds(float64(sentence.EndTime))
			text := strings.TrimSpace(sentence.Text)
			if text != "" {
				out.Phrases = append(out.Phrases, VideoTranscriptPhrase{
					Start:     start,
					End:       end,
					Text:      text,
					SpeakerID: speaker,
				})
			}
			if len(sentence.Words) == 0 {
				if text == "" {
					continue
				}
				if prevEnd != nil && start > *prevEnd {
					out.Words = append(out.Words, VideoTranscriptWord{Type: "spacing", Start: *prevEnd, End: start})
				}
				out.Words = append(out.Words, VideoTranscriptWord{
					Type:      "word",
					Text:      text,
					Start:     start,
					End:       end,
					SpeakerID: speaker,
				})
				prevEnd = &end
				continue
			}
			for _, word := range sentence.Words {
				wordText := strings.TrimSpace(word.Text)
				if wordText == "" {
					continue
				}
				wordText += word.Punctuation
				wordStart := millisToSeconds(float64(word.BeginTime))
				wordEnd := millisToSeconds(float64(word.EndTime))
				if prevEnd != nil && wordStart > *prevEnd {
					out.Words = append(out.Words, VideoTranscriptWord{Type: "spacing", Start: *prevEnd, End: wordStart})
				}
				out.Words = append(out.Words, VideoTranscriptWord{
					Type:      "word",
					Text:      wordText,
					Start:     wordStart,
					End:       wordEnd,
					SpeakerID: speaker,
				})
				prevEnd = &wordEnd
			}
		}
	}
	return out, nil
}

func PackVideoTranscripts(transcripts map[string]VideoTranscript, silenceThreshold float64) (string, error) {
	if silenceThreshold <= 0 {
		silenceThreshold = 0.5
	}
	if len(transcripts) == 0 {
		return "", fmt.Errorf("transcripts is required")
	}
	names := make([]string, 0, len(transcripts))
	for name := range transcripts {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Packed transcripts\n\n")
	fmt.Fprintf(&b, "Phrase-level, grouped on silences >= %.1fs or speaker change.\n", silenceThreshold)
	b.WriteString("Use `[start-end]` ranges to address cuts in the EDL.\n\n")
	for _, name := range names {
		phrases := groupVideoTranscriptPhrases(transcripts[name].Words, silenceThreshold)
		duration := 0.0
		if len(phrases) > 0 {
			duration = phrases[len(phrases)-1].End - phrases[0].Start
		}
		fmt.Fprintf(&b, "## %s  (duration: %s, %d phrases)\n", name, formatVideoDuration(duration), len(phrases))
		if len(phrases) == 0 {
			b.WriteString("  _no speech detected_\n\n")
			continue
		}
		for _, p := range phrases {
			spk := speakerTag(p.SpeakerID)
			fmt.Fprintf(&b, "  [%s-%s]%s %s\n", formatVideoTime(p.Start), formatVideoTime(p.End), spk, p.Text)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func groupVideoTranscriptPhrases(words []VideoTranscriptWord, silenceThreshold float64) []VideoTranscriptPhrase {
	var phrases []VideoTranscriptPhrase
	var current []VideoTranscriptWord
	var currentStart float64
	var currentSpeaker string
	var prevEnd *float64

	flush := func() {
		if len(current) == 0 {
			return
		}
		parts := make([]string, 0, len(current))
		for _, w := range current {
			if strings.TrimSpace(w.Text) != "" {
				parts = append(parts, strings.TrimSpace(w.Text))
			}
		}
		if len(parts) > 0 {
			text := strings.Join(parts, "")
			phrases = append(phrases, VideoTranscriptPhrase{
				Start:     currentStart,
				End:       current[len(current)-1].End,
				Text:      text,
				SpeakerID: currentSpeaker,
			})
		}
		current = nil
		currentSpeaker = ""
	}

	for _, w := range words {
		if w.Type == "spacing" {
			if w.End-w.Start >= silenceThreshold {
				flush()
			}
			continue
		}
		if w.Type != "" && w.Type != "word" && w.Type != "audio_event" {
			continue
		}
		if strings.TrimSpace(w.Text) == "" {
			continue
		}
		if currentSpeaker != "" && w.SpeakerID != "" && w.SpeakerID != currentSpeaker {
			flush()
		}
		if prevEnd != nil && w.Start-*prevEnd >= silenceThreshold {
			flush()
		}
		if len(current) == 0 {
			currentStart = w.Start
			currentSpeaker = w.SpeakerID
		}
		current = append(current, w)
		end := w.End
		prevEnd = &end
	}
	flush()
	return phrases
}

func millisToSeconds(v float64) float64 {
	return v / 1000
}

func normalizeSpeakerID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "speaker_") {
		return raw
	}
	return "speaker_" + raw
}

func anyASRString(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func speakerTag(speakerID string) string {
	if speakerID == "" {
		return ""
	}
	id := strings.TrimPrefix(speakerID, "speaker_")
	return " S" + id
}

func formatVideoTime(seconds float64) string {
	return fmt.Sprintf("%06.2f", seconds)
}

func formatVideoDuration(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	minutes := int(seconds / 60)
	remaining := seconds - float64(minutes*60)
	return fmt.Sprintf("%dm %04.1fs", minutes, remaining)
}

func firstASRNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
