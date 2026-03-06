package image

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// StylePreset 视觉风格预设（对应 YAML 文件的关键字段）
type StylePreset struct {
	Name        string `yaml:"name"`
	EnglishName string `yaml:"english_name"`
	Description string `yaml:"description"`
	Category    string `yaml:"category"`
	Prompt      string `yaml:"prompt"` // 核心：直接可用的风格提示词
}

// StylePresetManager 视觉风格预设管理器
type StylePresetManager struct {
	presets     map[string]*StylePreset
	stylesDir   string
	initialized bool
}

// NewStylePresetManager 创建风格预设管理器
func NewStylePresetManager() *StylePresetManager {
	return &StylePresetManager{
		presets: make(map[string]*StylePreset),
	}
}

// LoadPresets 加载所有风格预设
func (spm *StylePresetManager) LoadPresets() error {
	if spm.stylesDir == "" {
		spm.stylesDir = spm.getStylesDir()
	}

	if _, err := os.Stat(spm.stylesDir); os.IsNotExist(err) {
		// 目录不存在，不是错误
		spm.initialized = true
		return nil
	}

	entries, err := os.ReadDir(spm.stylesDir)
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
		presetPath := filepath.Join(spm.stylesDir, name)
		// 忽略单个文件错误，继续加载其他预设
		_ = spm.loadPreset(presetPath)
	}

	spm.initialized = true
	return nil
}

// loadPreset 加载单个预设文件
func (spm *StylePresetManager) loadPreset(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件: %w", err)
	}

	var preset StylePreset
	if err := yaml.Unmarshal(data, &preset); err != nil {
		return fmt.Errorf("解析 YAML: %w", err)
	}

	if preset.Name == "" {
		return fmt.Errorf("缺少必需字段: name")
	}

	spm.presets[preset.Name] = &preset
	return nil
}

// getStylesDir 获取 styles 目录路径
func (spm *StylePresetManager) getStylesDir() string {
	const stylesSubPath = "skills/visual-design/references/styles"

	var paths []string

	// Plugin root has highest priority (when running as Claude Code plugin)
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		paths = append(paths, filepath.Join(pluginRoot, stylesSubPath))
	}

	// Executable-relative path (for installed binaries)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir = filepath.Dir(realExe)
		}
		paths = append(paths,
			filepath.Join(exeDir, stylesSubPath),
			filepath.Join(exeDir, "..", stylesSubPath),
		)
	}

	// CWD
	paths = append(paths, stylesSubPath)

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return stylesSubPath
}

// GetPreset 按名称获取预设（确保已加载）
func (spm *StylePresetManager) GetPreset(name string) (*StylePreset, error) {
	if !spm.initialized {
		if err := spm.LoadPresets(); err != nil {
			return nil, err
		}
	}

	preset, ok := spm.presets[name]
	if !ok {
		return nil, fmt.Errorf("视觉风格预设未找到: %s", name)
	}
	return preset, nil
}

// ListPresetNames 返回所有可用预设名
func (spm *StylePresetManager) ListPresetNames() []string {
	if !spm.initialized {
		_ = spm.LoadPresets()
	}

	names := make([]string, 0, len(spm.presets))
	for name := range spm.presets {
		names = append(names, name)
	}
	return names
}

// HasPreset 检查是否存在指定预设
func (spm *StylePresetManager) HasPreset(name string) bool {
	_, err := spm.GetPreset(name)
	return err == nil
}
