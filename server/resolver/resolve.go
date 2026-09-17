// Package resolver is the SINGLE source of truth for resolving a task's effective
// style/author/theme dimensions. It is a leaf package (imports only server/model
// + server/app/writer) so that service (which imports agent), agent (which service
// imports), and mcp can ALL call it without an import cycle.
//
// Every delivery channel to the agent — get_project_profile (MCP),
// BuildUserPrompt (prompt), and settings.json (config_builder) — resolves through
// ResolveStyle, so the three channels can never disagree. This was the root cause
// the half-orthogonalized refactor kept re-implementing per layer.
package resolver

import (
	"github.com/anbanai/anban-creator/server/app/writer"
	"github.com/anbanai/anban-creator/server/model"
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
	VisualStyle       string
	Writer            string
	Author            string
	Theme             string
	VisualStyleSource string // "task" | "project"
	WriterSource      string
	AuthorSource      string
	ThemeSource       string
}

// ResolveStyle resolves every dimension from the task's project snapshot when
// present, falling back to legacy two-layer task override > project behavior for
// old rows. project MUST be non-nil; task may be nil.
//
// Article platform always carries a writer voice (seednote/ecommerce have none):
// when the resolved writer key is empty on an article project, the platform
// default is applied here — once, in the single resolution primitive — so every
// consumer (MCP, prompt, settings.json) sees it consistently. It folds into the
// "project" source bucket (it is the platform's built-in writer).
func ResolveStyle(project *model.Project, task *model.Task) Resolved {
	if task != nil {
		if snap := task.ProjectSnapshot.Data(); snap.Platform != "" {
			r := Resolved{
				VisualStyle:       snap.VisualStyle,
				Writer:            snap.Writer,
				Author:            snap.Author,
				Theme:             snap.Theme,
				VisualStyleSource: "snapshot",
				WriterSource:      "snapshot",
				AuthorSource:      "snapshot",
				ThemeSource:       "snapshot",
			}
			if snap.Platform == model.PlatformArticle && r.Writer == "" {
				r.Writer = writer.DefaultStyleName
			}
			return r
		}
	}
	r := Resolved{
		VisualStyle:       project.VisualStyle,
		Writer:            project.Writer,
		Author:            project.Author,
		Theme:             project.Theme,
		VisualStyleSource: "project",
		WriterSource:      "project",
		AuthorSource:      "project",
		ThemeSource:       "project",
	}
	if task != nil {
		o := task.Overrides.Data()
		if o.VisualStyle != "" {
			r.VisualStyle, r.VisualStyleSource = o.VisualStyle, "task"
		}
		if o.Writer != "" {
			r.Writer, r.WriterSource = o.Writer, "task"
		}
		if o.Author != "" {
			r.Author, r.AuthorSource = o.Author, "task"
		}
		if o.Theme != "" {
			r.Theme, r.ThemeSource = o.Theme, "task"
		}
	}
	// Article always carries a writer voice; apply the platform default when the
	// project left it empty. One place, every consumer agrees.
	if project.Platform == model.PlatformArticle && r.Writer == "" {
		r.Writer = writer.DefaultStyleName
	}
	return r
}
