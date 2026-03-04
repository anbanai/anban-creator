package image

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// StylePreset 视觉风格预设
type StylePreset struct {
	Name        string         `yaml:"name"`
	EnglishName string         `yaml:"english_name"`
	Description string         `yaml:"description"`
	Category    string         `yaml:"category"`
	Design      *DesignDef     `yaml:"design,omitempty"`
	Colors      *ColorsDef     `yaml:"colors,omitempty"`
	Border      *BorderDef     `yaml:"border,omitempty"`
	Background  *BackgroundDef `yaml:"background,omitempty"`
	Typography  *TypographyDef `yaml:"typography,omitempty"`
	Prompt      string         `yaml:"prompt"`
}

// DesignDef 设计定义
type DesignDef struct {
	Style string `yaml:"style" json:"style"`
	Mood  string `yaml:"mood"  json:"mood"`
}

// ColorsDef 配色定义
type ColorsDef struct {
	Primary    string `yaml:"primary"    json:"primary"`
	Secondary  string `yaml:"secondary"  json:"secondary"`
	Accent     string `yaml:"accent"     json:"accent"`
	Background string `yaml:"background" json:"background"`
	Text       string `yaml:"text"       json:"text"`
}

// BorderDef 边框定义
type BorderDef struct {
	Style  string `yaml:"style"  json:"style"`
	Radius string `yaml:"radius" json:"radius"`
}

// BackgroundDef 背景定义
type BackgroundDef struct {
	Type        string `yaml:"type"        json:"type"`
	Description string `yaml:"description" json:"description"`
}

// TypographyDef 字体定义
type TypographyDef struct {
	Heading string `yaml:"heading" json:"heading"`
	Body    string `yaml:"body"    json:"body"`
}

// StylePresetManager 管理视觉风格预设
type StylePresetManager struct {
	presets     map[string]*StylePreset
	stylesDir   string
	initialized bool
}

// NewStylePresetManager 创建视觉风格预设管理器
func NewStylePresetManager() *StylePresetManager {
	return &StylePresetManager{
		presets: make(map[string]*StylePreset),
	}
}

// LoadPresets 从 styles/ 目录加载所有预设
func (m *StylePresetManager) LoadPresets() error {
	if m.stylesDir == "" {
		m.stylesDir = m.getStylesDir()
	}

	if _, err := os.Stat(m.stylesDir); os.IsNotExist(err) {
		// 目录不存在，不是错误，只是没有预设
		m.initialized = true
		return nil
	}

	entries, err := os.ReadDir(m.stylesDir)
	if err != nil {
		return fmt.Errorf("读取 styles 目录: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		presetPath := filepath.Join(m.stylesDir, name)
		// 加载失败的单个文件跳过，不中断整体加载
		_ = m.loadPreset(presetPath)
	}

	m.initialized = true
	return nil
}

// loadPreset 加载单个预设文件
func (m *StylePresetManager) loadPreset(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件: %w", err)
	}

	var preset StylePreset
	if err := yaml.Unmarshal(data, &preset); err != nil {
		return fmt.Errorf("解析 YAML: %w", err)
	}

	if preset.EnglishName == "" {
		return fmt.Errorf("缺少必需字段: english_name")
	}
	if preset.Prompt == "" {
		return fmt.Errorf("缺少必需字段: prompt")
	}
	if preset.Name == "" {
		preset.Name = preset.EnglishName
	}
	if preset.Category == "" {
		preset.Category = "自定义"
	}

	m.presets[preset.EnglishName] = &preset
	return nil
}

// getStylesDir 获取 styles 目录路径（搜索链与 writers/themes 保持一致）
func (m *StylePresetManager) getStylesDir() string {
	var paths []string

	// Plugin root 优先（作为 Claude Code plugin 运行时）
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		paths = append(paths, filepath.Join(pluginRoot, "styles"))
	}

	// 可执行文件相对路径
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir = filepath.Dir(realExe)
		}
		paths = append(paths,
			filepath.Join(exeDir, "styles"),
			filepath.Join(exeDir, "..", "styles"),
		)
	}

	paths = append(paths,
		"styles",
		filepath.Join(os.Getenv("HOME"), ".config", "wechatwriter", "styles"),
		filepath.Join(os.Getenv("HOME"), ".wechatwriter", "styles"),
	)

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "styles"
}

// ensureLoaded 确保已加载（懒加载）
func (m *StylePresetManager) ensureLoaded() {
	if !m.initialized {
		_ = m.LoadPresets()
	}
}

// GetPreset 按名称获取预设（支持 english_name 和中文 name）
func (m *StylePresetManager) GetPreset(name string) (*StylePreset, error) {
	m.ensureLoaded()

	// 精确匹配 english_name
	if preset, ok := m.presets[name]; ok {
		return preset, nil
	}

	// fallback：按中文 name 匹配
	for _, preset := range m.presets {
		if preset.Name == name {
			return preset, nil
		}
	}

	return nil, fmt.Errorf("视觉风格预设未找到: %s（使用 `wechatwriter style list` 查看可用预设）", name)
}

// GetPrompt 获取预设的 prompt 字符串
func (m *StylePresetManager) GetPrompt(name string) (string, error) {
	preset, err := m.GetPreset(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(preset.Prompt), nil
}

// ListPresets 列出所有预设摘要
func (m *StylePresetManager) ListPresets() []*StylePreset {
	m.ensureLoaded()

	result := make([]*StylePreset, 0, len(m.presets))
	for _, preset := range m.presets {
		result = append(result, preset)
	}
	return result
}

// Count 返回已加载的预设数量
func (m *StylePresetManager) Count() int {
	m.ensureLoaded()
	return len(m.presets)
}
