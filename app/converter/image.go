package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// ImagePlaceholder 图片占位符
type ImagePlaceholder struct {
	Index int
	HTML  string
}

// imageProcessor 图片处理器
type imageProcessor struct{}

// NewImageProcessor 创建图片处理器
func NewImageProcessor() *imageProcessor {
	return &imageProcessor{}
}

// ExtractPlaceholders 从 HTML 中提取图片占位符
func (p *imageProcessor) ExtractPlaceholders(html string) []ImagePlaceholder {
	var placeholders []ImagePlaceholder

	// 匹配 <!-- IMG:n --> 格式
	pattern := regexp.MustCompile(`<!-- IMG:(\d+) -->`)
	matches := pattern.FindAllStringSubmatch(html, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			index := parseInt(match[1])
			placeholders = append(placeholders, ImagePlaceholder{
				Index: index,
				HTML:  match[0],
			})
		}
	}

	return placeholders
}

// InsertPlaceholders 在 HTML 中插入图片占位符
func (p *imageProcessor) InsertPlaceholders(html string, images []ImageRef) string {
	result := html
	imgIndex := 0

	// 按行处理 HTML
	lines := strings.Split(result, "\n")
	var newLines []string

	for _, line := range lines {
		// 检查是否是图片行
		trimmed := strings.TrimSpace(line)

		// 检测图片标记语法
		if p.isImageLine(trimmed) {
			// 生成占位符
			placeholder := fmt.Sprintf("<!-- IMG:%d -->", imgIndex)
			newLines = append(newLines, placeholder)

			// 更新图片引用的占位符
			if imgIndex < len(images) {
				images[imgIndex].Placeholder = placeholder
			}
			imgIndex++
		} else {
			newLines = append(newLines, line)
		}
	}

	return strings.Join(newLines, "\n")
}

// isImageLine 检查是否是图片行
func (p *imageProcessor) isImageLine(line string) bool {
	// 检测 Markdown 图片语法
	if strings.HasPrefix(line, "![") {
		return true
	}

	// 检测 HTML img 标签
	if strings.HasPrefix(line, "<img") {
		return true
	}

	// 检测已有占位符
	if strings.HasPrefix(line, "<!-- IMG:") {
		return true
	}

	return false
}

// ReplacePlaceholders 替换图片占位符为实际图片
func (p *imageProcessor) ReplacePlaceholders(html string, images []ImageRef) string {
	result := html

	for _, img := range images {
		if img.Placeholder == "" {
			continue
		}
		if img.WechatURL == "" {
			// 没有微信 URL，跳过
			continue
		}

		// 生成 img 标签
		imgTag := p.buildImageTag(img)

		// 替换占位符
		result = strings.ReplaceAll(result, img.Placeholder, imgTag)
	}

	return result
}

// buildImageTag 构建图片标签
func (p *imageProcessor) buildImageTag(img ImageRef) string {
	style := "max-width:100%;height:auto;display:block;margin:20px auto;"
	return fmt.Sprintf(`<img src="%s" style="%s" alt="" />`, img.WechatURL, style)
}

// CountImages 统计 Markdown 中的图片数量
func (p *imageProcessor) CountImages(markdown string) int {
	count := 0

	// 匹配 ![alt](url) 格式
	pattern := regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	matches := pattern.FindAllString(markdown, -1)
	count += len(matches)

	return count
}

// ParseImageSyntax 解析图片语法
func (p *imageProcessor) ParseImageSyntax(markdown string) []ImageRef {
	var images []ImageRef
	index := 0

	// 匹配本地图片: ![alt](./path/to/image.png)
	localPattern := regexp.MustCompile(`!\[([^\]]*)\]\((\.\/[^)]+)\)`)
	for _, match := range localPattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:    index,
				Original: match[2],
				Type:     ImageTypeLocal,
			})
			index++
		}
	}

	// 匹配在线图片: ![alt](https://...)
	onlinePattern := regexp.MustCompile(`!\[([^\]]*)\]\((https?://[^)]+)\)`)
	for _, match := range onlinePattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:    index,
				Original: match[2],
				Type:     ImageTypeOnline,
			})
			index++
		}
	}

	// 匹配 AI 生成图片: ![alt](__generate:prompt__)
	aiPattern := regexp.MustCompile(`!\[([^\]]*)\]\(__generate:([^)]+)__\)`)
	for _, match := range aiPattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:    index,
				Original: match[2],
				Type:     ImageTypeAI,
				AIPrompt: match[2],
			})
			index++
		}
	}

	return images
}

// 辅助函数
func parseInt(s string) int {
	result := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			result = result*10 + int(ch-'0')
		} else {
			break
		}
	}
	return result
}
