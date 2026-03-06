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

func TestProcessor_SetRefImage(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	p.SetRefImage("/path/to/ref.png")
	if p.refImagePath != "/path/to/ref.png" {
		t.Errorf("SetRefImage() refImagePath = %q, want %q", p.refImagePath, "/path/to/ref.png")
	}
	// Zero-value should be empty
	p2 := newTestProcessor(&config.ImageAPI{})
	if p2.refImagePath != "" {
		t.Errorf("default refImagePath should be empty, got %q", p2.refImagePath)
	}
}

func TestProcessor_buildPrompt(t *testing.T) {
	tests := []struct {
		name          string
		configStyle   string
		overrideStyle string
		userPrompt    string
		watermark     *config.WatermarkConfig
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
			watermark:  &config.WatermarkConfig{Enable: true, Margin: 50},
			size:       "16:9",
			want:       "春天的茶园\n\n【重要】图片四周需要预留空白边距：左右各至少 50 像素，上下各至少 28 像素。边缘区域应为纯色或渐变背景，不包含任何重要元素。",
		},
		{
			name:       "启用水印+无尺寸时追加通用留白提示",
			userPrompt: "春天的茶园",
			watermark:  &config.WatermarkConfig{Enable: true, Margin: 30},
			size:       "",
			want:       "春天的茶园\n\n【重要】图片四周需要预留空白边距：左右各至少 30 像素，上下各至少 30 像素。边缘区域应为纯色或渐变背景，不包含任何重要元素。",
		},
		{
			name:       "未启用水印时 prompt 不变",
			userPrompt: "春天的茶园",
			watermark:  &config.WatermarkConfig{Enable: false, Margin: 50},
			size:       "16:9",
			want:       "春天的茶园",
		},
		{
			name:       "启用水印但 margin 为 0 时 prompt 不变",
			userPrompt: "春天的茶园",
			watermark:  &config.WatermarkConfig{Enable: true, Margin: 0},
			size:       "16:9",
			want:       "春天的茶园",
		},
		{
			name:       "preset prompt 在无其他风格时生效",
			userPrompt: "封面图",
			want:       "预设风格\n\n封面图",
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
			// 对"preset prompt"测试用例注入预设 prompt
			if tt.name == "preset prompt 在无其他风格时生效" {
				p.presetPrompt = "预设风格"
			}
			got := p.buildPrompt(tt.userPrompt)
			if got != tt.want {
				t.Errorf("buildPrompt(%q) =\n  %q\nwant\n  %q", tt.userPrompt, got, tt.want)
			}
		})
	}
}

// TestProcessor_buildPrompt_preset 验证预设 prompt 三层回落链
func TestProcessor_buildPrompt_preset(t *testing.T) {
	tests := []struct {
		name         string
		configStyle  string
		cliStyle     string
		presetPrompt string
		userPrompt   string
		want         string
	}{
		{
			name:         "仅 preset 无其他风格",
			presetPrompt: "预设风格",
			userPrompt:   "封面图",
			want:         "预设风格\n\n封面图",
		},
		{
			name:         "config style_prompt 优先于 preset",
			configStyle:  "config风格",
			presetPrompt: "预设风格",
			userPrompt:   "封面图",
			want:         "config风格\n\n封面图",
		},
		{
			name:         "CLI --style 优先于 preset 和 config",
			cliStyle:     "CLI风格",
			configStyle:  "config风格",
			presetPrompt: "预设风格",
			userPrompt:   "封面图",
			want:         "CLI风格\n\n封面图",
		},
		{
			name:       "三者均空时直接返回 prompt",
			userPrompt: "封面图",
			want:       "封面图",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCfg := &config.ImageAPI{StylePrompt: tt.configStyle}
			p := newTestProcessor(apiCfg)
			p.stylePrompt = tt.cliStyle
			p.presetPrompt = tt.presetPrompt
			got := p.buildPrompt(tt.userPrompt)
			if got != tt.want {
				t.Errorf("buildPrompt(%q) =\n  %q\nwant\n  %q", tt.userPrompt, got, tt.want)
			}
		})
	}
}
