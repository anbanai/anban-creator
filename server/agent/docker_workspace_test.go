package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDockerHostWorkspacePreservesModesAndDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "input.txt")
	if err := os.WriteFile(file, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(nested, "tool.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}

	if err := validateDockerHostWorkspace(root); err != nil {
		t.Fatalf("validateDockerHostWorkspace: %v", err)
	}
	for _, tc := range []struct {
		path string
		want os.FileMode
	}{
		{path: nested, want: 0o700},
		{path: file, want: 0o600},
		{path: executable, want: 0o700},
		{path: outside, want: 0o600},
	} {
		info, err := os.Stat(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tc.want {
			t.Fatalf("%s mode = %o, want %o", tc.path, info.Mode().Perm(), tc.want)
		}
	}
}

func TestValidateDockerHostWorkspaceRejectsNonDirectoryRoots(t *testing.T) {
	file := filepath.Join(t.TempDir(), "workspace.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(t.TempDir(), symlink); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		root string
	}{
		{name: "missing", root: filepath.Join(t.TempDir(), "missing")},
		{name: "file", root: file},
		{name: "symlink", root: symlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDockerHostWorkspace(tc.root); err == nil || !strings.Contains(err.Error(), "real directory") {
				t.Fatalf("validateDockerHostWorkspace(%q) error = %v, want real directory error", tc.root, err)
			}
		})
	}
}
