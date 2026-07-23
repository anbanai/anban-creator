package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
		"image_model_key", "operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn",
	} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("generate_image schema exposes %q", removed)
		}
	}
	for _, key := range []string{"ref_image_path", "ref_image_paths"} {
		property := properties[key].(map[string]any)
		description, _ := property["description"].(string)
		for _, provider := range []string{"OpenAI", "Gemini", "Volcengine", "Seedream"} {
			if strings.Contains(description, provider) {
				t.Fatalf("%s description is provider-specific: %q", key, description)
			}
		}
	}
	required := schema["required"].([]any)
	for _, name := range []string{"project_id", "task_id", "prompt", "output_path"} {
		if !containsAnyString(required, name) {
			t.Fatalf("generate_image schema must require %s, got %#v", name, required)
		}
	}
}

func TestCategorizeImageGenFailureDetectsFilesystemErrors(t *testing.T) {
	err := fmt.Errorf("create output directory: %w", os.ErrPermission)

	if got := categorizeImageGenFailure(err, ""); got != "filesystem" {
		t.Fatalf("categorizeImageGenFailure() = %q, want filesystem", got)
	}
}
