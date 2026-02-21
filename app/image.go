package main

import (
	"path/filepath"
	"strings"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/image"
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
  generate  - AI 生成图片并上传到微信`,
	}

	cmd.AddCommand(imageUploadCmd())
	cmd.AddCommand(imageDownloadCmd())
	cmd.AddCommand(imageGenerateCmd())

	return cmd
}

// imageUploadCmd 上传本地图片
func imageUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file_path>",
		Short: "上传本地图片到微信素材库",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]
			processor := image.NewProcessor(cfg, &cfg.Article.Image, log)
			result, err := processor.UploadLocalImage(filePath)
			if err != nil {
				responseError(err)
				return
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
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if noUpload {
				return initConfigMinimal()
			}
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			processor := image.NewProcessor(cfg, &cfg.Article.Image, log)

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

	cmd := &cobra.Command{
		Use:   "generate <prompt>",
		Short: "AI 生成图片并上传到微信",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			prompt := args[0]

			// 选择 article 还是 post 的图片配置
			var apiCfg *config.ImageAPI
			if postMode {
				apiCfg = &cfg.Post.Image
				if size == "" {
					size = cfg.PostImageSize()
				}
			} else {
				apiCfg = &cfg.Article.Image
				if size == "" {
					size = cfg.ArticleImageSize()
				}
			}

			processor := image.NewProcessor(cfg, apiCfg, log)

			if size != "" {
				result, err := processor.GenerateAndUploadWithSize(prompt, size)
				if err != nil {
					responseError(err)
					return
				}
				responseSuccess(result)
				return
			}

			result, err := processor.GenerateAndUpload(prompt)
			if err != nil {
				responseError(err)
				return
			}
			responseSuccess(result)
		},
	}

	cmd.Flags().StringVarP(&size, "size", "s", "", "Image size (e.g., 4k for 16:9)")
	cmd.Flags().BoolVar(&postMode, "post", false, "使用小绿书图片尺寸（默认 3:4）")

	return cmd
}
