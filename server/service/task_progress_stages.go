package service

import "github.com/royalrick/anbanwriter/server/model"

// stagePercentByType maps task.Type → stage name → percent.
// Used as a fallback when update_task_progress is called without an explicit
// progress_percent, so the progress bar advances without requiring every
// skill to pass a number. Stages not listed here leave percent unchanged.
//
// Stage names are extracted from claudecode/agents/{wechatarticle,seednote}.md.
// Only article and seednote go through TaskService.UpdateProgress; other
// pipelines (designer, live-slicer, short-video-studio) have their own
// services and are out of scope here.
var stagePercentByType = map[string]map[string]int{
	model.ScopeArticle: {
		"research":     10,
		"outline":      20,
		"writing":      45,
		"humanize":     55,
		"seo":          65,
		"cover":        75,
		"illustration": 85,
		"html":         92,
		"draft":        100,
	},
	model.ScopeSeednote: {
		"project":          5,
		"research":         15,
		"viral_analysis":   30, // 改写分支：在 writing 之前
		"writing":          40,
		"image_generation": 65,
		"compliance":       85,
		"archive":          95,
		"finalize":         100,
	},
	// Ecommerce stage slugs come from claudecode/agents/ecommerce.md
	// update_task_progress calls (project → analysis → copywriting →
	// image_generation → compliance → archive). The bulk of work is image
	// generation; archive is the last progress event before the task completes.
	model.ScopeEcommerce: {
		"project":          5,
		"analysis":         20,
		"copywriting":      35,
		"image_generation": 70,
		"compliance":       90,
		"archive":          97,
	},
}

// defaultPercentForStage returns the percent for a (taskType, stage) pair.
// Returns 0 when type or stage is unknown — caller must not overwrite.
func defaultPercentForStage(taskType, stage string) int {
	if m, ok := stagePercentByType[taskType]; ok {
		return m[stage]
	}
	return 0
}
