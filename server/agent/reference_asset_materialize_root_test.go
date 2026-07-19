package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReferenceAssetRootCommitRejectsParentSwapWithoutEscaping(t *testing.T) {
	workDir := t.TempDir()
	external := t.TempDir()
	held := filepath.Join(workDir, ".anban-root-held")
	referenceMaterializeBeforeCommitHook = func() error {
		if err := os.Rename(filepath.Join(workDir, referenceImageDirName), held); err != nil {
			return err
		}
		return os.Symlink(external, filepath.Join(workDir, referenceImageDirName))
	}
	t.Cleanup(func() { referenceMaterializeBeforeCommitHook = nil })

	err := materializeReferenceAssetWithRoot(t.Context(), workDir, []byte("image"))
	if err == nil {
		t.Fatal("Root materializer accepted a swapped parent")
	}
	for _, path := range []string{
		filepath.Join(external, referenceImageFileName),
		filepath.Join(held, referenceImageFileName),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("reference escaped to %q: %v", path, statErr)
		}
	}
	temps, globErr := filepath.Glob(filepath.Join(held, ".reference-*.tmp"))
	if globErr != nil || len(temps) != 0 {
		t.Fatalf("temporary files after parent swap = %#v, err=%v", temps, globErr)
	}
}

func TestReferenceAssetRootCommitRejectsExistingParentSymlink(t *testing.T) {
	workDir := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(workDir, referenceImageDirName)); err != nil {
		t.Fatal(err)
	}

	if err := materializeReferenceAssetWithRoot(t.Context(), workDir, []byte("image")); err == nil {
		t.Fatal("Root materializer accepted an existing parent symlink")
	}
	if _, err := os.Stat(filepath.Join(external, referenceImageFileName)); !os.IsNotExist(err) {
		t.Fatalf("external reference created through parent symlink: %v", err)
	}
}

func TestReferenceAssetRootCommitAtomicallyReplacesTargetSymlink(t *testing.T) {
	workDir := t.TempDir()
	dir := filepath.Join(workDir, referenceImageDirName)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, referenceImageFileName)
	if err := os.Symlink(external, dest); err != nil {
		t.Fatal(err)
	}

	if err := materializeReferenceAssetWithRoot(t.Context(), workDir, []byte("image")); err != nil {
		t.Fatal(err)
	}
	outside, err := os.ReadFile(external)
	if err != nil || string(outside) != "outside" {
		t.Fatalf("external target = %q, err=%v", outside, err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("reference mode = %v, want regular 0600", info.Mode())
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "image" {
		t.Fatalf("reference = %q, err=%v", data, err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, err=%v", dirInfo.Mode(), err)
	}
}
