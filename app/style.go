package main

import (
	"github.com/royalrick/wechatwriter/app/image"
	"github.com/spf13/cobra"
)

// styleCmd 视觉风格预设命令组
func styleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "style",
		Short: "视觉风格预设管理",
		Long: `视觉风格预设管理命令组

支持的操作：
  list   - 列出所有可用的视觉风格预设
  show   - 查看指定预设的详细配置`,
	}

	cmd.AddCommand(styleListCmd())
	cmd.AddCommand(styleShowCmd())

	return cmd
}

// styleListCmd 列出所有预设
func styleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有可用的视觉风格预设",
		Long: `列出所有可用的视觉风格预设

预设名称可直接用于 image generate --style 参数，例如：
  wechatwriter image generate "封面标题" --post --style morandi-flat

示例:
  wechatwriter style list`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm := image.NewStylePresetManager()
			presets := pm.ListPresets()

			type presetSummary struct {
				EnglishName string `json:"english_name"`
				Name        string `json:"name"`
				Category    string `json:"category"`
				Description string `json:"description"`
			}

			summaries := make([]presetSummary, 0, len(presets))
			for _, p := range presets {
				summaries = append(summaries, presetSummary{
					EnglishName: p.EnglishName,
					Name:        p.Name,
					Category:    p.Category,
					Description: p.Description,
				})
			}

			responseSuccess(map[string]any{
				"presets": summaries,
				"count":   len(summaries),
			})
		},
	}
}

// styleShowCmd 展示预设详情
func styleShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "查看指定视觉风格预设的详细配置",
		Long: `查看指定视觉风格预设的详细配置，包含配色、边框、背景、字体和完整提示词

示例:
  wechatwriter style show morandi-flat
  wechatwriter style show tech-dark`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			pm := image.NewStylePresetManager()
			preset, err := pm.GetPreset(name)
			if err != nil {
				responseError(err)
				return
			}

			data := map[string]any{
				"name":         preset.Name,
				"english_name": preset.EnglishName,
				"category":     preset.Category,
				"description":  preset.Description,
				"prompt":       preset.Prompt,
			}
			if preset.Design != nil {
				data["design"] = preset.Design
			}
			if preset.Colors != nil {
				data["colors"] = preset.Colors
			}
			if preset.Border != nil {
				data["border"] = preset.Border
			}
			if preset.Background != nil {
				data["background"] = preset.Background
			}
			if preset.Typography != nil {
				data["typography"] = preset.Typography
			}

			responseSuccess(data)
		},
	}
}
