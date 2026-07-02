package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/storage"
)

func TestFunASRHTTPClientSubmitsPollsDownloadsAndNormalizesTranscript(t *testing.T) {
	var sawSubmit bool
	queryCount := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/audio/asr/transcription":
			if r.Method != http.MethodPost {
				t.Fatalf("submit method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer dashscope-key" {
				t.Fatalf("authorization header = %q", got)
			}
			if got := r.Header.Get("X-DashScope-Async"); got != "enable" {
				t.Fatalf("async header = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode submit body: %v", err)
			}
			if body["model"] != "fun-asr" {
				t.Fatalf("model = %#v", body["model"])
			}
			input := body["input"].(map[string]any)
			urls := input["file_urls"].([]any)
			if len(urls) != 1 || urls[0] != "https://download.example.com/take.wav?signature=1" {
				t.Fatalf("file_urls = %#v", urls)
			}
			params := body["parameters"].(map[string]any)
			channels := params["channel_id"].([]any)
			if len(channels) != 1 || channels[0].(float64) != 0 {
				t.Fatalf("channel_id = %#v", channels)
			}
			sawSubmit = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"output": map[string]any{
					"task_status": "PENDING",
					"task_id":     "task-1",
				},
				"request_id": "req-submit",
			})
		case "/api/v1/tasks/task-1":
			if got := r.Header.Get("Authorization"); got != "Bearer dashscope-key" {
				t.Fatalf("query authorization header = %q", got)
			}
			queryCount++
			status := "PENDING"
			if queryCount > 1 {
				status = "SUCCEEDED"
			}
			resp := map[string]any{
				"request_id": "req-query",
				"output": map[string]any{
					"task_id":     "task-1",
					"task_status": status,
				},
			}
			if status == "SUCCEEDED" {
				resp["output"].(map[string]any)["results"] = []map[string]any{{
					"file_url":          "https://download.example.com/take.wav?signature=1",
					"transcription_url": server.URL + "/result.json",
					"subtask_status":    "SUCCEEDED",
				}}
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/result.json":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("transcript download should not forward provider authorization header, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_url": "https://download.example.com/take.wav?signature=1",
				"transcripts": []map[string]any{{
					"channel_id": 0,
					"text":       "你好，世界。",
					"sentences": []map[string]any{{
						"begin_time": 100,
						"end_time":   900,
						"text":       "你好，世界。",
						"speaker_id": 2,
						"words": []map[string]any{
							{"begin_time": 100, "end_time": 300, "text": "你好", "punctuation": "，"},
							{"begin_time": 500, "end_time": 900, "text": "世界", "punctuation": "。"},
						},
					}},
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewFunASRHTTPClient(config.FunASRConfig{
		BaseURL: server.URL,
		APIKey:  "dashscope-key",
		Model:   "fun-asr",
	})
	if err != nil {
		t.Fatalf("NewFunASRHTTPClient: %v", err)
	}
	client.pollInterval = time.Millisecond
	result, err := client.Transcribe(context.Background(), AudioASRTaskRequest{
		AudioURL: "https://download.example.com/take.wav?signature=1",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !sawSubmit || queryCount != 2 {
		t.Fatalf("request flow wrong: sawSubmit=%v queryCount=%d", sawSubmit, queryCount)
	}
	if result.TaskID != "task-1" || result.Status != "SUCCEEDED" || result.TranscriptionURL != server.URL+"/result.json" {
		t.Fatalf("unexpected task result: %#v", result)
	}
	if result.Transcript == nil || len(result.Transcript.Words) != 3 || result.Transcript.Words[0].Text != "你好，" {
		t.Fatalf("unexpected transcript result: %#v", result)
	}
	if result.Transcript.Words[0].Start != 0.1 || result.Transcript.Words[0].End != 0.3 || result.Transcript.Words[0].SpeakerID != "speaker_2" {
		t.Fatalf("first word = %#v", result.Transcript.Words[0])
	}
	if result.Transcript.Words[1].Type != "spacing" || result.Transcript.Words[1].Start != 0.3 || result.Transcript.Words[1].End != 0.5 {
		t.Fatalf("spacing = %#v", result.Transcript.Words[1])
	}
	if result.Transcript.Phrases[0].Text != "你好，世界。" || result.Transcript.Phrases[0].SpeakerID != "speaker_2" {
		t.Fatalf("phrase = %#v", result.Transcript.Phrases[0])
	}
	if result.Transcript.Metadata["provider"] != "aliyun-fun-asr-http" || result.Transcript.Metadata["model"] != "fun-asr" {
		t.Fatalf("metadata = %#v", result.Transcript.Metadata)
	}
}

func TestFunASRHTTPClientReportsFailedTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/audio/asr/transcription":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		case "/api/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{
				"task_id":     "task-1",
				"task_status": "FAILED",
				"results": []map[string]any{{
					"subtask_status": "FAILED",
					"code":           "FILE_DOWNLOAD_FAILED",
					"message":        "cannot fetch audio",
				}},
			}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewFunASRHTTPClient(config.FunASRConfig{
		BaseURL: server.URL,
		APIKey:  "dashscope-key",
		Model:   "fun-asr",
	})
	if err != nil {
		t.Fatalf("NewFunASRHTTPClient: %v", err)
	}
	_, err = client.Transcribe(context.Background(), AudioASRTaskRequest{
		AudioURL: "https://download.example.com/take.wav?signature=1",
	})
	if err == nil {
		t.Fatal("expected failed task error")
	}
	for _, want := range []string{"FAILED", "FILE_DOWNLOAD_FAILED", "cannot fetch audio"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestFunASRHTTPClientReportsSubmitFailureAndMissingTaskID(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       map[string]any
		want       string
	}{
		{
			name:       "non 2xx submit",
			statusCode: http.StatusBadRequest,
			body:       map[string]any{"message": "bad request"},
			want:       "submit FunASR task failed",
		},
		{
			name:       "missing task id",
			statusCode: http.StatusOK,
			body:       map[string]any{"output": map[string]any{"task_status": "PENDING"}},
			want:       "returned no task id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			client, err := NewFunASRHTTPClient(config.FunASRConfig{BaseURL: server.URL, APIKey: "dashscope-key", Model: "fun-asr"})
			if err != nil {
				t.Fatalf("NewFunASRHTTPClient: %v", err)
			}
			_, err = client.Transcribe(context.Background(), AudioASRTaskRequest{AudioURL: "https://download.example.com/take.wav"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Transcribe error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFunASRHTTPClientReportsSucceededTaskWithoutTranscriptionURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/audio/asr/transcription":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		case "/api/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{
				"task_id":     "task-1",
				"task_status": "SUCCEEDED",
				"results": []map[string]any{{
					"file_url":       "https://download.example.com/take.wav",
					"subtask_status": "SUCCEEDED",
				}},
			}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewFunASRHTTPClient(config.FunASRConfig{BaseURL: server.URL, APIKey: "dashscope-key", Model: "fun-asr"})
	if err != nil {
		t.Fatalf("NewFunASRHTTPClient: %v", err)
	}
	client.pollInterval = time.Millisecond
	_, err = client.Transcribe(context.Background(), AudioASRTaskRequest{AudioURL: "https://download.example.com/take.wav"})
	if err == nil || !strings.Contains(err.Error(), "no successful transcription_url") {
		t.Fatalf("Transcribe error = %v, want missing transcription_url", err)
	}
}

func TestFunASRHTTPClientReportsTranscriptDecodeFailure(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/audio/asr/transcription":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		case "/api/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{
				"task_id":     "task-1",
				"task_status": "SUCCEEDED",
				"results": []map[string]any{{
					"file_url":          "https://download.example.com/take.wav",
					"transcription_url": server.URL + "/bad.json",
					"subtask_status":    "SUCCEEDED",
				}},
			}})
		case "/bad.json":
			_, _ = w.Write([]byte(`{"transcripts":`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewFunASRHTTPClient(config.FunASRConfig{BaseURL: server.URL, APIKey: "dashscope-key", Model: "fun-asr"})
	if err != nil {
		t.Fatalf("NewFunASRHTTPClient: %v", err)
	}
	client.pollInterval = time.Millisecond
	_, err = client.Transcribe(context.Background(), AudioASRTaskRequest{AudioURL: "https://download.example.com/take.wav"})
	if err == nil || !strings.Contains(err.Error(), "decode FunASR transcript") {
		t.Fatalf("Transcribe error = %v, want decode failure", err)
	}
}

func TestFunASRHTTPClientStopsPollingOnContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/audio/asr/transcription":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		case "/api/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewFunASRHTTPClient(config.FunASRConfig{BaseURL: server.URL, APIKey: "dashscope-key", Model: "fun-asr"})
	if err != nil {
		t.Fatalf("NewFunASRHTTPClient: %v", err)
	}
	client.pollInterval = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = client.Transcribe(ctx, AudioASRTaskRequest{AudioURL: "https://download.example.com/take.wav"})
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("Transcribe error = %v, want context deadline", err)
	}
}

type fakeAudioASRCompatClient struct {
	transcribeReq AudioASRTaskRequest
	result        *AudioASRTaskResult
}

func (f *fakeAudioASRCompatClient) Transcribe(_ context.Context, req AudioASRTaskRequest) (*AudioASRTaskResult, error) {
	f.transcribeReq = req
	return f.result, nil
}

type fakeAudioASRStorage struct {
	name              string
	files             map[string][]byte
	uploadContentType string
	downloadURL       string
	signedKey         string
	signedKeys        []string
	readKey           string
	ownedPrefix       string
}

func (f *fakeAudioASRStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeAudioASRStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = append([]byte(nil), data...)
	return &storage.UploadResult{URL: f.GetURL(key), Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeAudioASRStorage) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeAudioASRStorage) UploadURL(_ context.Context, key string, contentType string, _ int) (string, error) {
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}

func (f *fakeAudioASRStorage) GetURL(key string) string {
	return "https://cdn.example.com/" + key
}

func (f *fakeAudioASRStorage) Read(_ context.Context, key string) ([]byte, error) {
	f.readKey = key
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeAudioASRStorage) Delete(context.Context, string) error { return nil }

func (f *fakeAudioASRStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	f.signedKey = key
	f.signedKeys = append(f.signedKeys, key)
	if f.downloadURL != "" {
		return f.downloadURL, nil
	}
	return "https://download.example.com/" + key, nil
}

func (f *fakeAudioASRStorage) HasCustomDomain() bool { return true }
func (f *fakeAudioASRStorage) IsOwnedURL(rawURL string) bool {
	return f.ownedPrefix != "" && strings.HasPrefix(rawURL, f.ownedPrefix)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAudioASRServiceCreateTaskRejectsMissingAudioSource(t *testing.T) {
	svc := NewAudioASRServiceWithClient(&fakeAudioASRCompatClient{}, nil)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{})
	if err == nil || !strings.Contains(err.Error(), "audio_key or audio_url is required") {
		t.Fatalf("CreateTask error = %v, want missing audio source", err)
	}
}

func TestAudioASRServiceCreateTaskTranscribesOSSKey(t *testing.T) {
	fake := &fakeAudioASRCompatClient{result: &AudioASRTaskResult{
		TaskID: "asr-task-1",
		Status: "SUCCEEDED",
		Transcript: &VideoTranscript{
			Words: []VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0.12, End: 0.42, SpeakerID: "speaker_0"}},
		},
	}}
	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{
		"uploads/video-audio/take.wav": []byte("fake-wav"),
	}}
	svc := NewAudioASRServiceWithClient(fake, store)

	result, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{
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
	if fake.transcribeReq.AudioURL != "https://download.example.com/uploads/video-audio/take.wav" || fake.transcribeReq.AudioKey != "uploads/video-audio/take.wav" || fake.transcribeReq.SpeakerCount != 2 {
		t.Fatalf("transcribe request source not forwarded: %#v", fake.transcribeReq)
	}
	if !containsString(store.signedKeys, "uploads/video-audio/take.wav") {
		t.Fatalf("audio_key should sign storage object, signedKeys=%q", store.signedKeys)
	}
	if result.TranscriptKey != "uploads/video-transcripts/asr-task-1.json" || result.WordCount != 1 {
		t.Fatalf("compact transcript metadata not populated: %#v", result)
	}
	if !strings.Contains(string(store.files[result.TranscriptKey]), "你好") {
		t.Fatalf("stored transcript missing normalized JSON: %s", store.files[result.TranscriptKey])
	}
}

func TestAudioASRServiceCreateTaskTranscribesOwnedAudioURL(t *testing.T) {
	fake := &fakeAudioASRCompatClient{result: &AudioASRTaskResult{
		TaskID: "asr-task-1",
		Status: "SUCCEEDED",
		Transcript: &VideoTranscript{
			Words: []VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0.12, End: 0.42}},
		},
	}}
	store := &fakeAudioASRStorage{
		name:        "oss",
		ownedPrefix: "https://cdn.example.com/",
		files: map[string][]byte{
			"uploads/video-audio/take.wav": []byte("fake-wav"),
		},
	}
	svc := NewAudioASRServiceWithClient(fake, store)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{
		AudioURL: "https://cdn.example.com/uploads/video-audio/take.wav",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if !containsString(store.signedKeys, "uploads/video-audio/take.wav") {
		t.Fatalf("owned audio_url should sign storage object, signedKeys=%q", store.signedKeys)
	}
	if fake.transcribeReq.AudioURL != "https://download.example.com/uploads/video-audio/take.wav" {
		t.Fatalf("transcribe request metadata = %#v", fake.transcribeReq)
	}
}

func TestAudioASRServiceCreateTaskRequiresTranscriptStorage(t *testing.T) {
	fake := &fakeAudioASRCompatClient{result: &AudioASRTaskResult{
		TaskID: "asr-task-1",
		Status: "SUCCEEDED",
		Transcript: &VideoTranscript{
			Words: []VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0.12, End: 0.42}},
		},
	}}
	svc := NewAudioASRServiceWithClient(fake, nil)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{
		AudioURL: "https://media.example.org/audio.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "transcript storage is required") {
		t.Fatalf("CreateTask error = %v, want transcript storage requirement", err)
	}
	if fake.transcribeReq.AudioURL != "https://media.example.org/audio.wav" || fake.transcribeReq.Audio != nil {
		t.Fatalf("external audio URL not passed through: %#v", fake.transcribeReq)
	}
}

func TestAudioASRServiceCreateTaskRejectsNonHTTPSExternalAudioURL(t *testing.T) {
	svc := NewAudioASRServiceWithClient(&fakeAudioASRCompatClient{}, nil)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{
		AudioURL: "http://media.example.org/audio.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve audio URL") || !strings.Contains(err.Error(), "external media URL must be an HTTPS URL") {
		t.Fatalf("CreateTask error = %v, want HTTPS rejection", err)
	}
}

func TestAudioASRServiceCreateTaskRejectsFilePath(t *testing.T) {
	svc := NewAudioASRServiceWithClient(&fakeAudioASRCompatClient{}, nil)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{FilePath: "/tmp/audio.wav"})
	if err == nil || !strings.Contains(err.Error(), "file_path is no longer supported") {
		t.Fatalf("CreateTask error = %v, want file_path unsupported", err)
	}
}

func TestAudioASRServiceCreateTaskRejectsNonVideoUseAudioKey(t *testing.T) {
	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{
		"uploads/references/user-1/audio.wav": []byte("fake-wav"),
	}}
	svc := NewAudioASRServiceWithClient(&fakeAudioASRCompatClient{}, store)

	_, err := svc.CreateTask(context.Background(), AudioASRTaskRequest{AudioKey: "uploads/references/user-1/audio.wav"})
	if err == nil || !strings.Contains(err.Error(), "audio_key must be under uploads/video-audio/") {
		t.Fatalf("CreateTask error = %v, want video audio prefix rejection", err)
	}
}

type infiniteAReader struct{}

func (r *infiniteAReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestNormalizeFunASRRecordedTranscriptPreservesWordsSentencesAndSpacing(t *testing.T) {
	raw := []byte(`{
		"transcripts": [{
			"channel_id": 0,
			"text": "你好世界 第二句",
			"sentences": [
				{"begin_time": 120, "end_time": 980, "text": "你好世界", "speaker_id": 0, "words": [
					{"begin_time": 120, "end_time": 420, "text": "你好"},
					{"begin_time": 700, "end_time": 980, "text": "世界"}
				]},
				{"begin_time": 1650, "end_time": 2100, "text": "第二句", "speaker_id": 1, "words": [
					{"begin_time": 1650, "end_time": 2100, "text": "第二句"}
				]}
			]
		}]
	}`)

	tr, err := NormalizeFunASRRecordedTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeFunASRRecordedTranscript: %v", err)
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

func TestNormalizeFunASRRecordedTranscriptFallsBackToSentencesWhenWordsMissing(t *testing.T) {
	raw := []byte(`{
		"transcripts": [{
			"sentences": [
				{"begin_time": 0, "end_time": 600, "text": "第一句", "speaker_id": 0},
				{"begin_time": 1200, "end_time": 1800, "text": "第二句", "speaker_id": 0}
			]
		}]
	}`)

	tr, err := NormalizeFunASRRecordedTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeFunASRRecordedTranscript: %v", err)
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

func TestNormalizeFunASRRecordedTranscriptPreservesWordPunctuation(t *testing.T) {
	raw := []byte(`{
		"transcripts": [{
			"sentences": [{
				"begin_time": 0,
				"end_time": 1000,
				"text": "开场，好。",
				"speaker_id": 0,
				"words": [
					{"begin_time": 0, "end_time": 300, "text": "开场", "punctuation": "，"},
					{"begin_time": 700, "end_time": 1000, "text": "好", "punctuation": "。"}
				]
			}]
		}]
	}`)

	tr, err := NormalizeFunASRRecordedTranscript(raw)
	if err != nil {
		t.Fatalf("NormalizeFunASRRecordedTranscript: %v", err)
	}
	if len(tr.Words) != 3 {
		t.Fatalf("words = %#v", tr.Words)
	}
	if tr.Words[0].Text != "开场，" || tr.Words[0].SpeakerID != "speaker_0" {
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
