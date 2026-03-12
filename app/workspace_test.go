package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspacePrepareCmd(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		setup       func(stagingDir string) // optional: create files in staging before prepare
		wantErr     bool
		wantErrMsg  string
		wantArchive bool // expect a YYYYMMDD-NNN dir to exist after
	}{
		{
			name:        "no staging dir → creates new",
			contentType: "articles",
			setup:       nil,
			wantArchive: false,
		},
		{
			name:        "staging exists but empty → keeps, no archive",
			contentType: "articles",
			setup: func(stagingDir string) {
				_ = os.MkdirAll(stagingDir, 0o755)
			},
			wantArchive: false,
		},
		{
			name:        "staging exists with files → archives then creates new",
			contentType: "posts",
			setup: func(stagingDir string) {
				_ = os.MkdirAll(stagingDir, 0o755)
				_ = os.WriteFile(filepath.Join(stagingDir, "image_01.png"), []byte("data"), 0o644)
			},
			wantArchive: true,
		},
		{
			name:        "invalid contentType → error",
			contentType: "invalid",
			wantErr:     true,
			wantErrMsg:  "invalid content type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run in a temp dir to avoid polluting the real output/
			tmpDir := t.TempDir()
			origDir, _ := os.Getwd()
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			defer func() { _ = os.Chdir(origDir) }()

			stagingDir := filepath.Join("output", tt.contentType, "staging")
			if tt.setup != nil {
				tt.setup(stagingDir)
			}

			cmd := workspacePrepareCmd()
			cmd.SetArgs([]string{tt.contentType})
			err := cmd.Execute()

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// staging dir must exist and be empty
			entries, err := os.ReadDir(stagingDir)
			if err != nil {
				t.Fatalf("staging dir not created: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("staging dir should be empty, got %d entries", len(entries))
			}

			// check for archive dir
			base := filepath.Join("output", tt.contentType)
			dirs, _ := os.ReadDir(base)
			var archiveDirs []string
			for _, d := range dirs {
				if d.Name() != "staging" {
					archiveDirs = append(archiveDirs, d.Name())
				}
			}

			if tt.wantArchive && len(archiveDirs) == 0 {
				t.Error("expected an archive dir, found none")
			}
			if !tt.wantArchive && len(archiveDirs) > 0 {
				t.Errorf("expected no archive dir, found: %v", archiveDirs)
			}
		})
	}
}

func TestNextArchiveDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	_ = os.MkdirAll(filepath.Join("output", "articles"), 0o755)

	// First slot
	dir1, err := nextArchiveDir("articles")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := os.Mkdir(dir1, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Second slot should be different
	dir2, err := nextArchiveDir("articles")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir1 == dir2 {
		t.Errorf("expected different dirs, got %q twice", dir1)
	}
}
