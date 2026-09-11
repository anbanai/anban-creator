package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func TestGenerateImageToolDescriptionUsesTerminalFileSemantics(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	registerImageTools(server)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range result.Tools {
		if tool.Name != "generate_image" {
			continue
		}
		for _, want := range []string{
			"registers the generated task file to the current execution",
			"Terminal file collection occurs after the final workspace manifest and terminal finalization",
			"download_url is the immediate durable handle",
		} {
			if !strings.Contains(tool.Description, want) {
				t.Fatalf("generate_image description missing %q: %s", want, tool.Description)
			}
		}
		if strings.Contains(tool.Description, "list_task_files returns it immediately") {
			t.Fatalf("generate_image description recommends runtime task-file polling: %s", tool.Description)
		}
		return
	}
	t.Fatal("generate_image tool not registered")
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value, ok := value.(string); ok && value == want {
			return true
		}
	}
	return false
}

func callToolText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(*mcp.TextContent); ok {
		return content.Text
	}
	return ""
}

func TestGenerateImageSchemaDoesNotExposeModelSelection(t *testing.T) {
	schema := generateImageInputSchema()
	properties := schema["properties"].(map[string]any)
	for _, removed := range []string{
		"image_capability_key", "operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn",
	} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("generate_image schema exposes %q", removed)
		}
	}
	for _, key := range []string{"ref_image_path", "ref_image_paths"} {
		property := properties[key].(map[string]any)
		description, _ := property["description"].(string)
		if strings.Contains(strings.ToLower(description), "server-local") || strings.Contains(strings.ToLower(description), "absolute") {
			t.Fatalf("%s description permits server-local paths: %q", key, description)
		}
		for _, provider := range []string{"OpenAI", "Gemini", "Volcengine", "Seedream"} {
			if strings.Contains(description, provider) {
				t.Fatalf("%s description is provider-specific: %q", key, description)
			}
		}
	}
	required := schema["required"].([]any)
	for _, name := range []string{"project_id", "task_id", "prompt", "output_path", "aspect_ratio"} {
		if !containsAnyString(required, name) {
			t.Fatalf("generate_image schema must require %s, got %#v", name, required)
		}
	}
}

func TestCropImageSchemaIsAtomicAndTaskRelative(t *testing.T) {
	schema := cropImageInputSchema()
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"task_id", "input_path", "output_path", "target_width", "target_height", "anchor"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("crop_image schema missing %s", key)
		}
	}
	for _, forbidden := range []string{"project_id", "platform", "image_type", "provider", "model"} {
		if _, ok := properties[forbidden]; ok {
			t.Fatalf("crop_image schema exposes business routing field %s", forbidden)
		}
	}
	anchor := properties["anchor"].(map[string]any)
	if values, ok := anchor["enum"].([]any); !ok || !containsAnyString(values, "center") || !containsAnyString(values, "top") {
		t.Fatalf("crop anchors = %#v", anchor["enum"])
	}
}

func TestDownloadAndCompressImageSchemasAreExecutionScopedAndTaskRelative(t *testing.T) {
	download := downloadImageInputSchema()
	downloadProperties := download["properties"].(map[string]any)
	for _, key := range []string{"project_id", "task_id", "url", "output_path"} {
		if _, ok := downloadProperties[key]; !ok {
			t.Fatalf("download_image schema missing %s", key)
		}
		if !containsAnyString(download["required"].([]any), key) {
			t.Fatalf("download_image schema must require %s", key)
		}
	}

	compress := compressImageInputSchema()
	compressProperties := compress["properties"].(map[string]any)
	for _, key := range []string{"task_id", "input_path", "output_path", "max_width"} {
		if _, ok := compressProperties[key]; !ok {
			t.Fatalf("compress_image schema missing %s", key)
		}
	}
	for _, key := range []string{"task_id", "input_path", "output_path"} {
		if !containsAnyString(compress["required"].([]any), key) {
			t.Fatalf("compress_image schema must require %s", key)
		}
	}
	if _, ok := compressProperties["file_path"]; ok {
		t.Fatal("compress_image schema still exposes ambiguous server-local file_path")
	}
}

func TestImageOperationHandlersRejectMissingRequestParametersWithoutPanicking(t *testing.T) {
	old := svcs
	svcs = &Services{TaskImageOperationsSvc: service.NewTaskImageOperationsService(nil, nil, nil, nil, service.TaskImageOperationsConfig{}, nil)}
	t.Cleanup(func() { svcs = old })

	handlers := []struct {
		name    string
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
	}{
		{name: "upload", handler: uploadImageHandler},
		{name: "compress", handler: compressImageHandler},
		{name: "download", handler: downloadImageHandler},
		{name: "analyze", handler: analyzeImageHandler},
	}
	requests := []struct {
		name string
		req  *mcp.CallToolRequest
	}{
		{name: "nil request", req: nil},
		{name: "nil params", req: &mcp.CallToolRequest{}},
	}
	for _, handler := range handlers {
		for _, request := range requests {
			t.Run(handler.name+"/"+request.name, func(t *testing.T) {
				var recovered any
				var result *mcp.CallToolResult
				var err error
				func() {
					defer func() { recovered = recover() }()
					result, err = handler.handler(context.Background(), request.req)
				}()
				if recovered != nil {
					t.Fatalf("handler panicked for missing parameters: %v", recovered)
				}
				if err != nil || result == nil || !result.IsError {
					t.Fatalf("handler result = %#v, err = %v; want tool error", result, err)
				}
			})
		}
	}
}

func TestCategorizeImageGenFailureDetectsFilesystemErrors(t *testing.T) {
	err := fmt.Errorf("create output directory: %w", os.ErrPermission)

	if got := categorizeImageGenFailure(err, ""); got != "filesystem" {
		t.Fatalf("categorizeImageGenFailure() = %q, want filesystem", got)
	}
}

func TestClassifyImageToolFailurePreservesBusinessRatioDetails(t *testing.T) {
	err := &service.ImageRatioNotAllowedError{
		RequestedRatio:     "16:9",
		AllowedImageRatios: []string{"3:4", "1:1", "4:3"},
		TaskImageRatio:     "3:4",
	}
	failure := classifyImageToolFailure(context.Background(), context.Background(), err, "generate", time.Minute, false)
	if failure.Code != "image_ratio_not_allowed" {
		t.Fatalf("failure code = %q", failure.Code)
	}
	if failure.RequestedRatio != "16:9" || !slices.Equal(failure.AllowedImageRatios, []string{"3:4", "1:1", "4:3"}) || failure.TaskImageRatio != "3:4" {
		t.Fatalf("failure ratio details = %#v", failure)
	}
	if !errors.As(err, new(*service.ImageRatioNotAllowedError)) {
		t.Fatal("test error no longer exposes the typed capability failure")
	}
}
