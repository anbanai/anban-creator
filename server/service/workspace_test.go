package service

import (
	"strings"
	"testing"
)

func TestWorkspacePrepare(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		taskID      string
		want        PrepareResult
		wantErr     string
	}{
		{
			name:    "missing content type",
			taskID:  "task-1",
			wantErr: "content_type is required",
		},
		{
			name:        "blank content type",
			contentType: "   ",
			taskID:      "task-1",
			wantErr:     "content_type is required",
		},
		{
			name:        "missing task ID",
			contentType: "seednote",
			wantErr:     "task_id is required",
		},
		{
			name:        "blank task ID",
			contentType: "seednote",
			taskID:      "   ",
			wantErr:     "task_id is required",
		},
		{
			name:        "managed task workspace",
			contentType: "seednote",
			taskID:      "task-1",
			want:        PrepareResult{Path: "output"},
		},
	}

	service := NewWorkspaceService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.Prepare(tt.contentType, tt.taskID)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Prepare() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
			if got == nil || *got != tt.want {
				t.Fatalf("Prepare() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
