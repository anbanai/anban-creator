package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Theme 主题定义
type Theme struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Version     string            `yaml:"version"`
	StyleInfo   ThemeStyleInfo    `yaml:"style_info,omitempty"`
	Colors      map[string]string `yaml:"colors,omitempty"`
	Prompt      string            `yaml:"prompt,omitempty"`
}

// ThemeStyleInfo 主题风格信息
type ThemeStyleInfo struct {
	Mood    string `yaml:"mood"`
	Colors  string `yaml:"colors"`
	BestFor string `yaml:"best_for"`
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
	userThemeDir := filepath.Join(homeDir, ".config", "anbanwriter", "themes")
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

// GetAIPrompt 获取 AI 模式的提示词
func (tm *ThemeManager) GetAIPrompt(name string) (string, error) {
	theme, err := tm.GetTheme(name)
	if err != nil {
		return "", err
	}
	if theme.Prompt == "" {
		return "", fmt.Errorf("theme '%s' has no prompt defined", name)
	}
	return theme.Prompt, nil
}

// BuildCustomAIPrompt 构建自定义 AI 提示词
func BuildCustomAIPrompt(customPrompt string) string {
	if customPrompt == "" {
		return customPrompt
	}

	// 确保包含基本规则
	baseRules := `

## 重要规则
1. 所有 CSS 必须使用内联 style 属性
2. 不使用外部样式表或 <style> 标签
3. 只使用安全的 HTML 标签（section, p, span, strong, em, a, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr）
4. 图片使用占位符格式：<!-- IMG:index -->
5. 返回完整的 HTML，不需要其他说明文字

`

	if !strings.Contains(customPrompt, "重要规则") && !strings.Contains(customPrompt, "规则") {
		customPrompt += baseRules
	}

	if !strings.Contains(customPrompt, "请转换") {
		customPrompt += "\n\n请转换以下 Markdown内容："
	}

	return customPrompt
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
