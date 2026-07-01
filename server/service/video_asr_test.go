package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/server/config"
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

	audioPath := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake-wav"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	client, err := NewOpenAIFunASRClient(config.FunASRConfig{
		BaseURL: server.URL,
		APIKey:  "not-needed",
		Model:   "sensevoice",
	})
	if err != nil {
		t.Fatalf("NewOpenAIFunASRClient: %v", err)
	}
	result, err := client.Transcribe(context.Background(), VideoASRTaskRequest{FilePath: audioPath})
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

func TestVideoASRServiceCreateTaskTranscribesLocalFile(t *testing.T) {
	fake := &fakeVideoASRClient{result: &VideoASRTaskResult{
		TaskID: "asr-task-1",
		Status: "SUCCEEDED",
		Transcript: &VideoTranscript{
			Words: []VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0.12, End: 0.42, SpeakerID: "speaker_0"}},
		},
	}}
	svc := NewVideoASRServiceWithClient(fake, nil)
	audioPath := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake-wav"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	result, err := svc.CreateTask(context.Background(), VideoASRTaskRequest{
		FilePath:     audioPath,
		LanguageHint: "zh",
		SpeakerCount: 2,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if result.TaskID != "asr-task-1" || result.Status != "SUCCEEDED" {
		t.Fatalf("transcribe result = %#v", result)
	}
	if fake.transcribeReq.FilePath != audioPath || fake.transcribeReq.SpeakerCount != 2 {
		t.Fatalf("transcribe request not forwarded: %#v", fake.transcribeReq)
	}
	if result.Transcript == nil || len(result.Transcript.Words) != 1 {
		t.Fatalf("transcribe result = %#v", result)
	}
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
