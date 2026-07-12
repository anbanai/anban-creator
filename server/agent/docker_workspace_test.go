package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareDockerWorkspaceForNumericRuntimeMakesTreeWritableWithoutFollowingSymlinks(t *testing.T) {
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

	if err := prepareDockerWorkspaceForNumericRuntime(root); err != nil {
		t.Fatalf("prepareDockerWorkspaceForNumericRuntime: %v", err)
	}
	for _, path := range []string{root, nested} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o777 {
			t.Fatalf("directory %s mode = %o, want 777", path, info.Mode().Perm())
		}
	}
	for _, path := range []string{file, executable} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0o222 {
			t.Fatalf("file %s mode = %o, want writable by numeric container user", path, info.Mode().Perm())
		}
	}
	executableInfo, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	if executableInfo.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable mode = %o, want owner execute preserved", executableInfo.Mode().Perm())
	}
	outsideInfo, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideInfo.Mode().Perm() != 0o600 {
		t.Fatalf("symlink target mode = %o, want untouched 600", outsideInfo.Mode().Perm())
	}
}
