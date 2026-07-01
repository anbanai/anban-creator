package service

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/storage"
)

const defaultVideoAudioURLTTL = 24 * 3600

// VideoASRClient is the direct adapter boundary for OpenAI-compatible FunASR calls.
type VideoASRClient interface {
	Transcribe(ctx context.Context, req VideoASRTaskRequest) (*VideoASRTaskResult, error)
}

// VideoASRService coordinates storage and provider-side word-level ASR.
type VideoASRService struct {
	client VideoASRClient
	store  storage.Provider
	logger *zerolog.Logger
	mu     sync.RWMutex
	cache  map[string]*VideoASRTaskResult
}

func NewVideoASRService(cfg config.FunASRConfig, store storage.Provider, logger *zerolog.Logger) (*VideoASRService, error) {
	client, err := NewOpenAIFunASRClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewVideoASRServiceWithClient(client, store), nil
}

func NewVideoASRServiceWithClient(client VideoASRClient, store storage.Provider) *VideoASRService {
	return &VideoASRService{client: client, store: store, cache: map[string]*VideoASRTaskResult{}}
}

type VideoAudioUploadResult struct {
	URL           string `json:"url"`
	Key           string `json:"key"`
	DownloadURL   string `json:"download_url,omitempty"`
	Size          int64  `json:"size"`
	MimeType      string `json:"mime_type"`
	ExpiresSecond int    `json:"expires_seconds"`
}

func (s *VideoASRService) UploadAudio(ctx context.Context, filePath string, expiresSeconds int) (*VideoAudioUploadResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("storage provider is not available; configure OSS/CDN storage before using upload_video_audio")
	}
	if s.store.Name() != "oss" {
		return nil, fmt.Errorf("upload_video_audio requires OSS storage; configure OSS/CDN storage before using this optional URL helper")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file_path must point to an audio file, got directory")
	}
	if expiresSeconds <= 0 {
		expiresSeconds = defaultVideoAudioURLTTL
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := audioContentType(ext)
	if contentType == "application/octet-stream" {
		if t := mime.TypeByExtension(ext); t != "" {
			contentType = t
		}
	}
	key := fmt.Sprintf("uploads/video-audio/%s%s", uuid.NewString(), ext)
	uploaded, err := s.store.UploadFile(ctx, key, filePath, contentType)
	if err != nil {
		return nil, fmt.Errorf("upload video audio to OSS: %w", err)
	}
	if uploaded.Size == 0 {
		uploaded.Size = info.Size()
	}

	downloadURL := uploaded.URL
	if !s.store.HasCustomDomain() {
		downloadURL, err = s.store.DownloadURL(ctx, key, expiresSeconds)
		if err != nil {
			return nil, fmt.Errorf("create signed audio URL: %w", err)
		}
	}
	if !strings.HasPrefix(downloadURL, "https://") {
		return nil, fmt.Errorf("uploaded audio URL is not publicly accessible HTTPS; configure OSS/CDN storage or pass an existing public HTTPS URL")
	}
	return &VideoAudioUploadResult{
		URL:           uploaded.URL,
		Key:           uploaded.Key,
		DownloadURL:   downloadURL,
		Size:          uploaded.Size,
		MimeType:      uploaded.MimeType,
		ExpiresSecond: expiresSeconds,
	}, nil
}

type VideoASRTaskRequest struct {
	FilePath     string `json:"file_path,omitempty"`
	AudioURL     string `json:"audio_url"`
	LanguageHint string `json:"language_hint,omitempty"`
	SpeakerCount int    `json:"speaker_count,omitempty"`
}

type VideoASRTaskResult struct {
	TaskID           string           `json:"task_id"`
	Status           string           `json:"status"`
	TranscriptionURL string           `json:"transcription_url,omitempty"`
	Transcript       *VideoTranscript `json:"transcript,omitempty"`
	Error            string           `json:"error,omitempty"`
}

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

func (s *VideoASRService) CreateTask(ctx context.Context, req VideoASRTaskRequest) (*VideoASRTaskResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("FunASR client is not configured")
	}
	if strings.TrimSpace(req.FilePath) == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	info, err := os.Stat(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file_path must point to an audio file, got directory")
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

func (s *VideoASRService) QueryTask(ctx context.Context, taskID string) (*VideoASRTaskResult, error) {
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
		return nil, fmt.Errorf("video ASR task %q was not found; create_video_asr_task transcribes synchronously and only completed local results can be queried", taskID)
	}
	return result, nil
}

// OpenAIFunASRClient calls an OpenAI-compatible FunASR transcription endpoint.
type OpenAIFunASRClient struct {
	model  string
	client openai.Client
}

func NewOpenAIFunASRClient(cfg config.FunASRConfig) (*OpenAIFunASRClient, error) {
	if cfg.Empty() {
		return nil, nil
	}
	if !cfg.Complete() {
		return nil, fmt.Errorf("incomplete FunASR config: base_url, api_key, and model are required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	httpClient := &http.Client{Timeout: timeout}
	return &OpenAIFunASRClient{
		model: strings.TrimSpace(cfg.Model),
		client: openai.NewClient(
			option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")),
			option.WithAPIKey(cfg.APIKey),
			option.WithHTTPClient(httpClient),
		),
	}, nil
}

func (c *OpenAIFunASRClient) Transcribe(ctx context.Context, req VideoASRTaskRequest) (*VideoASRTaskResult, error) {
	if c == nil {
		return nil, fmt.Errorf("FunASR client is not configured")
	}
	file, err := os.Open(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("open audio file for FunASR: %w", err)
	}
	defer file.Close()
	params := openai.AudioTranscriptionNewParams{
		File:                   openai.File(file, filepath.Base(req.FilePath), audioContentType(strings.ToLower(filepath.Ext(req.FilePath)))),
		Model:                  openai.AudioModel(c.model),
		ResponseFormat:         openai.AudioResponseFormatVerboseJSON,
		TimestampGranularities: []string{"word", "segment"},
	}
	if strings.TrimSpace(req.LanguageHint) != "" {
		params.Language = openai.String(strings.TrimSpace(req.LanguageHint))
	}
	resp, err := c.client.Audio.Transcriptions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("transcribe audio with FunASR: %w", err)
	}
	raw := []byte(resp.RawJSON())
	if len(raw) == 0 {
		raw, _ = json.Marshal(resp)
	}
	transcript, err := NormalizeOpenAIVideoTranscript(raw)
	if err != nil {
		return nil, err
	}
	if transcript.Metadata == nil {
		transcript.Metadata = map[string]any{}
	}
	transcript.Metadata["model"] = c.model
	transcript.Metadata["file_path"] = req.FilePath
	return &VideoASRTaskResult{
		TaskID:     "asr-" + uuid.NewString(),
		Status:     "SUCCEEDED",
		Transcript: transcript,
	}, nil
}

type openAIVideoTranscript struct {
	Text     string                     `json:"text"`
	Language string                     `json:"language"`
	Duration float64                    `json:"duration"`
	Segments []openAIVideoSegment       `json:"segments"`
	Words    []openAIVideoWord          `json:"words"`
	Raw      map[string]json.RawMessage `json:"-"`
}

type openAIVideoSegment struct {
	Start     float64           `json:"start"`
	End       float64           `json:"end"`
	Text      string            `json:"text"`
	Speaker   string            `json:"speaker"`
	SpeakerID string            `json:"speaker_id"`
	Words     []openAIVideoWord `json:"words"`
}

type openAIVideoWord struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Word      string  `json:"word"`
	Text      string  `json:"text"`
	Speaker   string  `json:"speaker"`
	SpeakerID string  `json:"speaker_id"`
}

func NormalizeOpenAIVideoTranscript(raw []byte) (*VideoTranscript, error) {
	var parsed openAIVideoTranscript
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode FunASR transcript: %w", err)
	}
	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(raw, &rawMap)
	out := &VideoTranscript{
		Words:    []VideoTranscriptWord{},
		Phrases:  []VideoTranscriptPhrase{},
		Metadata: map[string]any{"provider": "openai-compatible-funasr"},
		Raw:      rawMap,
	}
	if parsed.Language != "" {
		out.Metadata["language"] = parsed.Language
	}
	if parsed.Duration > 0 {
		out.Metadata["duration"] = parsed.Duration
	}

	for _, segment := range parsed.Segments {
		speaker := normalizeSpeakerID(firstASRNonEmpty(segment.SpeakerID, segment.Speaker))
		out.Phrases = append(out.Phrases, VideoTranscriptPhrase{
			Start:     segment.Start,
			End:       segment.End,
			Text:      strings.TrimSpace(segment.Text),
			SpeakerID: speaker,
		})
	}

	words := parsed.Words
	if len(words) == 0 {
		for _, segment := range parsed.Segments {
			for _, word := range segment.Words {
				if word.Speaker == "" && word.SpeakerID == "" {
					word.Speaker = firstASRNonEmpty(segment.SpeakerID, segment.Speaker)
				}
				words = append(words, word)
			}
		}
	}

	var prevEnd *float64
	if len(words) > 0 {
		for _, word := range words {
			text := strings.TrimSpace(firstASRNonEmpty(word.Word, word.Text))
			if text == "" {
				continue
			}
			start := word.Start
			end := word.End
			if prevEnd != nil && start > *prevEnd {
				out.Words = append(out.Words, VideoTranscriptWord{Type: "spacing", Start: *prevEnd, End: start})
			}
			wordSpeaker := normalizeSpeakerID(firstASRNonEmpty(word.SpeakerID, word.Speaker, speakerForTime(parsed.Segments, start, end)))
			out.Words = append(out.Words, VideoTranscriptWord{
				Type:      "word",
				Text:      text,
				Start:     start,
				End:       end,
				SpeakerID: wordSpeaker,
			})
			prevEnd = &end
		}
		return out, nil
	}

	for _, segment := range parsed.Segments {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		if prevEnd != nil && segment.Start > *prevEnd {
			out.Words = append(out.Words, VideoTranscriptWord{Type: "spacing", Start: *prevEnd, End: segment.Start})
		}
		speaker := normalizeSpeakerID(firstASRNonEmpty(segment.SpeakerID, segment.Speaker))
		out.Words = append(out.Words, VideoTranscriptWord{
			Type:      "word",
			Text:      strings.TrimSpace(segment.Text),
			Start:     segment.Start,
			End:       segment.End,
			SpeakerID: speaker,
		})
		end := segment.End
		prevEnd = &end
	}
	if len(out.Phrases) == 0 && strings.TrimSpace(parsed.Text) != "" {
		out.Words = append(out.Words, VideoTranscriptWord{
			Type:  "word",
			Text:  strings.TrimSpace(parsed.Text),
			Start: 0,
			End:   parsed.Duration,
		})
		out.Phrases = append(out.Phrases, VideoTranscriptPhrase{Start: 0, End: parsed.Duration, Text: strings.TrimSpace(parsed.Text)})
	}
	return out, nil
}

func speakerForTime(segments []openAIVideoSegment, start, end float64) string {
	for _, segment := range segments {
		if start >= segment.Start && end <= segment.End {
			return firstASRNonEmpty(segment.SpeakerID, segment.Speaker)
		}
	}
	return ""
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
