package image

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/wechat"
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
	log          zerolog.Logger
	ws           *wechat.Service
	compressor   *Compressor
	provider     Provider
	stylePrompt  string
	refImagePath string // 参考图本地路径（可选）
	mu           sync.RWMutex
}

// NewProcessor 创建图片处理器
// apiCfg 决定使用哪套图片生成配置（article.image 或 xls.image）
func NewProcessor(cfg *config.Config, apiCfg *config.ImageAPI, log zerolog.Logger) *Processor {
	// 创建图片生成 Provider
	var provider Provider
	if apiCfg != nil {
		var err error
		provider, err = NewProvider(apiCfg, log)
		if err != nil {
			// 如果配置了 API Key 但创建失败，记录警告
			if apiCfg.Key != "" {
				log.Warn().Err(err).Msg("failed to create image provider, AI image generation will be unavailable")
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

// wechatUpload uploads a file to WeChat CDN with retry. Returns an error if
// WeChat credentials are not configured.
func (p *Processor) wechatUpload(filePath string) (*wechat.UploadMaterialResult, error) {
	if p.ws == nil {
		return nil, &ProcessorError{
			Message: "wechat credentials not configured, cannot upload image",
			HintText: "请先在频道配置中填写 WeChat App ID 和 Secret",
		}
	}
	return p.ws.UploadMaterialWithRetry(filePath, 3)
}

// SetStylePrompt 设置风格提示词（CLI --style 传入，优先级高于配置文件）
func (p *Processor) SetStylePrompt(prompt string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stylePrompt = prompt
}

// SetRefImage 设置参考图路径（CLI --ref 传入）
func (p *Processor) SetRefImage(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refImagePath = path
}

// styleAndRef returns the current stylePrompt and refImagePath under a read lock.
func (p *Processor) styleAndRef() (stylePrompt, refImagePath string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stylePrompt, p.refImagePath
}

// 优先级：CLI --style > config style_prompt > preset prompt > 无风格（原样返回）
func (p *Processor) buildPrompt(userPrompt string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	style := strings.TrimSpace(p.stylePrompt)
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
	p.log.Debug().Str("path", filePath).Msg("uploading local image")

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
			p.log.Warn().Err(err).Msg("compress failed, using original")
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
			p.log.Info().Str("path", processedPath).Msg("using compressed image")
		}
	}

	// 上传到微信
	result, err := p.wechatUpload(processedPath)
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
	p.log.Info().Str("url", url).Msg("downloading and uploading image")

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
			p.log.Warn().Err(err).Msg("compress failed, using original")
		} else if compressed {
			processedPath = compressedPath
			defer os.Remove(compressedPath)
			p.log.Info().Str("path", processedPath).Msg("using compressed image")
		}
	}

	// 上传到微信
	result, err := p.wechatUpload(processedPath)
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

// GenerateRawResult 调用 AI provider 生成图片后返回原始结果。
// URL 字段为 provider 直接返回的内容：远程 HTTPS URL、data URL 或本地临时文件路径（已转为 data URL）。
type GenerateRawResult struct {
	URL   string `json:"url"`   // 远程 URL 或 data URL
	Size  string `json:"size"`  // provider 报告的尺寸（如 "1024x1024"）
	Index int    `json:"index"` // 批量时的序号
}

// GenerateRaw 调用 AI provider 生成图片，返回原始 URL 或 data URL。
// 不下载、不压缩、不写磁盘（本地临时文件会被读取并转为 data URL 后清理）。
func (p *Processor) GenerateRaw(ctx context.Context, prompt string) (*GenerateRawResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 image.provider 和 image.key")
	}

	_, refImg := p.styleAndRef()
	genOpts := &GenerateOptions{RefImagePath: refImg}
	result, err := p.provider.Generate(ctx, p.buildPrompt(prompt), genOpts)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}

	p.log.Debug().Str("provider", result.Model).Str("size", result.Size).Msg("image generated (raw)")

	url, err := p.resolveRawURL(result.URL)
	if err != nil {
		return nil, err
	}

	return &GenerateRawResult{
		URL:  url,
		Size: result.Size,
	}, nil
}

// GenerateBatchRaw 批量生成图片，返回原始 URL 或 data URL。
// 不下载、不压缩、不写磁盘。
func (p *Processor) GenerateBatchRaw(ctx context.Context, prompt string, count int) ([]*GenerateRawResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 image.provider 和 image.key")
	}

	builtPrompt := p.buildPrompt(prompt)
	_, refImg := p.styleAndRef()
	opts := &GenerateOptions{
		RefImagePath: refImg,
		MaxImages:    count,
	}

	var rawResults []*GenerateResult
	if bp, ok := p.provider.(BatchProvider); ok {
		p.log.Info().Int("count", count).Str("provider", p.provider.Name()).Msg("using native batch generation (raw)")
		batchResult, err := bp.GenerateBatch(ctx, builtPrompt, opts)
		if err != nil {
			return nil, fmt.Errorf("batch generate images: %w", err)
		}
		rawResults = batchResult.Images
	} else {
		p.log.Info().Int("count", count).Str("provider", p.provider.Name()).Msg("using sequential generation (raw)")
		singleOpts := &GenerateOptions{RefImagePath: refImg}
		for i := 0; i < count; i++ {
			result, err := p.provider.Generate(ctx, builtPrompt, singleOpts)
			if err != nil {
				return nil, fmt.Errorf("generate image %d: %w", i+1, err)
			}
			rawResults = append(rawResults, result)
		}
	}

	results := make([]*GenerateRawResult, 0, len(rawResults))
	for i, raw := range rawResults {
		url, err := p.resolveRawURL(raw.URL)
		if err != nil {
			return nil, fmt.Errorf("resolve image %d URL: %w", i+1, err)
		}
		results = append(results, &GenerateRawResult{
			URL:   url,
			Size:  raw.Size,
			Index: i + 1,
		})
	}

	return results, nil
}

// resolveRawURL 统一处理 provider 返回的 URL：
// - 远程 URL（http/https）→ 直接返回
// - data URL → 直接返回
// - 本地临时文件 → 读取内容转为 data URL 并删除临时文件
func (p *Processor) resolveRawURL(rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL, nil
	}
	if strings.HasPrefix(rawURL, "data:") {
		return rawURL, nil
	}
	// 本地临时文件 → 转 data URL
	data, err := os.ReadFile(rawURL)
	if err != nil {
		return "", fmt.Errorf("read provider temp file: %w", err)
	}
	defer os.Remove(rawURL)

	mime := http.DetectContentType(data)
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mime, encoded), nil
}

// processRawResult 处理单张原始生成结果：下载（如需）、裁水印、压缩，保存到 outputPath。
// outputPath 非空时保存到目标路径；为空时返回临时文件路径（调用方负责清理）。
func (p *Processor) processRawResult(result *GenerateResult, outputPath string) (*GenerateOnlyResult, error) {
	var toClean []string
	// Ensure temp files are cleaned up even on panic.
	defer func() {
		for _, f := range toClean {
			os.Remove(f)
		}
	}()

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
			p.log.Warn().Err(err).Msg("compress failed, using original")
		} else if compressed {
			toClean = append(toClean, compressedPath)
			processedPath = compressedPath
			p.log.Debug().Str("path", processedPath).Msg("using compressed image")
		}
	}

	finalPath := processedPath
	if outputPath != "" {
		data, err := os.ReadFile(processedPath)
		if err != nil {
			return nil, fmt.Errorf("read processed image: %w", err)
		}
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return nil, fmt.Errorf("save to output %s: %w", outputPath, err)
		}
		// Mark all temps as cleaned since defer will handle it.
		toClean = nil
		finalPath = outputPath
	} else {
		// 保留最终处理结果（由调用方负责清理），清除中间文件。
		for _, f := range toClean {
			if f != processedPath {
				os.Remove(f)
			}
		}
		// Remove final file from toClean so defer won't delete it.
		toClean = nil
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
func (p *Processor) generateOnly(ctx context.Context, prompt, size, outputPath string) (*GenerateOnlyResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

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
		activeProvider, err = NewProvider(&apiCfgWithSize, p.log)
		if err != nil {
			return nil, fmt.Errorf("create provider with size: %w", err)
		}
	}

	// 调用图片生成 API
	_, refImg := p.styleAndRef()
	genOpts := &GenerateOptions{RefImagePath: refImg}
	result, err := activeProvider.Generate(ctx, p.buildPrompt(prompt), genOpts)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	p.log.Debug().Str("provider", result.Model).Str("size", result.Size).Msg("image generated")

	return p.processRawResult(result, outputPath)
}

// GenerateBatchOnly 组图生成（不上传到微信），将所有图片保存到 outputDir。
// count 为期望生成数量，outputDir 为已存在的输出目录。
// 若 provider 实现了 BatchProvider，使用原生组图 API（一次调用）；否则降级为逐张生成。
// 处理过程使用并发以提高效率。
func (p *Processor) GenerateBatchOnly(ctx context.Context, prompt string, count int, outputDir string) ([]*BatchImageResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := config.ValidateForImageGeneration(p.apiCfg); err != nil {
		return nil, err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("图片生成服务未配置，请检查配置文件中的 image.provider 和 image.key")
	}

	builtPrompt := p.buildPrompt(prompt)
	_, refImg := p.styleAndRef()
	opts := &GenerateOptions{
		RefImagePath: refImg,
		MaxImages:    count,
	}

	var rawResults []*GenerateResult
	if bp, ok := p.provider.(BatchProvider); ok {
		p.log.Info().Int("count", count).Str("provider", p.provider.Name()).Msg("using native batch generation")
		batchResult, err := bp.GenerateBatch(ctx, builtPrompt, opts)
		if err != nil {
			return nil, fmt.Errorf("batch generate images: %w", err)
		}
		rawResults = batchResult.Images
	} else {
		// 降级：循环单图生成
		p.log.Info().Int("count", count).Str("provider", p.provider.Name()).Msg("using sequential generation")
		singleOpts := &GenerateOptions{RefImagePath: refImg}
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
			p.log.Debug().Int("index", index+1).Str("path", outputPath).Msg("batch image processed")
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
func (p *Processor) GenerateOnly(ctx context.Context, prompt, outputPath string) (*GenerateOnlyResult, error) {
	p.log.Debug().Str("prompt", prompt).Msg("generating image via AI")
	return p.generateOnly(ctx, prompt, "", outputPath)
}

// GenerateOnlyWithSize AI 生成指定尺寸的图片到本地文件，不上传到微信
func (p *Processor) GenerateOnlyWithSize(ctx context.Context, prompt, size, outputPath string) (*GenerateOnlyResult, error) {
	p.log.Debug().Str("prompt", prompt).Str("size", size).Msg("generating image via AI with size")
	return p.generateOnly(ctx, prompt, size, outputPath)
}

// GenerateAndUpload AI 生成图片并上传
func (p *Processor) GenerateAndUpload(ctx context.Context, prompt string) (*GenerateAndUploadResult, error) {
	p.log.Debug().Str("prompt", prompt).Msg("generating image via AI")

	onlyResult, err := p.generateOnly(ctx, prompt, "", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(onlyResult.FilePath)

	// 上传到微信
	uploadResult, err := p.wechatUpload(onlyResult.FilePath)
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
func (p *Processor) GenerateAndUploadWithSize(ctx context.Context, prompt string, size string) (*GenerateAndUploadResult, error) {
	p.log.Debug().Str("prompt", prompt).Str("size", size).Msg("generating image via AI with size")

	onlyResult, err := p.generateOnly(ctx, prompt, size, "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(onlyResult.FilePath)

	// 上传到微信
	uploadResult, err := p.wechatUpload(onlyResult.FilePath)
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
	p.log.Info().Str("url", url).Msg("downloading image")

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
			p.log.Warn().Err(err).Msg("compress failed, using original")
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
