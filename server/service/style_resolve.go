package service

import (
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
)

// ResolveStyle and Resolved live in the leaf resolver package so the agent package
// (which service imports) can share the single resolution primitive without an
// import cycle. Re-exported here so service-internal callers and tests reference
// service.* and stay decoupled from the resolver import.
//
// Resolution is two-layer: task.Overrides.X (when non-empty) > project.X. See
// resolver.ResolveStyle for the full contract.
type Resolved = resolver.Resolved

// ResolveStyle resolves a task's effective style/author/theme dimensions
// (task override > project). Thin pass-through to the single primitive in the
// resolver package.
func ResolveStyle(project *model.Project, task *model.Task) resolver.Resolved {
	return resolver.ResolveStyle(project, task)
}

// firstNonEmpty returns the first non-empty string in args, or "" if all empty.
// Used by the few resolution call sites that still compose values from more than
// two layers (e.g. legacy migration, project-creation merging a template snapshot
// into the project). Runtime task>project resolution uses ResolveStyle above.
func firstNonEmpty(args ...string) string {
	for _, s := range args {
		if s != "" {
			return s
		}
	}
	return ""
}
