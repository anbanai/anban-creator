package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/service"
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

func TestUploadVideoAudioRejectsPrivateStorageURL(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{name: "oss", url: "/api/v1/files/uploads/video-audio/audio.wav"}
	svcs = &Services{Store: store, VideoASRSvc: service.NewVideoASRServiceWithClient(nil, store)}

	audioPath := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake-wav"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"file_path":` + strconv.Quote(audioPath) + `}`)},
	}
	result, err := uploadVideoAudioHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("uploadVideoAudioHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected private storage URL to be rejected")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "OSS/CDN") || !strings.Contains(text, "HTTPS") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestCreateVideoASRTaskHandlerReturnsTranscript(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{VideoASRSvc: service.NewVideoASRServiceWithClient(&fakeMCPVideoASRClient{
		result: &service.VideoASRTaskResult{
			TaskID: "asr-task-1",
			Status: "SUCCEEDED",
			Transcript: &service.VideoTranscript{
				Words: []service.VideoTranscriptWord{{Type: "word", Text: "你好", Start: 0, End: 0.3}},
			},
		},
	}, nil)}

	audioPath := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake-wav"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"file_path":` + strconv.Quote(audioPath) + `}`)},
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

func TestCreateVideoASRTaskHandlerRejectsMissingFilePath(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{VideoASRSvc: service.NewVideoASRServiceWithClient(&fakeMCPVideoASRClient{}, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	}
	result, err := createVideoASRTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoASRTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected missing file_path to be rejected")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "file_path is required") {
		t.Fatalf("unexpected error text: %q", text)
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
