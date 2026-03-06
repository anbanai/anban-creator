package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/converter"
	"github.com/royalrick/wechatwriter/app/image"
	"github.com/royalrick/wechatwriter/app/storage"
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
	cmd.AddCommand(imageCropCmd())
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
  wechatwriter image upload cover.jpg
  wechatwriter image upload ./images/photo.png`,
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
  wechatwriter image download https://example.com/photo.jpg

  # 仅下载到本地
  wechatwriter image download https://example.com/photo.jpg --no-upload
  wechatwriter image download https://example.com/photo.jpg --no-upload -o local.jpg`,
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
  volcengine  - 火山方舟 Seedream（doubao-seedream-4-5-251128）
                尺寸: 比例格式 1:1, 16:9, 3:4 等，可加档位 3:4:4K

需要配置:
  article.image.key      - 图片 API Key
  article.image.provider - 服务提供者 (openai/gemini/openrouter/volcengine)

示例:
  # 生成图片（自动以时间戳命名，如 generated_image_20060102_150405.png）
  wechatwriter image generate "春天的茶园，阳光明媚"

  # 指定输出文件
  wechatwriter image generate "春天的茶园" -o tea.png

  # 指定尺寸（比例格式，默认 2K 档位）
  wechatwriter image generate "清晨茶园" --size 16:9 -o cover.jpg
  wechatwriter image generate "竖版封面" --size 9:16 --post -o post.jpg

  # 指定比例+档位
  wechatwriter image generate "封面图" --size 3:4:1K -o xhs-cover.jpg
  wechatwriter image generate "封面图" --size 3:4:4K -o cover-4k.jpg

  # 生成后自动上传到微信
  wechatwriter image generate "封面图" --upload`,
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

			// 自动生成时间戳文件名
			if output == "" {
				output = time.Now().Format("generated_image_20060102_150405") + ".png"
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
			if postStyle == "" && cfg.Xiaohongshu != nil {
				postStyle = cfg.Xiaohongshu.Style
			}
			if postMode && stylePrompt == "" && postStyle != "" {
				pm := image.NewStylePresetManager()
				if err := pm.LoadPresets(); err == nil {
					if preset, err := pm.GetPreset(postStyle); err == nil {
						processor.SetPresetPrompt(preset.Prompt)
					}
				}
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

			// 静默记录生成结果到 DB
			if store != nil {
				imgRecord := &storage.Image{
					Prompt:      prompt,
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
	cmd.Flags().StringVar(&stylePrompt, "style", "", "风格提示词，前置于生成提示词以保持多图风格一致")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出文件路径（默认自动生成时间戳文件名）")
	cmd.Flags().BoolVar(&upload, "upload", false, "生成后自动上传到微信素材库（需要配置微信 AppID/Secret）")
	cmd.Flags().StringVar(&refer, "ref", "", "参考图本地路径（支持 JPG, PNG, WebP, GIF），用于风格/内容参考")

	return cmd
}

// imageCropCmd 裁剪图片四边
func imageCropCmd() *cobra.Command {
	var margin int
	var output string

	cmd := &cobra.Command{
		Use:   "crop <file_path>",
		Short: "裁剪图片四边（自动保持宽高比）",
		Long: `裁剪图片四边去除水印，左右各裁 margin 像素，上下按宽高比自动计算，保持原始宽高比不变

示例:
  # 左右各裁 20 像素，上下自动计算
  wechatwriter image crop cover.jpg --margin 20

  # 指定输出路径
  wechatwriter image crop cover.jpg --margin 20 -o cropped.jpg`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]

			outPath, err := image.CropMargin(log, filePath, margin)
			if err != nil {
				responseError(err)
				return
			}

			// 如果指定了输出路径，移动临时文件到目标位置
			if output != "" {
				data, readErr := os.ReadFile(outPath)
				os.Remove(outPath)
				if readErr != nil {
					responseError(readErr)
					return
				}
				if writeErr := os.WriteFile(output, data, 0644); writeErr != nil {
					responseError(writeErr)
					return
				}
				outPath = output
			}

			// 获取输出文件尺寸
			info, _ := image.GetImageInfo(outPath)
			result := map[string]any{
				"file_path": outPath,
				"margin":    margin,
			}
			if info != nil {
				result["width"] = info.Width
				result["height"] = info.Height
				result["size"] = info.Size
			}
			responseSuccess(result)
		},
	}

	cmd.Flags().IntVarP(&margin, "margin", "m", 0, "左右各裁剪的像素数，上下按宽高比自动计算（必填）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出文件路径（默认保存到临时目录）")
	_ = cmd.MarkFlagRequired("margin")

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
  wechatwriter image batch article.md --style "水彩插画，柔和暖色调" -o images.json

  # 仅生成不上传（输出本地路径）
  wechatwriter image batch article.md --style "水彩插画" --no-upload -o images.json`,
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
			if postStyle == "" && cfg.Xiaohongshu != nil {
				postStyle = cfg.Xiaohongshu.Style
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
