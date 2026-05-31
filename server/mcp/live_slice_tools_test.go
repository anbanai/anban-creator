package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/service"
)

func TestLiveSliceHandlersValidateMissingServiceAndArgs(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	}

	result, err := uploadLiveAudioHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("uploadLiveAudioHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when service is missing")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "live slice service not available") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestRecognizeLiveSegmentsHandlerRequiresSentences(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: nil}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"ask":"找产品卖点"}`)},
	}

	result, err := recognizeLiveSegmentsHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("recognizeLiveSegmentsHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "live slice service not available") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestBuildLiveClipPlanHandlerRequiresArguments(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"sentences":[]}`)},
	}

	result, err := buildLiveClipPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipPlanHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "video_path is required") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestBuildLiveClipPlanHandlerReturnsCommands(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"video_path":"/tmp/live.mp4",
			"output_dir":"output/live-slice/task",
			"sentences":[
				{"index":1,"start":0,"end":10,"text":"第一句"},
				{"index":2,"start":10,"end":20,"text":"第二句"}
			],
			"segments":[{"title":"片段","start":1,"end":2}]
		}`)},
	}

	result, err := buildLiveClipPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipPlanHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	var payload struct {
		Clips []struct {
			FastCutShell     string   `json:"fast_cut_shell"`
			AccurateCutShell string   `json:"accurate_cut_shell"`
			FastCutArgs      []string `json:"fast_cut_args"`
		} `json:"clips"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(payload.Clips) != 1 {
		t.Fatalf("clips = %#v", payload.Clips)
	}
	if payload.Clips[0].FastCutShell == "" || payload.Clips[0].AccurateCutShell == "" || len(payload.Clips[0].FastCutArgs) == 0 {
		t.Fatalf("missing command fields: %#v", payload.Clips[0])
	}
}

func TestBuildLiveSubjectClipPlanHandlerReturnsPartsAndConcat(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"video_path":"/tmp/live video.mp4",
			"output_dir":"output/live-slice/task",
			"sentences":[
				{"index":1,"start":0,"end":10,"text":"第一句"},
				{"index":3,"start":20,"end":30,"text":"第三句"}
			],
			"completions":[{
				"title":"重排脚本",
				"thoughts":"爆点前置",
				"sentences":[{"index":3,"text":"第三句"},{"index":1,"text":"第一句"}]
			}]
		}`)},
	}

	result, err := buildLiveSubjectClipPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveSubjectClipPlanHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"\"parts\"", "concat_list_content", "concat_shell", "accurate_cut_shell"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestBuildLiveSubjectClipPlanHandlerAllowsNarrationSentences(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"video_path":"/tmp/live.mp4",
			"output_dir":"output/live-slice/task",
			"sentences":[
				{"index":1,"start":0,"end":10,"text":"第一句"},
				{"index":2,"start":10,"end":20,"text":"第二句"}
			],
			"completions":[{
				"title":"含旁白脚本",
				"sentences":[
					{"index":0,"text":"这里补一句旁白","reason":"开头承接"},
					{"index":1,"text":"第一句"},
					{"index":2,"text":"第二句"}
				]
			}]
		}`)},
	}

	result, err := buildLiveSubjectClipPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveSubjectClipPlanHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"script_notes", "这里补一句旁白"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestBuildLiveSubjectClipPlanHandlerRequiresCompletions(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"video_path":"/tmp/live.mp4",
			"output_dir":"output/live-slice/task",
			"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}]
		}`)},
	}

	result, err := buildLiveSubjectClipPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveSubjectClipPlanHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected completions validation error, got: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "completions is required") {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestBuildLiveClipManifestHandlerReturnsMarkdown(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"source_video":"/tmp/live.mp4",
			"tingwu_task_id":"tw-task-1",
				"analysis_title":"直播复盘",
				"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
				"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
				"clip_results":[{"index":1,"status":"ok","method":"copy","output":"output/live-slice/task/exports/01.mp4","exit_code":0,"size":100,"actual_duration_seconds":10.1}]
			}`)},
	}

	result, err := buildLiveClipManifestHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipManifestHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"clip_manifest", "summary_markdown", "clip_notes_markdown", "markdown_path", "transcript", "听悟任务：tw-task-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestBuildLiveClipManifestHandlerRejectsOutputMismatch(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"source_video":"/tmp/live.mp4",
			"tingwu_task_id":"tw-task-1",
			"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
			"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
			"clip_results":[{"index":1,"status":"ok","method":"copy","output":"output/live-slice/task/exports/wrong.mp4","exit_code":0,"size":100,"actual_duration_seconds":10}]
		}`)},
	}

	result, err := buildLiveClipManifestHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipManifestHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected output mismatch validation error, got: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "output must match planned output") {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestBuildLiveClipManifestHandlerRejectsPartMismatch(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"source_video":"/tmp/live.mp4",
			"tingwu_task_id":"tw-task-1",
			"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"},{"index":2,"start":10,"end":22,"text":"第二句"}],
			"clips":[{
				"index":1,
				"title":"片段",
				"sentence_start":1,
				"sentence_end":2,
				"start":0,
				"end":22,
				"duration":22,
				"output":"output/live-slice/task/exports/01.mp4",
				"parts":[
					{"part_index":1,"sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/.parts/01-part-01.mp4"},
					{"part_index":2,"sentence_start":2,"sentence_end":2,"start":10,"end":22,"duration":12,"output":"output/live-slice/task/exports/.parts/01-part-02.mp4"}
				]
			}],
			"clip_results":[{
				"index":1,
				"status":"ok",
				"method":"concat",
				"output":"output/live-slice/task/exports/01.mp4",
				"exit_code":0,
				"size":100,
				"actual_duration_seconds":22,
				"part_results":[
					{"part_index":1,"status":"ok","method":"encode","output":"output/live-slice/task/exports/.parts/wrong.mp4","exit_code":0,"size":100,"actual_duration_seconds":10},
					{"part_index":2,"status":"ok","method":"encode","output":"output/live-slice/task/exports/.parts/01-part-02.mp4","exit_code":0,"size":100,"actual_duration_seconds":12}
				]
			}]
		}`)},
	}

	result, err := buildLiveClipManifestHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipManifestHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected part mismatch validation error, got: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "part output must match planned output") {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestBuildLiveClipManifestHandlerRejectsMissingActualDuration(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"source_video":"/tmp/live.mp4",
			"tingwu_task_id":"tw-task-1",
			"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
			"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
			"clip_results":[{"index":1,"status":"ok","method":"copy","output":"output/live-slice/task/exports/01.mp4","exit_code":0,"size":100}]
		}`)},
	}

	result, err := buildLiveClipManifestHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildLiveClipManifestHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected actual duration validation error, got: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "actual_duration_seconds must be positive") {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestBuildLiveClipManifestHandlerReturnsStrictValidationErrors(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{LiveSliceSvc: service.NewLiveSliceServiceWithClients(nil, nil, nil, nil)}

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing result",
			body: `{
				"source_video":"/tmp/live.mp4",
				"tingwu_task_id":"tw-task-1",
				"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
				"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
				"clip_results":[{"index":2,"status":"failed","error":"extra"}]
			}`,
			want: "missing clip_results for clip index 1",
		},
		{
			name: "duplicate result",
			body: `{
				"source_video":"/tmp/live.mp4",
				"tingwu_task_id":"tw-task-1",
				"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
				"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
				"clip_results":[{"index":1,"status":"failed","error":"first"},{"index":1,"status":"failed","error":"second"}]
			}`,
			want: "duplicate clip_results index 1",
		},
		{
			name: "extra result",
			body: `{
				"source_video":"/tmp/live.mp4",
				"tingwu_task_id":"tw-task-1",
				"sentences":[{"index":1,"start":0,"end":10,"text":"第一句"}],
				"clips":[{"index":1,"title":"片段","sentence_start":1,"sentence_end":1,"start":0,"end":10,"duration":10,"output":"output/live-slice/task/exports/01.mp4"}],
				"clip_results":[{"index":1,"status":"failed","error":"planned"},{"index":2,"status":"failed","error":"extra"}]
			}`,
			want: "clip_results index 2 does not match any clip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tt.body)},
			}

			result, err := buildLiveClipManifestHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("buildLiveClipManifestHandler returned error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected strict validation error, got: %s", result.Content[0].(*mcp.TextContent).Text)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tt.want) {
				t.Fatalf("error missing %q: %s", tt.want, text)
			}
		})
	}
}
