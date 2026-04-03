package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appimage "github.com/royalrick/anbanwriter/app/image"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// createFileTools builds the image generation, upload, compression, and file I/O tools.
func createFileTools(env *toolEnv) []*claudecode.McpTool {
	// --- Tool: generate_image ---
	generateImageTool := claudecode.NewTool(
		"generate_image",
		"Generate an AI image with a text prompt. Use this for content images (not covers).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Image generation prompt (Chinese preferred)"},
				"size":   map[string]any{"type": "string", "description": "Image size in WIDTHxHEIGHT format, e.g. 1728x2304", "default": "1728x2304"},
				"output": map[string]any{"type": "string", "description": "Output file path relative to work directory, e.g. cover.png"},
			},
			"required": []string{"prompt"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			size := "1728x2304"
			if s, ok := args["size"].(string); ok && s != "" {
				size = s
			}
			output := fmt.Sprintf("img_%d.png", time.Now().UnixNano())
			if o, ok := args["output"].(string); ok && o != "" {
				output = o
			}
			outputPath := filepath.Join(env.workDir, output)

			processor := appimage.NewProcessor(env.cfg, env.apiCfg, env.zapLog)
			result, err := processor.GenerateOnlyWithSize(prompt, size, outputPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image generated: %s (size: %s)", result.FilePath, result.Size),
				}},
			}, nil
		},
	)

	// --- Tool: generate_cover_image ---
	generateCoverTool := claudecode.NewTool(
		"generate_cover_image",
		"Generate a cover image for the content. Use the cover image API config (larger size, higher quality).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Cover image generation prompt"},
				"size":   map[string]any{"type": "string", "description": "Image size, e.g. 2560x1440 for articles or 1728x2304 for vertical", "default": "2560x1440"},
				"output": map[string]any{"type": "string", "description": "Output file path relative to work directory, e.g. cover.png"},
			},
			"required": []string{"prompt"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			size := "2560x1440"
			if s, ok := args["size"].(string); ok && s != "" {
				size = s
			}
			output := "cover.png"
			if o, ok := args["output"].(string); ok && o != "" {
				output = o
			}
			outputPath := filepath.Join(env.workDir, output)

			processor := appimage.NewProcessor(env.cfg, env.coverApiCfg, env.zapLog)
			result, err := processor.GenerateOnlyWithSize(prompt, size, outputPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_cover_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Cover image generated: %s (size: %s)", result.FilePath, result.Size),
				}},
			}, nil
		},
	)

	// --- Tool: generate_batch_images ---
	generateBatchTool := claudecode.NewTool(
		"generate_batch_images",
		"Generate a batch of AI images with a shared prompt. All images are saved to the work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Image generation prompt (Chinese preferred)"},
				"count":  map[string]any{"type": "integer", "description": "Number of images to generate", "default": 4},
			},
			"required": []string{"prompt", "count"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			var count int
			switch v := args["count"].(type) {
			case float64:
				count = int(v)
			case int:
				count = v
			}
			if count <= 0 || count > 20 {
				count = 4
			}

			outputDir := env.workDir
			processor := appimage.NewProcessor(env.cfg, env.apiCfg, env.zapLog)
			results, err := processor.GenerateBatchOnly(prompt, count, outputDir)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_batch_images failed: %v", err)}},
					IsError: true,
				}, nil
			}

			var lines []string
			for _, r := range results {
				lines = append(lines, fmt.Sprintf("Image %d: %s (size: %s)", r.Index, r.FilePath, r.Size))
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Generated %d images:\n%s", len(results), strings.Join(lines, "\n")),
				}},
			}, nil
		},
	)

	// --- Tool: upload_image ---
	uploadImageTool := claudecode.NewTool(
		"upload_image",
		"Upload a local image to WeChat CDN. Returns media_id and wechat_url.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path to the image file (absolute or relative to work directory)"},
			},
			"required": []string{"file_path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			filePath := args["file_path"].(string)
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(env.workDir, filePath)
			}

			processor := appimage.NewProcessor(env.cfg, env.apiCfg, env.zapLog)
			result, err := processor.UploadLocalImage(filePath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("upload_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image uploaded. media_id: %s, wechat_url: %s", result.MediaID, result.WechatURL),
				}},
			}, nil
		},
	)

	// --- Tool: compress_image ---
	compressImageTool := claudecode.NewTool(
		"compress_image",
		"Compress an image file to reduce its size while preserving quality.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path to the image file"},
			},
			"required": []string{"file_path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			filePath := args["file_path"].(string)
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(env.workDir, filePath)
			}

			processor := appimage.NewProcessor(env.cfg, env.apiCfg, env.zapLog)
			resultPath, compressed, err := processor.CompressImage(filePath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("compress_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			if !compressed {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: "Image already within size limits, no compression needed."}},
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image compressed: %s", resultPath),
				}},
			}, nil
		},
	)

	// --- Tool: save_file ---
	saveFileTool := claudecode.NewTool(
		"save_file",
		"Write content to a file in the work directory. Creates parent directories if needed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path relative to work directory, e.g. content.md"},
				"content": map[string]any{"type": "string", "description": "Content to write to the file"},
			},
			"required": []string{"path", "content"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			path := args["path"].(string)
			content := args["content"].(string)

			fullPath, err := safePath(env.workDir, path)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("access denied: %v", err)}},
					IsError: true,
				}, nil
			}
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("mkdir failed: %v", err)}},
					IsError: true,
				}, nil
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("write failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("File saved: %s (%d bytes)", fullPath, len(content)),
				}},
			}, nil
		},
	)

	// --- Tool: read_file ---
	readFileTool := claudecode.NewTool(
		"read_file",
		"Read content from a file in the work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to work directory"},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			path := args["path"].(string)
			fullPath, err := safePath(env.workDir, path)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("access denied: %v", err)}},
					IsError: true,
				}, nil
			}

			data, err := os.ReadFile(fullPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("read failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: string(data),
				}},
			}, nil
		},
	)

	// --- Tool: list_files ---
	listFilesTool := claudecode.NewTool(
		"list_files",
		"List files and directories in the work directory (or a subdirectory).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Subdirectory path relative to work directory (empty for root)"},
			},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			dir := env.workDir
			if p, ok := args["path"].(string); ok && p != "" {
				safeDir, err := safePath(env.workDir, p)
				if err != nil {
					return &claudecode.McpToolResult{
						Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("access denied: %v", err)}},
						IsError: true,
					}, nil
				}
				dir = safeDir
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("list failed: %v", err)}},
					IsError: true,
				}, nil
			}

			var lines []string
			for _, e := range entries {
				if e.IsDir() {
					lines = append(lines, e.Name()+"/")
				} else {
					info, _ := e.Info()
					size := ""
					if info != nil {
						size = fmt.Sprintf(" (%d bytes)", info.Size())
					}
					lines = append(lines, e.Name()+size)
				}
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: strings.Join(lines, "\n"),
				}},
			}, nil
		},
	)

	return []*claudecode.McpTool{
		generateImageTool,
		generateCoverTool,
		generateBatchTool,
		uploadImageTool,
		compressImageTool,
		saveFileTool,
		readFileTool,
		listFilesTool,
	}
}
