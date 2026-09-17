package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Theme 主题定义 — 机器可读的结构化排版规格，供确定性渲染器消费。
//
// 旧版的 prompt: 字段（喂给 LLM 的提示词）已移除：渲染现在由 render.go 确定性地
// 完成，主题完全由以下结构化块驱动。三个维度的设计值都内联到 HTML style 中。
type Theme struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"` // "deterministic"
	Description string            `yaml:"description"`
	Version     string            `yaml:"version"`
	StyleInfo   ThemeStyleInfo    `yaml:"style_info,omitempty"`
	Colors      map[string]string `yaml:"colors,omitempty"`
	Typography  ThemeTypography   `yaml:"typography,omitempty"`
	Layout      ThemeLayout       `yaml:"layout,omitempty"`
	Modules     ThemeModules      `yaml:"modules,omitempty"`
}

// ThemeStyleInfo 主题风格信息
type ThemeStyleInfo struct {
	Mood    string `yaml:"mood"`
	Colors  string `yaml:"colors"`
	BestFor string `yaml:"best_for"`
}

// ThemeTypography 字体/字号/行高/字间距。
type ThemeTypography struct {
	FontFamily    string `yaml:"font_family,omitempty"`
	FontSize      string `yaml:"font_size,omitempty"`
	LineHeight    string `yaml:"line_height,omitempty"`
	LetterSpacing string `yaml:"letter_spacing,omitempty"`
}

// ThemeLayout 容器与卡片的布局参数。
type ThemeLayout struct {
	ContainerPadding    string `yaml:"container_padding,omitempty"`
	MaxWidth            string `yaml:"max_width,omitempty"`
	CardPadding         string `yaml:"card_padding,omitempty"`
	SectionGap          string `yaml:"section_gap,omitempty"`
	ParagraphMargin     string `yaml:"paragraph_margin,omitempty"`
	BorderRadius        string `yaml:"border_radius,omitempty"`
	CardBackgroundColor string `yaml:"card_background_color,omitempty"`
	CardBackgroundImage string `yaml:"card_background_image,omitempty"`
	CardBackgroundSize  string `yaml:"card_background_size,omitempty"`
	CardBorder          string `yaml:"card_border,omitempty"`
	CardBoxShadow       string `yaml:"card_box_shadow,omitempty"`
}

// ThemeModules 元素级样式（标题/加粗/引用/分割线），render.go 按块类型应用。
type ThemeModules struct {
	H2         ThemeHeading `yaml:"h2,omitempty"`
	H3         ThemeHeading `yaml:"h3,omitempty"`
	Strong     ThemeTextMod `yaml:"strong,omitempty"`
	Blockquote ThemeBlock   `yaml:"blockquote,omitempty"`
	HR         ThemeHR      `yaml:"hr,omitempty"`
}

// ThemeHeading 标题样式：H2 带图标 span + 虚线下划线；H3 带实线下划线。
type ThemeHeading struct {
	Icon           string `yaml:"icon,omitempty"`
	IconColor      string `yaml:"icon_color,omitempty"`
	IconTextShadow string `yaml:"icon_text_shadow,omitempty"`
	TextColor      string `yaml:"text_color,omitempty"`
	BorderBottom   string `yaml:"border_bottom,omitempty"`
}

// ThemeTextMod 行内文本修饰（如 strong 的着色）。
type ThemeTextMod struct {
	Color string `yaml:"color,omitempty"`
}

// ThemeBlock 块级容器样式（如 blockquote）。
type ThemeBlock struct {
	BackgroundColor string `yaml:"background_color,omitempty"`
	BorderLeft      string `yaml:"border_left,omitempty"`
	BoxShadow       string `yaml:"box_shadow,omitempty"`
}

// ThemeHR 分割线样式。
type ThemeHR struct {
	Border     string `yaml:"border,omitempty"`
	Height     string `yaml:"height,omitempty"`
	Background string `yaml:"background,omitempty"`
}

// ThemeManager 主题管理器
type ThemeManager struct {
	themes map[string]Theme
}

// NewThemeManager 创建主题管理器
func NewThemeManager() *ThemeManager {
	return &ThemeManager{
		themes: make(map[string]Theme),
	}
}

// LoadThemes 从 YAML 文件加载主题
func (tm *ThemeManager) LoadThemes() error {
	// 获取主题目录
	themeDir := tm.getThemeDir()

	// 遍历主题目录
	entries, err := os.ReadDir(themeDir)
	if err != nil {
		// 如果主题目录不存在，返回空（不是错误）
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read theme directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// 只处理 .yaml 文件
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		// 加载主题文件
		themePath := filepath.Join(themeDir, entry.Name())
		if err := tm.loadThemeFromFile(themePath); err != nil {
			return fmt.Errorf("load theme from %s: %w", themePath, err)
		}
	}

	return nil
}

// loadThemeFromFile 从文件加载单个主题
func (tm *ThemeManager) loadThemeFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var theme Theme
	if err := yaml.Unmarshal(data, &theme); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	// 验证主题
	if theme.Name == "" {
		return fmt.Errorf("theme name is required")
	}

	// 如果 description 为空，设置默认值
	if theme.Description == "" {
		theme.Description = theme.Name
	}

	tm.themes[theme.Name] = theme
	return nil
}

// getThemeDir 获取主题目录
func (tm *ThemeManager) getThemeDir() string {
	// Plugin root has highest priority
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		pluginThemes := filepath.Join(pluginRoot, "themes")
		if _, err := os.Stat(pluginThemes); err == nil {
			return pluginThemes
		}
	}

	// Executable-relative path
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir = filepath.Dir(realExe)
		}
		for _, candidate := range []string{
			filepath.Join(exeDir, "themes"),
			filepath.Join(exeDir, "..", "themes"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// CWD-relative
	if _, err := os.Stat("themes"); err == nil {
		return "themes"
	}

	// User config dirs
	homeDir, _ := os.UserHomeDir()
	userThemeDir := filepath.Join(homeDir, ".config", "anban-creator", "themes")
	if _, err := os.Stat(userThemeDir); err == nil {
		return userThemeDir
	}

	return "themes"
}

// LoadTheme 加载单个主题（支持自定义路径）
func (tm *ThemeManager) LoadTheme(path string) error {
	return tm.loadThemeFromFile(path)
}

// GetTheme 获取主题
func (tm *ThemeManager) GetTheme(name string) (*Theme, error) {
	// 如果主题未加载，尝试从文件加载
	if _, ok := tm.themes[name]; !ok {
		if err := tm.LoadThemes(); err != nil {
			return nil, fmt.Errorf("theme not found: %s (load error: %w)", name, err)
		}
	}

	theme, ok := tm.themes[name]
	if !ok {
		return nil, fmt.Errorf("theme not found: %s", name)
	}
	return &theme, nil
}

// ListThemes 列出所有主题
func (tm *ThemeManager) ListThemes() []string {
	var names []string
	for name := range tm.themes {
		names = append(names, name)
	}
	return names
}

// GetThemeDescription 获取主题描述
func (tm *ThemeManager) GetThemeDescription(name string) string {
	theme, err := tm.GetTheme(name)
	if err != nil {
		return "未知主题"
	}
	return theme.Description
}

// GetThemeColors 获取主题颜色配置
func (tm *ThemeManager) GetThemeColors(name string) (map[string]string, error) {
	theme, err := tm.GetTheme(name)
	if err != nil {
		return nil, err
	}
	return theme.Colors, nil
}

// RegisterTheme adds a pre-parsed theme to the manager without filesystem access.
func (tm *ThemeManager) RegisterTheme(theme Theme) {
	if theme.Name == "" {
		return
	}
	if theme.Description == "" {
		theme.Description = theme.Name
	}
	tm.themes[theme.Name] = theme
}

// LoadFromBytes parses a theme from YAML bytes and registers it.
func (tm *ThemeManager) LoadFromBytes(data []byte) error {
	var theme Theme
	if err := yaml.Unmarshal(data, &theme); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	if theme.Name == "" {
		return fmt.Errorf("theme name is required")
	}
	tm.RegisterTheme(theme)
	return nil
}

// ReloadThemes 重新加载所有主题
func (tm *ThemeManager) ReloadThemes() error {
	tm.themes = make(map[string]Theme)
	return tm.LoadThemes()
}

// GetThemeInfo 获取主题完整信息（用于调试）
func (tm *ThemeManager) GetThemeInfo(name string) (*Theme, error) {
	return tm.GetTheme(name)
}

// EnsureLoaded 确保主题已加载
func (tm *ThemeManager) EnsureLoaded() error {
	if len(tm.themes) == 0 {
		return tm.LoadThemes()
	}
	return nil
}
