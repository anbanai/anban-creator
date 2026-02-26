package image

import (
	"testing"

	"github.com/royalrick/wechatwriter/app/config"
	"go.uber.org/zap"
)

func newTestProcessor(apiCfg *config.ImageAPI) *Processor {
	return &Processor{
		apiCfg: apiCfg,
		log:    zap.NewNop(),
	}
}

func TestProcessor_GenerateOnly_NoAPIKey(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	_, err := p.GenerateOnly("春天的茶园", "output.png")
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
}

func TestProcessor_GenerateOnly_NoProvider(t *testing.T) {
	// API key is set but provider is nil (creation failed silently)
	p := newTestProcessor(&config.ImageAPI{Key: "test-key"})
	// provider is nil by default in newTestProcessor
	_, err := p.GenerateOnly("春天的茶园", "output.png")
	if err == nil {
		t.Fatal("expected error when provider is nil, got nil")
	}
}

func TestProcessor_GenerateOnlyWithSize_NoAPIKey(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	_, err := p.GenerateOnlyWithSize("春天的茶园", "16:9", "output.png")
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
}

func TestProcessor_buildPrompt(t *testing.T) {
	tests := []struct {
		name          string
		configStyle   string
		overrideStyle string
		userPrompt    string
		watermark     config.WatermarkConfig
		size          string
		want          string
	}{
		{
			name:       "无风格时返回原始 prompt",
			userPrompt: "春天的茶园",
			want:       "春天的茶园",
		},
		{
			name:        "config 风格与 prompt 拼接",
			configStyle: "扁平插画，莫兰迪色系",
			userPrompt:  "封面标题",
			want:        "扁平插画，莫兰迪色系\n\n封面标题",
		},
		{
			name:          "CLI 覆盖 config 风格（CLI 优先）",
			configStyle:   "config 风格",
			overrideStyle: "CLI 风格",
			userPrompt:    "内容图",
			want:          "CLI 风格\n\n内容图",
		},
		{
			name:          "仅 CLI 风格无 config 风格",
			overrideStyle: "CLI 风格",
			userPrompt:    "内容图",
			want:          "CLI 风格\n\n内容图",
		},
		{
			name:        "空 prompt 只返回风格",
			configStyle: "扁平插画，莫兰迪色系",
			userPrompt:  "",
			want:        "扁平插画，莫兰迪色系",
		},
		{
			name:       "空风格空 prompt 返回空字符串",
			userPrompt: "",
			want:       "",
		},
		{
			name:        "空格裁剪：风格有前后空格",
			configStyle: "  扁平插画  ",
			userPrompt:  "  封面  ",
			want:        "扁平插画\n\n封面",
		},
		{
			name:          "空格裁剪：override 有前后空格",
			overrideStyle: "  CLI 风格  ",
			userPrompt:    "  内容  ",
			want:          "CLI 风格\n\n内容",
		},
		{
			name:          "override 为纯空格时回退到 config 风格",
			configStyle:   "config 风格",
			overrideStyle: "   ",
			userPrompt:    "内容图",
			want:          "config 风格\n\n内容图",
		},
		{
			name:       "启用水印+有尺寸时追加具体像素留白提示",
			userPrompt: "春天的茶园",
			watermark:  config.WatermarkConfig{Enable: true, Margin: 50},
			size:       "2560x1440",
			want:       "春天的茶园\n\n【重要】图片四周需要预留空白边距：左右各至少 50 像素，上下各至少 28 像素。边缘区域应为纯色或渐变背景，不包含任何重要元素。",
		},
		{
			name:       "启用水印+无尺寸时追加通用留白提示",
			userPrompt: "春天的茶园",
			watermark:  config.WatermarkConfig{Enable: true, Margin: 30},
			size:       "",
			want:       "春天的茶园\n\n【重要】图片四周需要预留至少 30 像素的空白边距，边缘区域应为纯色或渐变背景，不包含任何重要元素。",
		},
		{
			name:       "未启用水印时 prompt 不变",
			userPrompt: "春天的茶园",
			watermark:  config.WatermarkConfig{Enable: false, Margin: 50},
			size:       "2560x1440",
			want:       "春天的茶园",
		},
		{
			name:       "启用水印但 margin 为 0 时 prompt 不变",
			userPrompt: "春天的茶园",
			watermark:  config.WatermarkConfig{Enable: true, Margin: 0},
			size:       "2560x1440",
			want:       "春天的茶园",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCfg := &config.ImageAPI{
				StylePrompt: tt.configStyle,
				Watermark:   tt.watermark,
				Size:        tt.size,
			}
			p := newTestProcessor(apiCfg)
			if tt.overrideStyle != "" {
				p.stylePrompt = tt.overrideStyle
			}
			got := p.buildPrompt(tt.userPrompt)
			if got != tt.want {
				t.Errorf("buildPrompt(%q) =\n  %q\nwant\n  %q", tt.userPrompt, got, tt.want)
			}
		})
	}
}
