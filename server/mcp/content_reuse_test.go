package mcp

import "testing"

func TestContentReuseContextExecutionScope(t *testing.T) {
	for _, tc := range []struct {
		name, project, task string
		allowed             bool
	}{
		{"bound task", "project-1", "task-1", true},
		{"other task", "project-1", "task-2", false},
		{"other project", "project-2", "task-1", false},
		{"missing project", "", "task-1", false},
		{"missing task", "project-1", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExecutionToolScope("get_content_reuse_context", map[string]any{"project_id": tc.project, "task_id": tc.task}, "project-1", "task-1", "execution-1")
			if (err == nil) != tc.allowed {
				t.Fatalf("scope error=%v, allowed=%v", err, tc.allowed)
			}
		})
	}
}
