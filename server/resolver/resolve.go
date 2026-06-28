// Package resolver is the SINGLE source of truth for resolving a task's effective
// style/persona/theme dimensions. It is a leaf package (imports only server/model
// + app/writer) so that service (which imports agent), agent (which service
// imports), and mcp can ALL call it without an import cycle.
//
// Every delivery channel to the agent — get_project_profile (MCP),
// BuildUserPrompt (prompt), and settings.json (config_builder) — resolves through
// ResolveStyle, so the three channels can never disagree. This was the root cause
// the half-orthogonalized refactor kept re-implementing per layer.
package resolver

import (
	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/model"
)

// Resolved holds the effective (post-resolution) value and its source for every
// dimension a task carries. Resolution is STRICTLY two-layer:
//
//	task.Overrides.X (when non-empty)  >  project.X
//
// Source is "task" when the task overrode the dimension, "project" otherwise.
// The template/plan layers are GONE — a project is the single source of truth and
// a task only stores per-dimension overrides.
type Resolved struct {
	VisualStyle         string
	WriterKey           string
	WritingVoice        string
	Byline              string
	PersonaAvatar       string
	Theme               string
	VisualStyleSource   string // "task" | "project"
	WriterKeySource     string
	WritingVoiceSource  string
	BylineSource        string
	PersonaAvatarSource string
	ThemeSource         string
}

// ResolveStyle resolves every dimension two-layer (task override > project) and
// records each dimension's source. project MUST be non-nil; task may be nil (in
// which case every dimension is inherited from the project).
//
// Article platform always carries a writer voice (seednote/ecommerce have none):
// when the resolved writer key is empty on an article project, the platform
// default is applied here — once, in the single resolution primitive — so every
// consumer (MCP, prompt, settings.json) sees it consistently. It folds into the
// "project" source bucket (it is the platform's built-in writer).
func ResolveStyle(project *model.Project, task *model.Task) Resolved {
	r := Resolved{
		VisualStyle:         project.VisualStyle,
		WriterKey:           project.WriterKey,
		WritingVoice:        project.WritingVoice,
		Byline:              project.Byline,
		PersonaAvatar:       project.PersonaAvatar,
		Theme:               project.Theme,
		VisualStyleSource:   "project",
		WriterKeySource:     "project",
		WritingVoiceSource:  "project",
		BylineSource:        "project",
		PersonaAvatarSource: "project",
		ThemeSource:         "project",
	}
	if task != nil {
		o := task.Overrides.Data()
		if o.VisualStyle != "" {
			r.VisualStyle, r.VisualStyleSource = o.VisualStyle, "task"
		}
		if o.WriterKey != "" {
			r.WriterKey, r.WriterKeySource = o.WriterKey, "task"
		}
		if o.WritingVoice != "" {
			r.WritingVoice, r.WritingVoiceSource = o.WritingVoice, "task"
		}
		if o.Byline != "" {
			r.Byline, r.BylineSource = o.Byline, "task"
		}
		if o.PersonaAvatar != "" {
			r.PersonaAvatar, r.PersonaAvatarSource = o.PersonaAvatar, "task"
		}
		if o.Theme != "" {
			r.Theme, r.ThemeSource = o.Theme, "task"
		}
	}
	// Article always carries a writer voice; apply the platform default when the
	// project left it empty. One place, every consumer agrees.
	if project.Platform == model.PlatformArticle && r.WriterKey == "" {
		r.WriterKey = writer.DefaultStyleName
	}
	return r
}
