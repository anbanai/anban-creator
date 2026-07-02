package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/storage"
)

func TestOpenAIFunASRClientTranscribesWithOpenAICompatibleAudioAPI(t *testing.T) {
	var sawModel, sawFormat, sawFile bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer not-needed" {
			t.Fatalf("authorization header = %q", got)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		sawModel = r.FormValue("model") == "sensevoice"
		sawFormat = r.FormValue("response_format") == "verbose_json"
		files := r.MultipartForm.File["file"]
		sawFile = len(files) == 1 && files[0].Filename == "audio.wav"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": "你好世界",
			"segments": []map[string]any{{
				"start": 0,
				"end":   0.8,
				"text":  "你好世界",
			}},
			"words": []map[string]any{{
				"start": 0,
				"end":   0.8,
				"word":  "你好世界",
			}},
		})
	}))
	defer server.Close()

	client, err := NewOpenAIFunASRClient(config.FunASRConfig{
		BaseURL: server.URL,
		APIKey:  "not-needed",
		Model:   "sensevoice",
	})
	if err != nil {
		t.Fatalf("NewOpenAIFunASRClient: %v", err)
	}
	result, err := client.Transcribe(context.Background(), VideoASRTaskRequest{
		Audio:       bytes.NewReader([]byte("fake-wav")),
		Filename:    "audio.wav",
		ContentType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !sawModel || !sawFormat || !sawFile {
		t.Fatalf("SDK request missing expected fields: model=%v format=%v file=%v", sawModel, sawFormat, sawFile)
	}
	if result.Transcript == nil || len(result.Transcript.Words) != 1 || result.Transcript.Words[0].Text != "你好世界" {
		t.Fatalf("unexpected transcript result: %#v", result)
	}
}

type fakeVideoASRClient struct {
	transcribeReq VideoASRTaskRequest
	result        *VideoASRTaskResult
}

func (f *fakeVideoASRClient) Transcribe(_ context.Context, req VideoASRTaskRequest) (*VideoASRTaskResult, error) {
	f.transcribeReq = req
	return f.result, nil
}

type fakeVideoASRStorage struct {
	name              string
	files             map[string][]byte
	uploadContentType string
	downloadURL       string
}

func (f *fakeVideoASRStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeVideoASRStorage) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeVideoASRStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeVideoASRStorage) UploadURL(_ context.Context, key string, contentType string, _ int) (string, error) {
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}

func (f *fakeVideoASRStorage) GetURL(key string) string {
	return "https://cdn.example.com/" + key
}

func (f *fakeVideoASRStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeVideoASRStorage) Delete(context.Context, string) error { return nil }

func (f *fakeVideoASRStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	if f.downloadURL != "" {
		return f.downloadURL, nil
	}
	return "https://download.example.com/" + key, nil
}

func (f *fakeVideoASRStorage) HasCustomDomain() bool  { return true }
func (f *fakeVideoASRStorage) IsOwnedURL(string) bool { return false }

func TestVideoASRServiceCreateTaskRejectsMissingAudioSource(t *testing.T) {
	svc := NewVideoASRServiceWithClient(&fakeVideoASRClient{}, nil)

	_, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{})
	if err == nil || !strings.Contains(err.Error(), "audio_key or audio_url is required") {
		t.Fatalf("CreateTask error = %v, want missing audio source", err)
	}
}

func TestVideoASRServiceCreateTaskTranscribesOSSKey(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("fake-wav"))
	}))
	defer srv.Close()
	oldClient := videoASRHTTPClient
	videoASRHTTPClient = srv.Client()
	t.Cleanup(func() { videoASRHTTPClient = oldClient })

	fake := &fakeVideoASRClient{result: &VideoASRTaskResult{
		TaskID: "asr-task-1",
		Status: "SUCCEEDED",
		Transcript: &VideoTranscript{
			Words: []VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0.12, End: 0.42, SpeakerID: "speaker_0"}},
		},
	}}
	store := &fakeVideoASRStorage{name: "oss", downloadURL: srv.URL}
	svc := NewVideoASRServiceWithClient(fake, store)

	result, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{
		AudioKey:     "uploads/video-audio/take.wav",
		LanguageHint: "zh",
		SpeakerCount: 2,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if result.TaskID != "asr-task-1" || result.Status != "SUCCEEDED" {
		t.Fatalf("transcribe result = %#v", result)
	}
	if fake.transcribeReq.Audio == nil || fake.transcribeReq.Filename != "take.wav" || fake.transcribeReq.ContentType != "audio/wav" {
		t.Fatalf("transcribe request audio metadata not forwarded: %#v", fake.transcribeReq)
	}
	if fake.transcribeReq.AudioKey != "uploads/video-audio/take.wav" || fake.transcribeReq.SpeakerCount != 2 {
		t.Fatalf("transcribe request source not forwarded: %#v", fake.transcribeReq)
	}
}

func TestVideoASRServiceCreateTaskRejectsFilePath(t *testing.T) {
	svc := NewVideoASRServiceWithClient(&fakeVideoASRClient{}, nil)

	_, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{FilePath: "/tmp/audio.wav"})
	if err == nil || !strings.Contains(err.Error(), "file_path is no longer supported") {
		t.Fatalf("CreateTask error = %v, want file_path unsupported", err)
	}
}

func TestVideoASRServiceCreateTaskRejectsNonVideoAudioKey(t *testing.T) {
	store := &fakeVideoASRStorage{name: "oss", files: map[string][]byte{
		"uploads/references/user-1/audio.wav": []byte("fake-wav"),
	}}
	svc := NewVideoASRServiceWithClient(&fakeVideoASRClient{}, store)

	_, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{AudioKey: "uploads/references/user-1/audio.wav"})
	if err == nil || !strings.Contains(err.Error(), "audio_key must be under uploads/video-audio/") {
		t.Fatalf("CreateTask error = %v, want video audio prefix rejection", err)
	}
}

func TestVideoASRServiceCreateTaskRejectsOversizedAudioURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = io.Copy(w, io.LimitReader(&infiniteAReader{}, maxVideoASRAudioBytes+1))
	}))
	defer srv.Close()
	oldClient := videoASRHTTPClient
	videoASRHTTPClient = srv.Client()
	t.Cleanup(func() { videoASRHTTPClient = oldClient })

	svc := NewVideoASRServiceWithClient(&fakeVideoASRClient{}, nil)
	_, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{AudioURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "audio URL exceeds max size") {
		t.Fatalf("CreateTask error = %v, want oversized URL rejection", err)
	}
}

func TestVideoASRServiceCreateTaskRejectsOversizedAudioKeyWithoutStorageRead(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = io.Copy(w, io.LimitReader(&infiniteAReader{}, maxVideoASRAudioBytes+1))
	}))
	defer srv.Close()
	oldClient := videoASRHTTPClient
	videoASRHTTPClient = srv.Client()
	t.Cleanup(func() { videoASRHTTPClient = oldClient })

	store := &fakeVideoASRStorage{name: "oss", downloadURL: srv.URL}
	svc := NewVideoASRServiceWithClient(&fakeVideoASRClient{}, store)
	_, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{AudioKey: "uploads/video-audio/take.wav"})
	if err == nil || !strings.Contains(err.Error(), "audio URL exceeds max size") {
		t.Fatalf("CreateTask error = %v, want oversized audio_key rejection", err)
	}
}

type infiniteAReader struct{}

func (r *infiniteAReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestNormalizeOpenAIVideoTranscriptPreservesWordsSegmentsAndSpacing(t *testing.T) {
	raw := []byte(`{
		"text": "你好世界 第二句",
		"segments": [
			{"id": 0, "start": 0.12, "end": 0.98, "text": "你好世界", "speaker": "0"},
			{"id": 1, "start": 1.65, "end": 2.10, "text": "第二句", "speaker": "1"}
		],
		"words": [
			{"start": 0.12, "end": 0.42, "word": "你好"},
			{"start": 0.70, "end": 0.98, "word": "世界"},
			{"start": 1.65, "end": 2.10, "word": "第二句"}
		]
	}`)

	tr, err := NormalizeOpenAIVideoTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeOpenAIVideoTranscript: %v", err)
	}
	if len(tr.Words) != 5 {
		t.Fatalf("words = %#v", tr.Words)
	}
	if tr.Words[0].Type != "word" || tr.Words[0].Start != 0.12 || tr.Words[0].End != 0.42 || tr.Words[0].SpeakerID != "speaker_0" {
		t.Fatalf("first word = %#v", tr.Words[0])
	}
	if tr.Words[1].Type != "spacing" || tr.Words[1].Start != 0.42 || tr.Words[1].End != 0.70 {
		t.Fatalf("spacing = %#v", tr.Words[1])
	}
	if tr.Words[3].Type != "spacing" || tr.Words[3].Start != 0.98 || tr.Words[3].End != 1.65 {
		t.Fatalf("sentence spacing = %#v", tr.Words[3])
	}
	if tr.Words[4].SpeakerID != "speaker_1" {
		t.Fatalf("speaker normalization failed: %#v", tr.Words[4])
	}
}

func TestNormalizeOpenAIVideoTranscriptFallsBackToSegmentsWhenWordsMissing(t *testing.T) {
	raw := []byte(`{
		"text": "第一句 第二句",
		"segments": [
			{"start": 0, "end": 0.6, "text": "第一句", "speaker": "speaker_0"},
			{"start": 1.2, "end": 1.8, "text": "第二句", "speaker": "speaker_0"}
		]
	}`)

	tr, err := NormalizeOpenAIVideoTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeOpenAIVideoTranscript: %v", err)
	}
	if len(tr.Words) != 3 {
		t.Fatalf("words = %#v", tr.Words)
	}
	if tr.Words[0].Text != "第一句" || tr.Words[0].Start != 0 || tr.Words[0].End != 0.6 {
		t.Fatalf("first fallback word = %#v", tr.Words[0])
	}
	if tr.Words[1].Type != "spacing" || tr.Words[1].Start != 0.6 || tr.Words[1].End != 1.2 {
		t.Fatalf("fallback spacing = %#v", tr.Words[1])
	}
}

func TestNormalizeOpenAIVideoTranscriptReadsNestedSegmentWords(t *testing.T) {
	raw := []byte(`{
		"text": "开场 好",
		"segments": [
			{
				"start": 0,
				"end": 1.0,
				"text": "开场 好",
				"speaker_id": "0",
				"words": [
					{"start": 0.0, "end": 0.3, "text": "开场"},
					{"start": 0.7, "end": 1.0, "text": "好"}
				]
			}
		]
	}`)

	tr, err := NormalizeOpenAIVideoTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeOpenAIVideoTranscript: %v", err)
	}
	if len(tr.Words) != 3 {
		t.Fatalf("words = %#v", tr.Words)
	}
	if tr.Words[0].Text != "开场" || tr.Words[0].SpeakerID != "speaker_0" {
		t.Fatalf("first nested word = %#v", tr.Words[0])
	}
	if tr.Words[1].Type != "spacing" || tr.Words[1].Start != 0.3 || tr.Words[1].End != 0.7 {
		t.Fatalf("nested spacing = %#v", tr.Words[1])
	}
}

func TestPackVideoTranscriptsBreaksOnSilenceAndSpeaker(t *testing.T) {
	transcripts := map[string]VideoTranscript{
		"take-a": {
			Words: []VideoTranscriptWord{
				{Type: "word", Text: "第一", Start: 0.0, End: 0.2, SpeakerID: "speaker_0"},
				{Type: "word", Text: "句", Start: 0.25, End: 0.4, SpeakerID: "speaker_0"},
				{Type: "spacing", Start: 0.4, End: 1.1},
				{Type: "word", Text: "第二", Start: 1.1, End: 1.4, SpeakerID: "speaker_0"},
				{Type: "word", Text: "换人", Start: 1.5, End: 1.8, SpeakerID: "speaker_1"},
			},
		},
	}

	md, err := PackVideoTranscripts(transcripts, 0.5)
	if err != nil {
		t.Fatalf("PackVideoTranscripts: %v", err)
	}
	for _, want := range []string{
		"## take-a",
		"[000.00-000.40] S0 第一句",
		"[001.10-001.40] S0 第二",
		"[001.50-001.80] S1 换人",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("packed markdown missing %q:\n%s", want, md)
		}
	}
}
