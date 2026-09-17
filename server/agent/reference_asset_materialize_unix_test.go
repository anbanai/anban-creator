//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestMaterializeReferenceAssetRejectsParentSymlinkWithoutTouchingTarget(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	external := t.TempDir()
	before, err := os.Stat(external)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(workDir, appconfig.ConfigDir)); err != nil {
		t.Fatal(err)
	}

	err = MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5})
	if err == nil {
		t.Fatal("parent symlink was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(external, taskReferenceImageFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("external reference created through parent symlink: %v", statErr)
	}
	after, statErr := os.Stat(external)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("external directory mode changed from %v to %v", before.Mode().Perm(), after.Mode().Perm())
	}
}

func TestMaterializeReferenceAssetParentSwapBeforeCommitDoesNotEscape(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	external := t.TempDir()
	held := filepath.Join(workDir, ".anban-held")
	referenceMaterializeBeforeCommitHook = func() error {
		if err := os.Rename(filepath.Join(workDir, appconfig.ConfigDir), held); err != nil {
			return err
		}
		return os.Symlink(external, filepath.Join(workDir, appconfig.ConfigDir))
	}
	t.Cleanup(func() { referenceMaterializeBeforeCommitHook = nil })

	err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5})
	if err == nil {
		t.Fatal("parent swap was accepted")
	}
	for _, path := range []string{
		filepath.Join(external, taskReferenceImageFileName),
		filepath.Join(held, taskReferenceImageFileName),
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

func TestMaterializeReferenceAssetWorkspaceSwapBeforeCommitDoesNotEscape(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	external := t.TempDir()
	held := workDir + "-held"
	referenceMaterializeBeforeCommitHook = func() error {
		if err := os.Rename(workDir, held); err != nil {
			return err
		}
		return os.Symlink(external, workDir)
	}
	t.Cleanup(func() {
		referenceMaterializeBeforeCommitHook = nil
		_ = os.Remove(workDir)
		_ = os.RemoveAll(held)
	})

	err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5})
	if err == nil {
		t.Fatal("workspace swap was accepted")
	}
	for _, path := range []string{
		filepath.Join(external, appconfig.ConfigDir, taskReferenceImageFileName),
		filepath.Join(held, appconfig.ConfigDir, taskReferenceImageFileName),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("reference escaped to %q: %v", path, statErr)
		}
	}
	temps, globErr := filepath.Glob(filepath.Join(held, appconfig.ConfigDir, ".reference-*.tmp"))
	if globErr != nil || len(temps) != 0 {
		t.Fatalf("temporary files after workspace swap = %#v, err=%v", temps, globErr)
	}
}
