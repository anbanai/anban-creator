package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/wechat"
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
	cfg          *config.Config
	apiCfg       *config.ImageAPI
	log          *zap.Logger
	ws           *wechat.Service
	compressor   *Compressor
	provider     Provider
	stylePrompt  string
	presetPrompt string // 来自 config 预设（第三优先级）
	refImagePath string // 参考图本地路径（可选）
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
		compressor: NewCompressor(log, apiCfg.MaxWidth, apiCfg.MaxSizeBytes()),
		provider:   provider,
	}
}

// SetStylePrompt 设置风格提示词（CLI --style 传入，优先级高于配置文件）
func (p *Processor) SetStylePrompt(prompt string) {
	p.stylePrompt = prompt
}

// SetPresetPrompt 设置预设风格提示词（来自 config 预设，优先级低于 CLI --style 和 config style_prompt）
func (p *Processor) SetPresetPrompt(prompt string) {
	p.presetPrompt = prompt
}

// SetRefImage 设置参考图路径（CLI --ref 传入）
func (p *Processor) SetRefImage(path string) {
	p.refImagePath = path
}

// 优先级：CLI --style > config style_prompt > preset prompt > 无风格（原样返回）
func (p *Processor) buildPrompt(userPrompt string) string {
	style := strings.TrimSpace(p.stylePrompt)
	if style == "" && p.apiCfg != nil {
		style = strings.TrimSpace(p.apiCfg.StylePrompt)
	}
	if style == "" {
		style = strings.TrimSpace(p.presetPrompt)
	}
	userPrompt = strings.TrimSpace(userPrompt)

	var prompt string
	if style == "" {
		prompt = userPrompt
	} else if userPrompt == "" {
		prompt = style
	} else {
		prompt = style + "\n\n" + userPrompt
	}

	return prompt
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
	p.log.Debug("uploading local image", zap.String("path", filePath))

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
	if p.apiCfg.Compress {
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
	if p.apiCfg.Compress {
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

// GenerateOnlyResult AI 生成图片结果（不含上传）
type GenerateOnlyResult struct {
	FilePath string `json:"file_path"` // 最终本地文件路径
	Size     string `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	url      string // 原始 URL（远程）或 provider 临时路径，仅内部使用
}

// BatchImageResult 组图生成结果（单张）
type BatchImageResult struct {
	FilePath string `json:"file_path"`
	Size     string `json:"size"`
	Index    int    `json:"index"`
}

// processRawResult 处理单张原始生成结果：下载（如需）、裁水印、压缩，保存到 outputPath。
// outputPath 非空时保存到目标路径；为空时返回临时文件路径（调用方负责清理）。
func (p *Processor) processRawResult(result *GenerateResult, outputPath string) (*GenerateOnlyResult, error) {
	var toClean []string

	// 远程 URL 需要下载，本地路径（Gemini/OpenRouter）直接使用
	sourcePath := result.URL
	if strings.HasPrefix(result.URL, "http://") || strings.HasPrefix(result.URL, "https://") {
		tmpPath, err := wechat.DownloadFile(result.URL)
		if err != nil {
			return nil, fmt.Errorf("download generated image: %w", err)
		}
		toClean = append(toClean, tmpPath)
		sourcePath = tmpPath
	} else {
		toClean = append(toClean, sourcePath)
	}

	processedPath := sourcePath

	// 压缩（如果需要）
	if p.apiCfg.Compress {
		compressedPath, compressed, err := p.compressor.CompressImage(processedPath)
		if err != nil {
			p.log.Warn("compress failed, using original", zap.Error(err))
		} else if compressed {
			toClean = append(toClean, compressedPath)
			processedPath = compressedPath
			p.log.Debug("using compressed image", zap.String("path", processedPath))
		}
	}

	finalPath := processedPath
	if outputPath != "" {
		data, err := os.ReadFile(processedPath)
		if err != nil {
			for _, f := range toClean {
				os.Remove(f)
			}
			return nil, fmt.Errorf("read processed image: %w", err)
		}
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			for _, f := range toClean {
				os.Remove(f)
			}
			return nil, fmt.Errorf("save to output %s: %w", outputPath, err)
		}
		for _, f := range toClean {
			os.Remove(f)
		}
		finalPath = outputPath
	} else {
		// 清理中间临时文件，保留最终处理结果（由调用方负责清理）
		for _, f := range toClean {
			if f != processedPath {
				os.Remove(f)
			}
		}
	}

	return &GenerateOnlyResult{
		FilePath: finalPath,
		Size:     result.Size,
		url:      result.URL,
	}, nil
}

// generateOnly 生成图片到本地，不上传到微信。
// outputPath 非空时复制到目标路径并清理所有临时文件；
// outputPath 为空时返回临时文件路径（调用方负责清理）。
func (p *Processor) generateOnly(prompt, size, outputPath string) (*GenerateOnlyResult, error) {
	// 验证配置
	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}

	// 检查 provider 是否可用
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 article.image.provider 和 article.image.key")
	}

	// 如果指定了尺寸，创建带覆盖尺寸的临时 provider，不 mutate 原始配置
	activeProvider := p.provider
	if size != "" {
		apiCfgWithSize := *p.apiCfg
		apiCfgWithSize.Size = size
		var err error
		activeProvider, err = NewProvider(&apiCfgWithSize)
		if err != nil {
			return nil, fmt.Errorf("create provider with size: %w", err)
		}
	}

	// 调用图片生成 API
	ctx := context.Background()
	genOpts := &GenerateOptions{RefImagePath: p.refImagePath}
	result, err := activeProvider.Generate(ctx, p.buildPrompt(prompt), genOpts)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	p.log.Debug("image generated",
		zap.String("provider", result.Model),
		zap.String("size", result.Size))

	return p.processRawResult(result, outputPath)
}

// GenerateBatchOnly 组图生成（不上传到微信），将所有图片保存到 outputDir。
// count 为期望生成数量，outputDir 为已存在的输出目录。
// 若 provider 实现了 BatchProvider，使用原生组图 API（一次调用）；否则降级为逐张生成。
// 处理过程使用并发以提高效率。
func (p *Processor) GenerateBatchOnly(prompt string, count int, outputDir string) ([]*BatchImageResult, error) {
	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 image.provider 和 image.key")
	}

	builtPrompt := p.buildPrompt(prompt)
	ctx := context.Background()
	opts := &GenerateOptions{
		RefImagePath: p.refImagePath,
		MaxImages:    count,
	}

	var rawResults []*GenerateResult
	if bp, ok := p.provider.(BatchProvider); ok {
		p.log.Info("using native batch generation", zap.Int("count", count), zap.String("provider", p.provider.Name()))
		batchResult, err := bp.GenerateBatch(ctx, builtPrompt, opts)
		if err != nil {
			return nil, fmt.Errorf("batch generate images: %w", err)
		}
		rawResults = batchResult.Images
	} else {
		// 降级：循环单图生成
		p.log.Info("using sequential generation", zap.Int("count", count), zap.String("provider", p.provider.Name()))
		singleOpts := &GenerateOptions{RefImagePath: p.refImagePath}
		for i := 0; i < count; i++ {
			result, err := p.provider.Generate(ctx, builtPrompt, singleOpts)
			if err != nil {
				return nil, fmt.Errorf("generate image %d: %w", i+1, err)
			}
			rawResults = append(rawResults, result)
		}
	}

	// 并发处理生成的图片（下载、裁水印、压缩）
	return p.processBatchConcurrent(rawResults, outputDir)
}

// GenerateBatchWithVariants 组图生成支持多内容描述变体
// basePrompt 为基础风格描述，variants 为每张图的具体内容描述
// outputDir 为已存在的输出目录
func (p *Processor) GenerateBatchWithVariants(basePrompt string, variants []string, outputDir string) ([]*BatchImageResult, error) {
	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 image.provider 和 image.key")
	}

	p.log.Info("using batch generation with variants",
		zap.Int("count", len(variants)),
		zap.String("provider", p.provider.Name()))

	ctx := context.Background()
	baseStyle := p.buildPrompt(basePrompt)

	// 为每张图生成：基础风格 + 具体变体
	var rawResults []*GenerateResult
	singleOpts := &GenerateOptions{RefImagePath: p.refImagePath}

	for i, variant := range variants {
		// 组合 prompt：基础风格 + 变体描述
		fullPrompt := baseStyle
		if strings.TrimSpace(variant) != "" {
			fullPrompt = baseStyle + "\n\n" + strings.TrimSpace(variant)
		}

		p.log.Debug("generating variant", zap.Int("index", i+1), zap.String("prompt", fullPrompt))
		result, err := p.provider.Generate(ctx, fullPrompt, singleOpts)
		if err != nil {
			return nil, fmt.Errorf("generate image %d: %w", i+1, err)
		}
		rawResults = append(rawResults, result)
	}

	// 并发处理生成的图片（下载、裁水印、压缩）
	return p.processBatchConcurrent(rawResults, outputDir)
}

// processBatchConcurrent 并发处理批量图片生成结果
func (p *Processor) processBatchConcurrent(rawResults []*GenerateResult, outputDir string) ([]*BatchImageResult, error) {
	count := len(rawResults)
	if count == 0 {
		return nil, fmt.Errorf("no images to process")
	}

	// 使用 WaitGroup 等待所有处理完成
	var wg sync.WaitGroup
	results := make([]*BatchImageResult, count)
	errorsChan := make(chan error, count)

	// 限制并发数，避免过多 goroutine
	sem := make(chan struct{}, 3)

	for i, raw := range rawResults {
		wg.Add(1)
		go func(index int, rawResult *GenerateResult) {
			defer wg.Done()

			sem <- struct{}{}        // 获取信号量
			defer func() { <-sem }() // 释放信号量

			outputPath := filepath.Join(outputDir, fmt.Sprintf("image_%02d.png", index+1))
			processed, err := p.processRawResult(rawResult, outputPath)
			if err != nil {
				errorsChan <- fmt.Errorf("process image %d: %w", index+1, err)
				return
			}

			results[index] = &BatchImageResult{
				FilePath: processed.FilePath,
				Size:     processed.Size,
				Index:    index + 1,
			}
			p.log.Debug("batch image processed", zap.Int("index", index+1), zap.String("path", outputPath))
		}(i, raw)
	}

	wg.Wait()
	close(errorsChan)

	// 检查是否有错误
	var errs []error
	for err := range errorsChan {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errs[0] // 返回第一个错误
	}

	return results, nil
}

// GenerateOnly AI 生成图片到本地文件，不上传到微信
func (p *Processor) GenerateOnly(prompt, outputPath string) (*GenerateOnlyResult, error) {
	p.log.Debug("generating image via AI", zap.String("prompt", prompt))
	return p.generateOnly(prompt, "", outputPath)
}

// GenerateOnlyWithSize AI 生成指定尺寸的图片到本地文件，不上传到微信
func (p *Processor) GenerateOnlyWithSize(prompt, size, outputPath string) (*GenerateOnlyResult, error) {
	p.log.Debug("generating image via AI with size",
		zap.String("prompt", prompt),
		zap.String("size", size))
	return p.generateOnly(prompt, size, outputPath)
}

// GenerateAndUpload AI 生成图片并上传
func (p *Processor) GenerateAndUpload(prompt string) (*GenerateAndUploadResult, error) {
	p.log.Debug("generating image via AI", zap.String("prompt", prompt))

	onlyResult, err := p.generateOnly(prompt, "", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(onlyResult.FilePath)

	// 上传到微信
	uploadResult, err := p.ws.UploadMaterialWithRetry(onlyResult.FilePath, 3)
	if err != nil {
		return nil, err
	}

	return &GenerateAndUploadResult{
		Prompt:      prompt,
		OriginalURL: onlyResult.url,
		MediaID:     uploadResult.MediaID,
		WechatURL:   uploadResult.WechatURL,
	}, nil
}

// GenerateAndUploadWithSize AI 生成指定尺寸的图片并上传
func (p *Processor) GenerateAndUploadWithSize(prompt string, size string) (*GenerateAndUploadResult, error) {
	p.log.Debug("generating image via AI with size",
		zap.String("prompt", prompt),
		zap.String("size", size))

	onlyResult, err := p.generateOnly(prompt, size, "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(onlyResult.FilePath)

	// 上传到微信
	uploadResult, err := p.ws.UploadMaterialWithRetry(onlyResult.FilePath, 3)
	if err != nil {
		return nil, err
	}

	return &GenerateAndUploadResult{
		Prompt:      prompt,
		OriginalURL: onlyResult.url,
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
	if p.apiCfg.Compress {
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
			HintText: "封面图需 ≥ 3,686,400 像素，建议使用 -s 2k 生成 2560x1440 封面",
		}
	}
	stat, err := os.Stat(filePath)
	if err == nil && stat.Size() > 10*1024*1024 {
		return &ProcessorError{Message: "cover image too large (max 10MB)", HintText: "确认 image.compress: true 已在配置中启用"}
	}
	return nil
}
