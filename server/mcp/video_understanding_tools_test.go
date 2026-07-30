package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

type fakeTaskVideoOperationsService struct {
	req service.AnalyzeTaskVideoRequest
}

func (f *fakeTaskVideoOperationsService) Analyze(_ context.Context, req service.AnalyzeTaskVideoRequest) (*service.AnalyzeTaskVideoResult, error) {
	f.req = req
	return &service.AnalyzeTaskVideoResult{Analysis: "analysis"}, nil
}

func TestAnalyzeVideoToolIsRegisteredWithoutLegacyAlias(t *testing.T) {
	names := listToolNames(t, registerVideoUnderstandingTools)
	if !names["analyze_video"] {
		t.Fatal("analyze_video is not registered")
	}
	if names["analyze_video_reference"] {
		t.Fatal("legacy alias must not be registered")
	}
}

func TestAnalyzeVideoHandlerPassesAtomicRequest(t *testing.T) {
	old := svcs
	fake := &fakeTaskVideoOperationsService{}
	svcs = &Services{TaskVideoOperationsSvc: fake}
	t.Cleanup(func() { svcs = old })

	arguments, err := json.Marshal(map[string]any{
		"project_id": "project-1", "task_id": "task-1", "task_file_id": "file-1", "prompt": "分析完整视频",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := analyzeVideoHandler(withMCPUserID(context.Background(), "user-1"), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("result=%#v", result)
	}
	if fake.req.UserID != "user-1" || fake.req.ProjectID != "project-1" || fake.req.TaskID != "task-1" || fake.req.TaskFileID != "file-1" || fake.req.Prompt != "分析完整视频" {
		t.Fatalf("request=%#v", fake.req)
	}
}
