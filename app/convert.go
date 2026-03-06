package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/converter"
	"github.com/royalrick/wechatwriter/app/draft"
	"github.com/royalrick/wechatwriter/app/image"
	"github.com/royalrick/wechatwriter/app/wechat"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// convertCmd convert 命令
var convertCmd = &cobra.Command{
	Use:   "convert <markdown_file>",
	Short: "Convert Markdown to WeChat HTML",
	Long: `Convert Markdown article to WeChat Official Account formatted HTML.

Two-step workflow using Claude agent:
  Step 1: Run without --ai-html to get the prompt in JSON output.
          Claude agent reads the prompt, generates HTML, then provides it back.
  Step 2: Run with --ai-html <html_file> to complete conversion with the generated HTML.

Supported themes:
  - autumn-warm, spring-fresh, ocean-calm, custom`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runConvert(cmd, args); err != nil {
			responseError(err)
		}
	},
}

// convert 命令参数
var (
	convertTheme        string
	convertCustomPrompt string
	convertPreview      bool
	convertUpload       bool
	convertDraft        bool
	convertSaveDraft    string
	convertCoverImage   string // 封面图片路径
	convertAIHTML       string // Step 2: agent 生成的 HTML 文件路径
	convertOutput       string // 输出文件路径
	convertImageURLs    string // JSON 文件，包含图片索引到 CDN URL 的映射
)

func init() {
	// 添加 flags
	convertCmd.Flags().StringVar(&convertTheme, "theme", "", "Theme name (default: from config or 'default')")
	convertCmd.Flags().StringVar(&convertCustomPrompt, "custom-prompt", "", "Custom AI prompt")
	convertCmd.Flags().BoolVar(&convertPreview, "preview", false, "Preview only, do not upload images")
	convertCmd.Flags().BoolVar(&convertUpload, "upload", false, "Upload images to WeChat and replace URLs")
	convertCmd.Flags().BoolVar(&convertDraft, "draft", false, "Create WeChat draft after conversion")
	convertCmd.Flags().StringVar(&convertSaveDraft, "save-draft", "", "Save draft JSON to file")
	convertCmd.Flags().StringVar(&convertCoverImage, "cover", "", "Cover image path for draft (required when using --draft)")
	convertCmd.Flags().StringVar(&convertAIHTML, "ai-html", "", "Path to agent-generated HTML file (Step 2 of two-step workflow)")
	convertCmd.Flags().StringVarP(&convertOutput, "output", "o", "", "Output HTML file path")
	convertCmd.Flags().StringVar(&convertImageURLs, "image-urls", "", "JSON file mapping image indexes to CDN URLs for placeholder replacement")
}

// runConvert 执行转换
func runConvert(cmd *cobra.Command, args []string) error {
	// 默认加载轻量级配置（不验证微信账号）
	if err := initConfigMinimal(); err != nil {
		return fmt.Errorf("初始化配置失败: %w", err)
	}

	// 回退到配置文件默认值
	if !cmd.Flags().Changed("theme") && cfg.Wechat.Article.Theme != "" {
		convertTheme = cfg.Wechat.Article.Theme
	}

	// 如果仍然为空，使用默认值
	if convertTheme == "" {
		convertTheme = config.DefaultArticleTheme
	}

	markdownFile := args[0]

	log.Info("starting conversion",
		zap.String("file", markdownFile),
		zap.String("theme", convertTheme))

	// 读取 Markdown 文件
	markdown, err := os.ReadFile(markdownFile)
	if err != nil {
		return fmt.Errorf("read markdown file: %w", err)
	}

	// 创建转换器
	conv := converter.NewConverter(log)

	// 构建转换请求
	req := &converter.ConvertRequest{
		Markdown:     string(markdown),
		Theme:        convertTheme,
		CustomPrompt: convertCustomPrompt,
	}

	// 执行转换
	result := conv.Convert(req)

	// Step 2: --ai-html 提供了 agent 生成的 HTML，完成转换
	if convertAIHTML != "" {
		htmlBytes, err := os.ReadFile(convertAIHTML)
		if err != nil {
			return fmt.Errorf("read ai-html file: %w", err)
		}

		_, images, ok := converter.GetAIRequestInfo(result)
		if !ok {
			// 如果不是 AI 请求结果，直接使用 HTML
			images = result.Images
		}

		result = converter.CompleteAIConversion(string(htmlBytes), images, convertTheme)

		// Populate placeholder fields on all images (fixes root bug where Placeholder was empty)
		for i := range result.Images {
			result.Images[i].Placeholder = fmt.Sprintf("<!-- IMG:%d -->", i)
		}

		// If --image-urls provided, load CDN URLs and replace placeholders
		if convertImageURLs != "" {
			urls, err := loadImageURLs(convertImageURLs)
			if err != nil {
				return fmt.Errorf("load image URLs: %w", err)
			}
			for i := range result.Images {
				if i < len(urls) && urls[i] != "" {
					result.Images[i].WechatURL = urls[i]
				}
			}
			result.HTML = converter.ReplaceImagePlaceholders(result.HTML, result.Images)
		}
	} else if converter.IsAIRequest(result) {
		// Step 1: 返回提示词给 Claude 代理处理
		prompt, images, ok := converter.GetAIRequestInfo(result)
		if !ok {
			return fmt.Errorf("invalid AI request result")
		}

		// 序列化图片列表
		type imageInfo struct {
			Index    int    `json:"index"`
			Type     string `json:"type"`
			Original string `json:"original"`
		}
		var imageList []imageInfo
		for _, img := range images {
			imageList = append(imageList, imageInfo{
				Index:    img.Index,
				Type:     string(img.Type),
				Original: img.Original,
			})
		}

		log.Debug("returning AI prompt for agent",
			zap.Int("count", len(images)),
			zap.Int("prompt_length", len(prompt)))

		responseSuccess(map[string]any{
			"type":   "convert_prompt",
			"prompt": prompt,
			"theme":  convertTheme,
			"images": imageList,
		})
		return nil
	}

	if !result.Success {
		return fmt.Errorf("conversion failed: %s", result.Error)
	}

	log.Info("conversion completed",
		zap.String("theme", result.Theme),
		zap.Int("count", len(result.Images)))

	// 处理图片（需要微信配置）
	if convertUpload || convertDraft {
		// 需要微信功能，加载完整配置（包含微信账号验证）
		if err := initConfig(); err != nil {
			return fmt.Errorf("加载微信配置失败: %w\n提示: 运行 'wechatwriter account init' 配置微信账号", err)
		}

		if err := processImages(result); err != nil {
			log.Warn("image processing failed", zap.Error(err))
		}
	}

	// 输出结果
	if convertSaveDraft != "" {
		if err := saveDraft(result); err != nil {
			return fmt.Errorf("save draft: %w", err)
		}
	}

	if convertDraft {
		if err := createWeChatDraft(result, convertCoverImage); err != nil {
			return fmt.Errorf("create draft: %w", err)
		}
	}

	// 输出 HTML
	outputHTML(result.HTML, convertOutput, convertPreview)

	// --ai-html 路径：输出 JSON 元数据（字符数、图片替换状态）
	if convertAIHTML != "" && convertOutput != "" {
		charCount := len(result.HTML)
		imagesReplaced := 0
		for _, img := range result.Images {
			if img.WechatURL != "" {
				imagesReplaced++
			}
		}

		resp := map[string]any{
			"type":            "convert_result",
			"file":            convertOutput,
			"char_count":      charCount,
			"max_chars":       20000,
			"images_replaced": imagesReplaced,
			"images_total":    len(result.Images),
		}
		if charCount > 20000 {
			resp["size_warning"] = fmt.Sprintf(
				"HTML content is %d characters, exceeds WeChat limit of 20000. Suggestions: remove HTML comments (<!-- ... -->), simplify inline CSS (remove radial-gradient, background-image), remove <!DOCTYPE>/head/body wrapper, reduce content length.",
				charCount,
			)
		}
		responseSuccess(resp)
	}

	return nil
}

// processImages 处理图片上传
func processImages(result *converter.ConvertResult) error {
	if len(result.Images) == 0 {
		log.Info("no images to process")
		return nil
	}

	processor := image.NewProcessor(cfg, &cfg.Wechat.Article.Content.Image, log)

	for i, imgRef := range result.Images {
		log.Info("processing image",
			zap.Int("index", i),
			zap.String("type", string(imgRef.Type)),
			zap.String("original", imgRef.Original))

		var uploadResult *image.UploadResult
		var err error

		switch imgRef.Type {
		case converter.ImageTypeLocal:
			uploadResult, err = processor.UploadLocalImage(imgRef.Original)
		case converter.ImageTypeOnline:
			uploadResult, err = processor.DownloadAndUpload(imgRef.Original)
		case converter.ImageTypeAI:
			// AI 生成的图片需要先调用生成 API
			genResult, genErr := processor.GenerateAndUpload(imgRef.AIPrompt)
			if genErr != nil {
				err = genErr
			} else {
				uploadResult = &image.UploadResult{
					MediaID:   genResult.MediaID,
					WechatURL: genResult.WechatURL,
				}
			}
		}

		if err != nil {
			log.Warn("image upload failed",
				zap.Int("index", i),
				zap.Error(err))
			continue
		}

		// 更新图片 URL
		result.Images[i].WechatURL = uploadResult.WechatURL

		log.Info("image uploaded",
			zap.Int("index", i),
			zap.String("media_id", maskMediaID(uploadResult.MediaID)),
			zap.String("wechat_url", uploadResult.WechatURL))
	}

	// 替换 HTML 中的图片占位符
	result.HTML = converter.ReplaceImagePlaceholders(result.HTML, result.Images)

	return nil
}

// saveDraft 保存草稿 JSON 到文件
func saveDraft(result *converter.ConvertResult) error {
	articles := []draft.Article{
		{
			Title:   "Draft Article", // TODO: 从 markdown 提取标题
			Content: result.HTML,
		},
	}

	draftData := map[string]any{
		"articles": articles,
	}

	jsonData, err := json.MarshalIndent(draftData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal draft: %w", err)
	}

	if err := os.WriteFile(convertSaveDraft, jsonData, 0644); err != nil {
		return fmt.Errorf("write draft file: %w", err)
	}

	log.Info("draft saved", zap.String("file", convertSaveDraft))
	return nil
}

// createWeChatDraft 创建微信草稿
func createWeChatDraft(result *converter.ConvertResult, coverImagePath string) error {
	svc := draft.NewService(cfg, log)

	// 检查封面图片（微信要求必须有封面图）
	if coverImagePath == "" {
		return &DraftError{
			Message: "创建草稿需要封面图片",
			HintMsg: "请使用 --cover 参数指定封面图片路径，例如: --cover /path/to/cover.jpg\n" +
				"或者先上传封面图片到微信素材库: writer upload_image /path/to/cover.jpg",
		}
	}

	// 上传封面图片到微信素材库
	log.Info("uploading cover image", zap.String("path", coverImagePath))
	coverMediaID, err := uploadCoverImage(coverImagePath)
	if err != nil {
		return fmt.Errorf("上传封面图片失败: %w", err)
	}
	log.Info("cover image uploaded", zap.String("media_id", maskMediaID(coverMediaID)))

	// 提取标题（TODO: 从 markdown frontmatter 或第一个标题获取）
	title := "Article Title"

	draftResult, err := svc.CreateDraft([]draft.Article{
		{
			Title:        title,
			Content:      result.HTML,
			Digest:       draft.GenerateDigestFromContent(result.HTML, 120),
			ThumbMediaID: coverMediaID,
			ShowCoverPic: 1, // 显示封面
		},
	})

	if err != nil {
		return fmt.Errorf("create draft: %w", err)
	}

	log.Info("article draft created",
		zap.String("media_id", maskMediaID(draftResult.MediaID)),
		zap.String("draft_url", draftResult.DraftURL))

	return nil
}

// uploadCoverImage 上传封面图片到微信素材库
func uploadCoverImage(imagePath string) (string, error) {
	svc := wechat.NewService(cfg, log)
	result, err := svc.UploadMaterial(imagePath)
	if err != nil {
		return "", err
	}
	return result.MediaID, nil
}

// DraftError 草稿错误
type DraftError struct {
	Message string
	HintMsg string
}

func (e *DraftError) Error() string {
	msg := fmt.Sprintf("草稿错误: %s", e.Message)
	if e.HintMsg != "" {
		msg += fmt.Sprintf("\n💡 提示:\n   %s", e.HintMsg)
	}
	return msg
}

func (e *DraftError) Hint() string { return e.HintMsg }

// outputHTML 输出 HTML
func outputHTML(html, outputPath string, preview bool) {
	if preview || outputPath == "" {
		// 预览模式或未指定输出，输出到标准输出
		fmt.Println("\n=== HTML Output ===")
		fmt.Println(html)
		fmt.Println("\n=== End ===")
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(html), 0644); err != nil {
			log.Error("failed to write output file", zap.Error(err))
		} else {
			log.Info("html saved", zap.String("file", outputPath))
		}
	}
}

// loadImageURLs 从 JSON 文件中加载 CDN URL 列表
// JSON 格式为字符串数组，索引对应图片索引
func loadImageURLs(filePath string) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var urls []string
	if err := json.Unmarshal(data, &urls); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return urls, nil
}
