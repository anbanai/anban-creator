package image

import (
	"context"
	"fmt"
	"os"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/wechat"
	"go.uber.org/zap"
)
// ProcessorError 图片处理错误，携带修复建议
type ProcessorError struct {
	Message  string
	HintText string
}

func (e *ProcessorError) Error() string { return e.Message }
func (e *ProcessorError) Hint() string  { return e.HintText }

// Processor 图片处理器
type Processor struct {
	cfg        *config.Config
	apiCfg     *config.ImageAPI
	log        *zap.Logger
	ws         *wechat.Service
	compressor *Compressor
	provider   Provider
}

// NewProcessor 创建图片处理器
// apiCfg 决定使用哪套图片生成配置（article.image 或 post.image）
func NewProcessor(cfg *config.Config, apiCfg *config.ImageAPI, log *zap.Logger) *Processor {
	// 创建图片生成 Provider
	var provider Provider
	if apiCfg != nil {
		var err error
		provider, err = NewProvider(apiCfg)
		if err != nil {
			// 如果配置了 API Key 但创建失败，记录警告
			if apiCfg.Key != "" {
				log.Warn("failed to create image provider, AI image generation will be unavailable", zap.Error(err))
			}
		}
	}

	// 创建微信服务用于图片上传
	var wechatService *wechat.Service
	if cfg.Wechat.AppID != "" && cfg.Wechat.Secret != "" {
		wechatService = wechat.NewService(cfg, log)
	}

	return &Processor{
		cfg:        cfg,
		apiCfg:     apiCfg,
		log:        log,
		ws:         wechatService,
		compressor: NewCompressor(log, cfg.Image.MaxWidth, cfg.MaxImageSizeBytes()),
		provider:   provider,
	}
}

// UploadResult 上传结果
type UploadResult struct {
	MediaID   string `json:"media_id"`
	WechatURL string `json:"wechat_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// UploadLocalImage 上传本地图片
func (p *Processor) UploadLocalImage(filePath string) (*UploadResult, error) {
	p.log.Info("uploading local image", zap.String("path", filePath))

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, &ProcessorError{Message: fmt.Sprintf("file not found: %s", filePath), HintText: "请检查文件路径是否正确"}
	}

	// 检查图片格式
	if !IsValidImageFormat(filePath) {
		return nil, &ProcessorError{Message: fmt.Sprintf("unsupported image format: %s", filePath), HintText: "支持格式: JPG, PNG"}
	}

	// 如果需要压缩，先处理
	processedPath := filePath
	if p.cfg.Image.Compress {
		compressedPath, compressed, err := p.compressor.CompressImage(filePath)
		if err != nil {
			p.log.Warn("compress failed, using original", zap.Error(err))
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
			p.log.Info("using compressed image", zap.String("path", processedPath))
		}
	}

	// 上传到微信
	result, err := p.ws.UploadMaterialWithRetry(processedPath, 3)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		MediaID:   result.MediaID,
		WechatURL: result.WechatURL,
	}, nil
}

// DownloadAndUpload 下载在线图片并上传
func (p *Processor) DownloadAndUpload(url string) (*UploadResult, error) {
	p.log.Info("downloading and uploading image", zap.String("url", url))

	// 下载图片
	tmpPath, err := wechat.DownloadFile(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpPath)

	// 检查格式
	if !IsValidImageFormat(tmpPath) {
		return nil, fmt.Errorf("downloaded file is not a valid image")
	}

	// 压缩（如果需要）
	processedPath := tmpPath
	if p.cfg.Image.Compress {
		compressedPath, compressed, err := p.compressor.CompressImage(tmpPath)
		if err != nil {
			p.log.Warn("compress failed, using original", zap.Error(err))
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
			p.log.Info("using compressed image", zap.String("path", processedPath))
		}
	}

	// 上传到微信
	result, err := p.ws.UploadMaterialWithRetry(processedPath, 3)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		MediaID:   result.MediaID,
		WechatURL: result.WechatURL,
	}, nil
}

// GenerateAndUploadResult AI 生成图片结果
type GenerateAndUploadResult struct {
	Prompt      string `json:"prompt"`
	OriginalURL string `json:"original_url"`
	MediaID     string `json:"media_id"`
	WechatURL   string `json:"wechat_url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// GenerateAndUpload AI 生成图片并上传
func (p *Processor) GenerateAndUpload(prompt string) (*GenerateAndUploadResult, error) {
	p.log.Info("generating image via AI", zap.String("prompt", prompt))

	// 验证配置
	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}

	// 检查 provider 是否可用
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 article.image.provider 和 article.image.key")
	}

	// 调用图片生成 API
	ctx := context.Background()
	result, err := p.provider.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	p.log.Info("image generated",
		zap.String("url", result.URL),
		zap.String("provider", result.Model),
		zap.String("size", result.Size))

	// 下载生成的图片
	tmpPath, err := wechat.DownloadFile(result.URL)
	if err != nil {
		return nil, fmt.Errorf("download generated image: %w", err)
	}
	defer os.Remove(tmpPath)

	// 压缩（如果需要）
	processedPath := tmpPath
	if p.cfg.Image.Compress {
		compressedPath, compressed, err := p.compressor.CompressImage(tmpPath)
		if err != nil {
			p.log.Warn("compress failed, using original", zap.Error(err))
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
			p.log.Info("using compressed image", zap.String("path", processedPath))
		}
	}

	// 上传到微信
	uploadResult, err := p.ws.UploadMaterialWithRetry(processedPath, 3)
	if err != nil {
		return nil, err
	}

	return &GenerateAndUploadResult{
		Prompt:      prompt,
		OriginalURL: result.URL,
		MediaID:     uploadResult.MediaID,
		WechatURL:   uploadResult.WechatURL,
	}, nil
}

// GenerateAndUploadWithSize AI 生成指定尺寸的图片并上传
func (p *Processor) GenerateAndUploadWithSize(prompt string, size string) (*GenerateAndUploadResult, error) {
	p.log.Info("generating image via AI with size",
		zap.String("prompt", prompt),
		zap.String("size", size))

	// 验证配置
	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}

	// 检查 provider 是否可用
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 article.image.provider 和 article.image.key")
	}

	// 创建带有覆盖尺寸的临时 apiCfg 副本，不 mutate 原始配置
	apiCfgWithSize := *p.apiCfg
	apiCfgWithSize.Size = size

	// 重新创建 provider 以使用新尺寸
	newProvider, err := NewProvider(&apiCfgWithSize)
	if err != nil {
		return nil, fmt.Errorf("create provider with size: %w", err)
	}

	// 调用图片生成 API
	ctx := context.Background()
	result, err := newProvider.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	p.log.Info("image generated",
		zap.String("url", result.URL),
		zap.String("provider", result.Model),
		zap.String("size", result.Size))

	// 下载生成的图片
	tmpPath, err := wechat.DownloadFile(result.URL)
	if err != nil {
		return nil, fmt.Errorf("download generated image: %w", err)
	}
	defer os.Remove(tmpPath)

	// 上传到微信
	uploadResult, err := p.ws.UploadMaterialWithRetry(tmpPath, 3)
	if err != nil {
		return nil, err
	}

	return &GenerateAndUploadResult{
		Prompt:      prompt,
		OriginalURL: result.URL,
		MediaID:     uploadResult.MediaID,
		WechatURL:   uploadResult.WechatURL,
	}, nil
}

// DownloadResult 仅下载结果
type DownloadResult struct {
	FilePath string `json:"file_path"`
	Size     int64  `json:"size"`
}

// DownloadOnly 下载图片到本地，不上传到微信
func (p *Processor) DownloadOnly(url, outputPath string) (*DownloadResult, error) {
	p.log.Info("downloading image", zap.String("url", url))

	tmpPath, err := wechat.DownloadFile(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpPath)

	if !IsValidImageFormat(tmpPath) {
		return nil, fmt.Errorf("downloaded file is not a valid image")
	}

	processedPath := tmpPath
	if p.cfg.Image.Compress {
		compressedPath, compressed, err := p.compressor.CompressImage(tmpPath)
		if err != nil {
			p.log.Warn("compress failed, using original", zap.Error(err))
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
		}
	}

	data, err := os.ReadFile(processedPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write output: %w", err)
	}

	return &DownloadResult{FilePath: outputPath, Size: int64(len(data))}, nil
}

// GetImageInfo 获取图片信息
func (p *Processor) GetImageInfo(filePath string) (*ImageInfo, error) {
	return GetImageInfo(filePath)
}

// CompressImage 压缩图片（公开方法）
func (p *Processor) CompressImage(filePath string) (string, bool, error) {
	return p.compressor.CompressImage(filePath)
}

// SetCompressQuality 设置压缩质量
func (p *Processor) SetCompressQuality(quality int) {
	p.compressor.SetQuality(quality)
}

// ValidateCoverImage 验证封面图片是否满足微信要求
// 要求：总像素 >= 3,686,400、< 10MB、格式为 JPG/PNG
func (p *Processor) ValidateCoverImage(filePath string) error {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return &ProcessorError{Message: fmt.Sprintf("cover image not found: %s", filePath), HintText: "请检查封面图片路径是否正确"}
	}
	if !IsValidImageFormat(filePath) {
		return &ProcessorError{Message: "unsupported cover image format", HintText: "封面图支持格式: JPG, PNG"}
	}
	info, err := GetImageInfo(filePath)
	if err != nil {
		return &ProcessorError{Message: "cannot read cover image info: " + err.Error(), HintText: "图片文件可能已损坏"}
	}
	if info.Width*info.Height < config.MinWeChatPixels {
		return &ProcessorError{
			Message:  fmt.Sprintf("cover image too small: %dx%d = %d pixels (min %d)", info.Width, info.Height, info.Width*info.Height, config.MinWeChatPixels),
			HintText: "封面图需 ≥ 3,686,400 像素，建议使用 -s 4k 生成 2560x1440 封面",
		}
	}
	stat, err := os.Stat(filePath)
	if err == nil && stat.Size() > 10*1024*1024 {
		return &ProcessorError{Message: "cover image too large (max 10MB)", HintText: "确认 image.compress: true 已在配置中启用"}
	}
	return nil
}
