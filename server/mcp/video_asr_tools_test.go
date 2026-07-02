package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func TestVideoASRHandlersValidateMissingServiceAndArgs(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	}
	result, err := createVideoASRTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoASRTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when service is missing")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "video ASR service not available") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestPrepareFileUploadReturnsSignedVideoAudioUpload(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{name: "oss", url: "https://cdn.example.com/uploads/video-audio/audio.wav"}
	svcs = &Services{Store: store, VideoASRSvc: service.NewVideoASRServiceWithClient(nil, store)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"purpose":"video_audio","filename":"take.wav","content_type":"audio/wav"}`)},
	}
	result, err := prepareFileUploadHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("prepareFileUploadHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{
		`"method":"PUT"`,
		`"key":"uploads/video-audio/`,
		`"upload_url":"https://upload.example.com/put?signature=1"`,
		`"download_url":"https://download.example.com/get?signature=1"`,
		`"public_url":"https://cdn.example.com/`,
		`"Content-Type":"audio/wav"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
	if store.uploadContentType != "audio/wav" {
		t.Fatalf("UploadURL content type = %q, want audio/wav", store.uploadContentType)
	}
}

func TestPrepareFileUploadReturnsSignedLiveAudioUpload(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{name: "oss", url: "https://cdn.example.com/uploads/live-audio/audio.mp3"}
	svcs = &Services{Store: store}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"purpose":"live_audio","filename":"live.mp3","content_type":"audio/mpeg"}`)},
	}
	result, err := prepareFileUploadHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("prepareFileUploadHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{
		`"method":"PUT"`,
		`"key":"uploads/live-audio/`,
		`"Content-Type":"audio/mpeg"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestCreateVideoASRTaskHandlerReturnsTranscript(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("fake-wav"))
	}))
	defer srv.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	store := &fakeVideoReferenceStorage{name: "oss", url: "https://cdn.example.com/uploads/video-audio/take.wav", files: map[string][]byte{
		"uploads/video-audio/take.wav": []byte("fake-wav"),
	}, downloadURL: srv.URL}
	svcs = &Services{VideoASRSvc: service.NewVideoASRServiceWithClient(&fakeMCPVideoASRClient{
		result: &service.VideoASRTaskResult{
			TaskID: "asr-task-1",
			Status: "SUCCEEDED",
			Transcript: &service.VideoTranscript{
				Words: []service.VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0, End: 0.3}},
			},
		},
	}, store)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"audio_key":"uploads/video-audio/take.wav"}`)},
	}
	result, err := createVideoASRTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoASRTaskHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"asr-task-1", "SUCCEEDED", "你好", "words"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestPrepareFileUploadRejectsInvalidInputs(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{name: "oss", url: "https://cdn.example.com/uploads/video-audio/audio.wav"}
	svcs = &Services{Store: store, VideoASRSvc: service.NewVideoASRServiceWithClient(nil, store)}

	tests := []struct {
		name string
		args string
		want string
	}{
		{"unsupported purpose", `{"purpose":"avatar","filename":"a.wav","content_type":"audio/wav"}`, "unsupported upload purpose"},
		{"missing filename", `{"purpose":"video_audio","content_type":"audio/wav"}`, "filename is required"},
		{"non audio", `{"purpose":"video_audio","filename":"a.png","content_type":"image/png"}`, "audio content_type is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tt.args)},
			}
			result, err := prepareFileUploadHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("prepareFileUploadHandler returned error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected error")
			}
			if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, tt.want) {
				t.Fatalf("error = %q, want %q", text, tt.want)
			}
		})
	}
}

func TestCreateVideoASRTaskHandlerRejectsMissingAudioSourceAndFilePath(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{VideoASRSvc: service.NewVideoASRServiceWithClient(&fakeMCPVideoASRClient{}, nil)}

	tests := []struct {
		name string
		args string
		want string
	}{
		{"missing audio source", `{}`, "audio_key or audio_url is required"},
		{"local file path removed", `{"file_path":"/tmp/audio.wav"}`, "file_path is no longer supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tt.args)},
			}
			result, err := createVideoASRTaskHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("createVideoASRTaskHandler returned error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected error")
			}
			if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, tt.want) {
				t.Fatalf("error = %q, want %q", text, tt.want)
			}
		})
	}
}

func TestQueryVideoASRTaskHandlerRejectsUnknownTaskID(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{VideoASRSvc: service.NewVideoASRServiceWithClient(&fakeMCPVideoASRClient{}, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"task_id":"missing"}`)},
	}
	result, err := queryVideoASRTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("queryVideoASRTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected unknown task_id to be rejected")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "was not found") {
		t.Fatalf("unexpected error text: %q", text)
	} else if strings.Contains(text, "local") {
		t.Fatalf("query error should not mention local-only ASR: %q", text)
	}
}

func TestPackVideoTranscriptsHandlerReturnsMarkdown(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"transcripts":{
				"take-a":{"words":[
					{"type":"word","text":"第一","start":0,"end":0.2,"speaker_id":"speaker_0"},
					{"type":"spacing","start":0.2,"end":0.8},
					{"type":"word","text":"第二","start":0.8,"end":1.0,"speaker_id":"speaker_0"}
				]}
			}
		}`)},
	}
	result, err := packVideoTranscriptsHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("packVideoTranscriptsHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"takes_packed_markdown", "## take-a", "[000.00-000.20] S0 第一", "[000.80-001.00] S0 第二"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

type fakeMCPVideoASRClient struct {
	result *service.VideoASRTaskResult
}

func (f *fakeMCPVideoASRClient) Transcribe(context.Context, service.VideoASRTaskRequest) (*service.VideoASRTaskResult, error) {
	return f.result, nil
}
