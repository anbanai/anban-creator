package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/converter"
	"github.com/royalrick/anbanwriter/app/image"
	"github.com/royalrick/anbanwriter/app/storage"
	"github.com/spf13/cobra"
)

// imageCmd 图片处理命令组
func imageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "图片处理（上传、下载、生成）",
		Long: `图片处理命令组

支持的操作：
  upload    - 上传本地图片到微信素材库
  download  - 下载在线图片并上传到微信
  generate  - AI 生成图片并保存到本地
  batch     - 批量生成并上传 Markdown 中的 AI 配图`,
	}

	cmd.AddCommand(imageUploadCmd())
	cmd.AddCommand(imageDownloadCmd())
	cmd.AddCommand(imageGenerateCmd())
	cmd.AddCommand(imageBatchCmd())

	return cmd
}

// imageUploadCmd 上传本地图片
func imageUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file_path>",
		Short: "上传本地图片到微信素材库",
		Long: `上传本地图片文件到微信公众号素材库

支持格式: JPG, PNG, GIF, BMP, WebP（微信限制 < 10MB）
上传前自动压缩（配置 image.compress: true 和 image.max_width）

需要配置:
  wechat.appid   - 微信公众号 AppID
  wechat.secret  - 微信公众号 Secret

示例:
  anbanwriter image upload cover.jpg
  anbanwriter image upload ./images/photo.png`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]
			processor := image.NewProcessor(cfg, &cfg.Wechat.Article.Content.Image, log)
			result, err := processor.UploadLocalImage(filePath)
			if err != nil {
				responseError(err)
				return
			}

			// 静默记录到 DB
			if store != nil {
				absPath, _ := filepath.Abs(filePath)
				_ = store.UpsertImageUpload(absPath, result.MediaID, result.WechatURL)
			}

			responseSuccess(result)
		},
	}
}

// imageDownloadCmd 下载并上传图片
func imageDownloadCmd() *cobra.Command {
	var noUpload bool
	var output string

	cmd := &cobra.Command{
		Use:   "download <url>",
		Short: "下载在线图片并上传到微信",
		Long: `下载在线图片，可选上传到微信素材库

默认行为：下载并自动上传到微信素材库
使用 --no-upload 仅下载到本地文件

需要配置（上传时）:
  wechat.appid   - 微信公众号 AppID
  wechat.secret  - 微信公众号 Secret

示例:
  # 下载并上传到微信
  anbanwriter image download https://example.com/photo.jpg

  # 仅下载到本地
  anbanwriter image download https://example.com/photo.jpg --no-upload
  anbanwriter image download https://example.com/photo.jpg --no-upload -o local.jpg`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if noUpload {
				return initConfigMinimal()
			}
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			processor := image.NewProcessor(cfg, &cfg.Wechat.Article.Content.Image, log)

			if noUpload {
				outPath := output
				if outPath == "" {
					outPath = filenameFromURL(url)
				}
				result, err := processor.DownloadOnly(url, outPath)
				if err != nil {
					responseError(err)
					return
				}
				responseSuccess(result)
				return
			}

			result, err := processor.DownloadAndUpload(url)
			if err != nil {
				responseError(err)
				return
			}
			responseSuccess(result)
		},
	}

	cmd.Flags().BoolVar(&noUpload, "no-upload", false, "仅下载到本地，不上传到微信")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出文件路径（配合 --no-upload 使用）")

	return cmd
}

// filenameFromURL 从 URL 提取文件名
func filenameFromURL(rawURL string) string {
	base := filepath.Base(rawURL)
	// 去掉查询参数
	if idx := strings.IndexByte(base, '?'); idx != -1 {
		base = base[:idx]
	}
	if base == "" || base == "." || base == "/" {
		return "image.jpg"
	}
	return base
}

// imageGenerateCmd AI 生成图片
func imageGenerateCmd() *cobra.Command {
	var size string
	var postMode bool
	var stylePrompt string
	var output string
	var upload bool
	var refer string
	var count int
	var variants []string

	cmd := &cobra.Command{
		Use:   "generate <prompt>",
		Short: "AI 生成图片",
		Long: `使用 AI 生成图片并保存到本地文件

支持的图片生成服务提供者（通过 article.image.provider 配置）:
  openai      - OpenAI DALL-E（dall-e-2, dall-e-3）
                尺寸: 比例格式 16:9/9:16/1:1/3:4/4:3 等（DALL-E 内部映射）
  gemini      - Google Gemini（gemini-3-pro-image-preview）
                尺寸: 比例格式 1:1, 16:9, 9:16, 3:4, 4:3 等
  openrouter  - OpenRouter 多模型网关
                尺寸: 比例格式 16:9/3:4 等，可加档位 3:4:1K/3:4:2K
  volcengine  - 火山方舟 Seedream（doubao-seedream-5-0-250128）
                尺寸: 比例格式 1:1, 16:9, 3:4 等，可加档位 3:4:4K

风格控制：
  --style 提供文字风格描述，如"扁平插画风格，莫兰迪色系"
  --ref 提供参考图片路径，生成时会参考该图的视觉风格
  两者可以同时使用，style 提供文字描述，ref 提供视觉参考

Prompt 构建优先级（从高到低）：
  1. CLI --style 参数
  2. 配置文件 image.style_prompt
  3. 预设风格（通过 --post-style 配置）
  4. 无风格（仅使用用户 prompt）

需要配置:
  article.image.key      - 图片 API Key
  article.image.provider - 服务提供者 (openai/gemini/openrouter/volcengine)

示例:
  # 生成图片（自动以时间戳命名，如 generated_image_20060102_150405.png）
  anbanwriter image generate "春天的茶园，阳光明媚"

  # 指定输出文件
  anbanwriter image generate "春天的茶园" -o tea.png

  # 指定尺寸（比例格式，默认 2K 档位）
  anbanwriter image generate "清晨茶园" --size 16:9 -o cover.jpg
  anbanwriter image generate "竖版封面" --size 9:16 --post -o post.jpg

  # 指定比例+档位
  anbanwriter image generate "封面图" --size 3:4:1K -o rednote-cover.jpg
  anbanwriter image generate "封面图" --size 3:4:4K -o cover-4k.jpg

  # 组图模式：一次生成 4 张风格一致的图片到目录
  anbanwriter image generate "小绿书内容图" --post --count 4 -o ./output/images/

  # 组图变体模式：为每张图指定不同内容描述
  anbanwriter image generate "清新扁平风格，莫兰迪色系" \
    --post --count 4 \
    --variants "封面：茶园晨景，阳光洒在茶树上","图2：采茶姑娘手部特写","图3：传统制茶工艺","图4：一杯清香绿茶" \
    -o ./rednote_post_images/

  # 生成后自动上传到微信
  anbanwriter image generate "封面图" --upload`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if upload {
				return initConfig()
			}
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			prompt := args[0]

			// 验证参考图（如果指定）
			if refer != "" {
				if _, err := os.Stat(refer); os.IsNotExist(err) {
					responseError(fmt.Errorf("参考图文件不存在: %s", refer))
					return
				}
				if !image.IsValidImageFormat(refer) {
					responseError(fmt.Errorf("参考图格式不支持: %s（支持格式: JPG, JPEG, PNG, WebP, GIF）", refer))
					return
				}
			}

			// 选择 article 还是 post 的图片配置
			var apiCfg *config.ImageAPI
			if postMode {
				resolved := cfg.ResolvedPostContentImage()
				apiCfg = &resolved
				if size == "" {
					size = cfg.PostImageSize()
				}
			} else {
				apiCfg = &cfg.Wechat.Article.Content.Image
				if size == "" {
					size = cfg.ArticleImageSize()
				}
			}

			processor := image.NewProcessor(cfg, apiCfg, log)
			if refer != "" {
				processor.SetRefImage(refer)
			} else if apiCfg.Refer != "" {
				processor.SetRefImage(apiCfg.Refer)
			}
			if stylePrompt != "" {
				processor.SetStylePrompt(stylePrompt)
			}

			// 解析预设 prompt（post 模式、无显式 --style、有配置预设时）
			postStyle := cfg.Wechat.Post.Style
			if postStyle == "" && cfg.Rednote != nil {
				postStyle = cfg.Rednote.Style
			}
			if postMode && stylePrompt == "" && postStyle != "" {
				pm := image.NewStylePresetManager()
				if err := pm.LoadPresets(); err == nil {
					if preset, err := pm.GetPreset(postStyle); err == nil {
						processor.SetPresetPrompt(preset.Prompt)
					}
				}
			}

			// 组图模式：count > 1 时调用 GenerateBatchOnly 或 GenerateBatchWithVariants
			if count > 1 {
				outputDir := output
				if outputDir == "" {
					outputDir = "."
				}
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					responseError(fmt.Errorf("创建输出目录失败: %w", err))
					return
				}

				var batchResults []*image.BatchImageResult
				var err error

				// 如果提供了 variants，使用 GenerateBatchWithVariants
				if len(variants) > 0 {
					batchResults, err = processor.GenerateBatchWithVariants(prompt, variants, outputDir)
				} else {
					batchResults, err = processor.GenerateBatchOnly(prompt, count, outputDir)
				}

				if err != nil {
					responseError(err)
					return
				}

				var filePaths []string
				for _, r := range batchResults {
					filePaths = append(filePaths, r.FilePath)
				}

				if store != nil {
					for i, r := range batchResults {
						imgRecord := &storage.Image{
							Prompt:      prompt,
							Provider:    apiCfg.Provider,
							LocalPath:   r.FilePath,
							StylePreset: stylePrompt,
							CreatedAt:   time.Now(),
						}
						// 如果有 variants，记录具体变体 prompt
						if len(variants) > 0 && i < len(variants) {
							imgRecord.Prompt = fmt.Sprintf("%s\n\n[变体]: %s", prompt, variants[i])
						}
						if info, infoErr := image.GetImageInfo(r.FilePath); infoErr == nil {
							imgRecord.Width = info.Width
							imgRecord.Height = info.Height
							imgRecord.SizeBytes = info.Size
						}
						_ = store.CreateImage(imgRecord)
					}
				}

				responseSuccess(map[string]any{
					"count":      len(batchResults),
					"file_paths": filePaths,
					"size":       size,
					"results":    batchResults,
				})
				return
			}

			// 单图模式：自动生成时间戳文件名
			if output == "" {
				output = time.Now().Format("generated_image_20060102_150405") + ".png"
			}

			var genResult *image.GenerateOnlyResult
			var err error
			if size != "" {
				genResult, err = processor.GenerateOnlyWithSize(prompt, size, output)
			} else {
				genResult, err = processor.GenerateOnly(prompt, output)
			}
			if err != nil {
				responseError(err)
				return
			}

			// 若显式指定了 size，覆盖 provider 返回的 Size 字段（部分 provider 不返回尺寸）
			if size != "" {
				genResult.Size = size
			}

			// 获取实际像素尺寸（同时供 JSON 输出和 DB 记录使用）
			var imgWidth, imgHeight int
			var imgSizeBytes int64
			if info, infoErr := image.GetImageInfo(genResult.FilePath); infoErr == nil {
				imgWidth = info.Width
				imgHeight = info.Height
				imgSizeBytes = info.Size
				genResult.Width = info.Width
				genResult.Height = info.Height
			}

			// 静默记录生成结果到 DB
			if store != nil {
				imgRecord := &storage.Image{
					Prompt:      prompt,
					Provider:    apiCfg.Provider,
					LocalPath:   genResult.FilePath,
					StylePreset: stylePrompt,
					Width:       imgWidth,
					Height:      imgHeight,
					SizeBytes:   imgSizeBytes,
					CreatedAt:   time.Now(),
				}
				_ = store.CreateImage(imgRecord)
			}

			if !upload {
				responseSuccess(genResult)
				return
			}

			// 上传到微信
			uploadResult, err := processor.UploadLocalImage(genResult.FilePath)
			if err != nil {
				responseError(err)
				return
			}

			// 静默更新 DB 中的上传信息
			if store != nil {
				_ = store.UpdateImageUpload(genResult.FilePath, uploadResult.MediaID, uploadResult.WechatURL)
			}

			responseSuccess(map[string]any{
				"file_path":  genResult.FilePath,
				"size":       genResult.Size,
				"media_id":   uploadResult.MediaID,
				"wechat_url": uploadResult.WechatURL,
			})
		},
	}

	cmd.Flags().StringVarP(&size, "size", "s", "", "图片尺寸（比例格式 16:9/9:16/1:1/3:4/4:3，可加档位 3:4:1K/3:4:2K/3:4:4K，默认档位 2K）")
	cmd.Flags().BoolVar(&postMode, "post", false, "使用小绿书图片尺寸（默认 3:4）")
	cmd.Flags().StringVar(&stylePrompt, "style", "", "风格提示词（文字描述），前置于 prompt 以保持多图视觉风格一致。可与 --ref 同时使用")
	cmd.Flags().StringVarP(&output, "output", "o", "", "单图模式：输出文件路径；组图模式（--count>1）：输出目录（默认当前目录）")
	cmd.Flags().BoolVar(&upload, "upload", false, "生成后自动上传到微信素材库（需要配置微信 AppID/Secret，仅单图模式）")
	cmd.Flags().StringVar(&refer, "ref", "", "参考图本地路径（支持 JPG, PNG, WebP, GIF）。生成图片会参考此图的视觉风格和内容。可与 --style 同时使用：--ref 提供视觉参考，--style 提供文字风格描述")
	cmd.Flags().IntVar(&count, "count", 1, "生成图片数量（>1 时启用组图模式，支持 volcengine 原生批量 API）")
	cmd.Flags().StringArrayVar(&variants, "variants", nil, "组图变体描述，每张图的内容描述（与 --count 一起使用）。例如：--variants \"封面：茶园晨景\",\"图2：采茶特写\"")

	return cmd
}

// imageBatchCmd 批量生成并上传 Markdown 中的 AI 配图
func imageBatchCmd() *cobra.Command {
	var stylePrompt string
	var noUpload bool
	var output string
	var size string
	var postMode bool

	cmd := &cobra.Command{
		Use:   "batch <markdown_file>",
		Short: "批量生成并上传 Markdown 中的 AI 配图",
		Long: `从 Markdown 文件中提取所有 ![desc](__generate:prompt__) 占位符，批量生成并上传配图

输出 JSON，包含每张图片的微信 CDN URL（--no-upload 时为本地路径）

示例:
  # 批量生成并上传，使用统一风格
  anbanwriter image batch article.md --style "水彩插画，柔和暖色调" -o images.json

  # 仅生成不上传（输出本地路径）
  anbanwriter image batch article.md --style "水彩插画" --no-upload -o images.json`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if noUpload {
				return initConfigMinimal()
			}
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			markdownPath := args[0]

			// Read markdown
			data, err := os.ReadFile(markdownPath)
			if err != nil {
				responseError(fmt.Errorf("读取文件失败: %w", err))
				return
			}
			markdown := string(data)

			// Extract AI image refs using converter
			conv := converter.NewConverter(log)
			allImages := conv.ExtractImages(markdown)

			// Filter to AI-type only
			var aiImages []converter.ImageRef
			for _, img := range allImages {
				if img.Type == converter.ImageTypeAI {
					aiImages = append(aiImages, img)
				}
			}

			if len(aiImages) == 0 {
				responseSuccess(map[string]any{
					"count":   0,
					"urls":    []string{},
					"message": "文章中没有找到 AI 图片占位符（![desc](__generate:prompt__)）",
				})
				return
			}

			// Select image config
			var apiCfg *config.ImageAPI
			if postMode {
				resolved := cfg.ResolvedPostContentImage()
				apiCfg = &resolved
				if size == "" {
					size = cfg.PostImageSize()
				}
			} else {
				apiCfg = &cfg.Wechat.Article.Content.Image
				if size == "" {
					size = cfg.ArticleImageSize()
				}
			}

			processor := image.NewProcessor(cfg, apiCfg, log)
			if stylePrompt != "" {
				processor.SetStylePrompt(stylePrompt)
			}
			if apiCfg.Refer != "" {
				processor.SetRefImage(apiCfg.Refer)
			}

			// 解析预设 prompt（post 模式、无显式 --style、有配置预设时）
			postStyle := cfg.Wechat.Post.Style
			if postStyle == "" && cfg.Rednote != nil {
				postStyle = cfg.Rednote.Style
			}
			if postMode && stylePrompt == "" && postStyle != "" {
				pm := image.NewStylePresetManager()
				if err := pm.LoadPresets(); err == nil {
					if preset, err := pm.GetPreset(postStyle); err == nil {
						processor.SetPresetPrompt(preset.Prompt)
					}
				}
			}

			// Determine output dir (same directory as markdown file)
			markdownDir := filepath.Dir(markdownPath)
			baseName := strings.TrimSuffix(filepath.Base(markdownPath), filepath.Ext(markdownPath))

			type imageResult struct {
				Index    int    `json:"index"`
				Prompt   string `json:"prompt"`
				FilePath string `json:"file_path,omitempty"`
				URL      string `json:"url,omitempty"`
			}

			var results []imageResult
			var urls []string

			for i, img := range aiImages {
				outputFile := filepath.Join(markdownDir, fmt.Sprintf("%s_img_%02d.png", baseName, i+1))

				// Generate image
				var genResult *image.GenerateOnlyResult
				var genErr error
				if size != "" {
					genResult, genErr = processor.GenerateOnlyWithSize(img.AIPrompt, size, outputFile)
				} else {
					genResult, genErr = processor.GenerateOnly(img.AIPrompt, outputFile)
				}
				if genErr != nil {
					responseError(fmt.Errorf("生成第 %d 张图片失败: %w", i+1, genErr))
					return
				}

				// Record generate to DB
				if store != nil {
					imgRecord := &storage.Image{
						Prompt:      img.AIPrompt,
						Provider:    apiCfg.Provider,
						LocalPath:   genResult.FilePath,
						StylePreset: stylePrompt,
						CreatedAt:   time.Now(),
					}
					if info, infoErr := image.GetImageInfo(genResult.FilePath); infoErr == nil {
						imgRecord.Width = info.Width
						imgRecord.Height = info.Height
						imgRecord.SizeBytes = info.Size
					}
					_ = store.CreateImage(imgRecord)
				}

				if noUpload {
					results = append(results, imageResult{
						Index:    img.Index,
						Prompt:   img.AIPrompt,
						FilePath: genResult.FilePath,
					})
					urls = append(urls, genResult.FilePath)
					continue
				}

				// Upload to WeChat
				uploadResult, uploadErr := processor.UploadLocalImage(genResult.FilePath)
				if uploadErr != nil {
					responseError(fmt.Errorf("上传第 %d 张图片失败: %w", i+1, uploadErr))
					return
				}

				// Update DB with upload info
				if store != nil {
					_ = store.UpdateImageUpload(genResult.FilePath, uploadResult.MediaID, uploadResult.WechatURL)
				}

				results = append(results, imageResult{
					Index:  img.Index,
					Prompt: img.AIPrompt,
					URL:    uploadResult.WechatURL,
				})
				urls = append(urls, uploadResult.WechatURL)
			}

			// Write URL list to output file if specified
			if output != "" {
				jsonData, jsonErr := json.Marshal(urls)
				if jsonErr != nil {
					responseError(fmt.Errorf("序列化输出失败: %w", jsonErr))
					return
				}
				if writeErr := os.WriteFile(output, jsonData, 0644); writeErr != nil {
					responseError(fmt.Errorf("写入输出文件失败: %w", writeErr))
					return
				}
			}

			responseSuccess(map[string]any{
				"count":   len(results),
				"results": results,
				"urls":    urls,
			})
		},
	}

	cmd.Flags().StringVar(&stylePrompt, "style", "", "统一风格提示词，所有配图共享此风格")
	cmd.Flags().BoolVar(&noUpload, "no-upload", false, "仅生成不上传到微信（输出本地路径）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出 JSON URL 数组文件路径（默认 stdout）")
	cmd.Flags().StringVarP(&size, "size", "s", "", "图片尺寸（宽高比 16:9/9:16/1:1/3:4 等）")
	cmd.Flags().BoolVar(&postMode, "post", false, "使用小绿书图片尺寸配置")

	return cmd
}
