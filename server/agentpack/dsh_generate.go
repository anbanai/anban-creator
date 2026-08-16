package agentpack

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type generatedPresetFile struct {
	body []byte
	mode fs.FileMode
}

const generatedDSHPresetsPath = "dsh/presets"

type generatedPresetTree struct {
	root *os.Root
}

func openGeneratedPresetTree(outputRoot string, create bool) (*generatedPresetTree, bool, error) {
	cleanOutputRoot := filepath.Clean(outputRoot)
	baseRoot := filepath.Dir(filepath.Dir(cleanOutputRoot))
	if cleanOutputRoot != filepath.Join(baseRoot, filepath.FromSlash(generatedDSHPresetsPath)) {
		return nil, false, fmt.Errorf("generated DSH Preset root must end in %s: %s", generatedDSHPresetsPath, outputRoot)
	}

	baseInfo, err := os.Lstat(baseRoot)
	if os.IsNotExist(err) {
		if !create {
			return nil, false, nil
		}
		if err := os.MkdirAll(baseRoot, 0o755); err != nil {
			return nil, false, fmt.Errorf("create generated output root %s: %w", baseRoot, err)
		}
	} else if err != nil {
		return nil, false, fmt.Errorf("stat generated output root %s: %w", baseRoot, err)
	} else if baseInfo.Mode()&fs.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("generated output root %s is a symlink", baseRoot)
	} else if !baseInfo.IsDir() {
		return nil, false, fmt.Errorf("generated output root %s is not a directory", baseRoot)
	}

	root, err := os.OpenRoot(baseRoot)
	if err != nil {
		return nil, false, fmt.Errorf("open generated output root %s: %w", baseRoot, err)
	}
	changed := false
	for _, component := range []string{"dsh", generatedDSHPresetsPath} {
		info, err := root.Lstat(component)
		if os.IsNotExist(err) {
			if !create {
				_ = root.Close()
				return nil, false, nil
			}
			if err := root.Mkdir(component, 0o755); err != nil {
				_ = root.Close()
				return nil, false, fmt.Errorf("create generated directory %s: %w", filepath.Join(baseRoot, filepath.FromSlash(component)), err)
			}
			changed = true
			continue
		}
		if err != nil {
			_ = root.Close()
			return nil, false, fmt.Errorf("stat generated directory %s: %w", filepath.Join(baseRoot, filepath.FromSlash(component)), err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			_ = root.Close()
			return nil, false, fmt.Errorf("generated directory %s is a symlink", filepath.Join(baseRoot, filepath.FromSlash(component)))
		}
		if !info.IsDir() {
			_ = root.Close()
			return nil, false, fmt.Errorf("generated directory %s is not a directory", filepath.Join(baseRoot, filepath.FromSlash(component)))
		}
	}
	return &generatedPresetTree{root: root}, changed, nil
}

func expectedDSHPresetFiles(pluginRoot string, catalog *Catalog) (map[string]generatedPresetFile, error) {
	files := make(map[string]generatedPresetFile)
	for _, pack := range catalog.Packs {
		if pack.Agent.DSHSource == "" {
			continue
		}

		sourcePath, err := securePackFile(pack.dir, pack.Agent.DSHSource)
		if err != nil {
			return nil, fmt.Errorf("resolve DSH Agent source for Pack %q: %w", pack.ID, err)
		}
		body, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read DSH Agent source for Pack %q: %w", pack.ID, err)
		}
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("stat DSH Agent source for Pack %q: %w", pack.ID, err)
		}
		presetRoot := filepath.ToSlash(pack.Agent.Name)
		files[path.Join(presetRoot, "agent.cordis.yml")] = generatedPresetFile{
			body: body,
			mode: normalizedGeneratedMode(info.Mode()),
		}

		metadata, err := yaml.Marshal(struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}{
			Name:        pack.DisplayName,
			Description: pack.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("encode DSH Preset metadata for Pack %q: %w", pack.ID, err)
		}
		files[path.Join(presetRoot, "preset.yml")] = generatedPresetFile{body: metadata, mode: 0o644}

		for _, skill := range pack.Agent.Skills {
			skillRoot := filepath.Join(pluginRoot, "skills", skill)
			err := filepath.WalkDir(skillRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Name() == ".git" {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.Type()&fs.ModeSymlink != 0 {
					return fmt.Errorf("referenced Skill %q contains symlink %q", skill, sourcePath)
				}
				if entry.IsDir() {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("referenced Skill %q contains non-regular file %q", skill, sourcePath)
				}
				relative, err := filepath.Rel(skillRoot, sourcePath)
				if err != nil {
					return err
				}
				body, err := os.ReadFile(sourcePath)
				if err != nil {
					return err
				}
				key := path.Join(presetRoot, "skills", skill, filepath.ToSlash(relative))
				files[key] = generatedPresetFile{body: body, mode: normalizedGeneratedMode(info.Mode())}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("copy Skill %q for DSH Preset %q: %w", skill, pack.Agent.Name, err)
			}
		}
	}
	return files, nil
}

func generateDSHPresets(pluginRoot, outputRoot string, catalog *Catalog) (bool, error) {
	expected, err := expectedDSHPresetFiles(pluginRoot, catalog)
	if err != nil {
		return false, err
	}
	if len(expected) == 0 {
		tree, _, err := openGeneratedPresetTree(outputRoot, false)
		if err != nil {
			return false, err
		}
		if tree == nil {
			return false, nil
		}
		defer tree.root.Close()
		if err := tree.root.RemoveAll(generatedDSHPresetsPath); err != nil {
			return false, fmt.Errorf("remove generated DSH Presets: %w", err)
		}
		return true, nil
	}

	tree, changed, err := openGeneratedPresetTree(outputRoot, true)
	if err != nil {
		return false, err
	}
	defer tree.root.Close()
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		destination := path.Join(generatedDSHPresetsPath, key)
		parentChanged, err := ensureGeneratedParent(tree.root, path.Dir(destination))
		if err != nil {
			return false, err
		}
		changed = changed || parentChanged
		fileChanged, err := writeGeneratedFileIfChanged(tree.root, destination, expected[key])
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}

	expectedDirs := generatedPresetDirectories(expected)
	var staleFiles, staleDirs []string
	if err := fs.WalkDir(tree.root.FS(), generatedDSHPresetsPath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == generatedDSHPresetsPath {
			return nil
		}
		relative := strings.TrimPrefix(current, generatedDSHPresetsPath+"/")
		if entry.IsDir() {
			if !expectedDirs[relative] {
				staleDirs = append(staleDirs, current)
			}
			return nil
		}
		if _, ok := expected[relative]; !ok {
			staleFiles = append(staleFiles, current)
		}
		return nil
	}); err != nil {
		return false, fmt.Errorf("walk generated DSH Presets: %w", err)
	}
	for _, current := range staleFiles {
		if err := tree.root.Remove(current); err != nil {
			return false, fmt.Errorf("remove stale generated DSH Preset file %s: %w", current, err)
		}
		changed = true
	}
	for i := len(staleDirs) - 1; i >= 0; i-- {
		if err := tree.root.Remove(staleDirs[i]); err != nil {
			return false, fmt.Errorf("remove stale generated DSH Preset directory %s: %w", staleDirs[i], err)
		}
		changed = true
	}
	return changed, nil
}

func checkDSHPresets(pluginRoot, outputRoot string, catalog *Catalog) error {
	expected, err := expectedDSHPresetFiles(pluginRoot, catalog)
	if err != nil {
		return err
	}
	tree, _, err := openGeneratedPresetTree(outputRoot, false)
	if err != nil {
		return err
	}
	if tree == nil {
		if len(expected) == 0 {
			return nil
		}
		return fmt.Errorf("generated DSH Preset drift: missing %s", outputRoot)
	}
	defer tree.root.Close()
	if len(expected) == 0 {
		return fmt.Errorf("generated DSH Preset drift: unexpected %s", outputRoot)
	}

	drift := make(map[string]bool)
	seen := make(map[string]bool, len(expected))
	expectedDirs := generatedPresetDirectories(expected)
	if err := fs.WalkDir(tree.root.FS(), generatedDSHPresetsPath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == generatedDSHPresetsPath {
			return nil
		}
		relative := strings.TrimPrefix(current, generatedDSHPresetsPath+"/")
		if entry.IsDir() {
			if !expectedDirs[relative] {
				drift[relative] = true
			}
			return nil
		}
		want, ok := expected[relative]
		if !ok {
			drift[relative] = true
			return nil
		}
		seen[relative] = true
		info, err := entry.Info()
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			drift[relative] = true
			return nil
		}
		body, err := tree.root.ReadFile(current)
		if err != nil || !bytes.Equal(body, want.body) || !generatedModeMatches(info.Mode(), want.mode) {
			drift[relative] = true
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk generated DSH Presets: %w", err)
	}
	for relative := range expected {
		if !seen[relative] {
			drift[relative] = true
		}
	}
	if len(drift) == 0 {
		return nil
	}
	paths := make([]string, 0, len(drift))
	for relative := range drift {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return fmt.Errorf("generated DSH Preset drift: %s", strings.Join(paths, ", "))
}

func normalizedGeneratedMode(mode fs.FileMode) fs.FileMode {
	if mode.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func generatedModeMatches(got, want fs.FileMode) bool {
	return got.Perm() == want.Perm() && got&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == 0
}

func generatedPresetDirectories(files map[string]generatedPresetFile) map[string]bool {
	directories := make(map[string]bool)
	for relative := range files {
		for parent := path.Dir(relative); parent != "."; parent = path.Dir(parent) {
			directories[parent] = true
		}
	}
	return directories
}

func ensureGeneratedDirectory(root *os.Root, directory string) (bool, error) {
	info, err := root.Lstat(directory)
	if os.IsNotExist(err) {
		if err := root.Mkdir(directory, 0o755); err != nil {
			return false, fmt.Errorf("create generated DSH Preset directory %s: %w", directory, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat generated DSH Preset directory %s: %w", directory, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 && info.IsDir() {
		return false, nil
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return false, fmt.Errorf("generated DSH Preset directory %s is a symlink", directory)
	}
	if err := root.RemoveAll(directory); err != nil {
		return false, fmt.Errorf("replace generated DSH Preset directory %s: %w", directory, err)
	}
	if err := root.Mkdir(directory, 0o755); err != nil {
		return false, fmt.Errorf("create generated DSH Preset directory %s: %w", directory, err)
	}
	return true, nil
}

func ensureGeneratedParent(root *os.Root, directory string) (bool, error) {
	if directory == "." {
		return false, nil
	}
	changed := false
	current := ""
	for _, component := range strings.Split(directory, "/") {
		current = path.Join(current, component)
		itemChanged, err := ensureGeneratedDirectory(root, current)
		if err != nil {
			return false, err
		}
		changed = changed || itemChanged
	}
	return changed, nil
}

func writeGeneratedFileIfChanged(root *os.Root, destination string, file generatedPresetFile) (bool, error) {
	info, err := root.Lstat(destination)
	if err == nil && info.Mode()&fs.ModeSymlink == 0 && info.Mode().IsRegular() {
		current, readErr := root.ReadFile(destination)
		bodyChanged := !bytes.Equal(current, file.body)
		modeChanged := !generatedModeMatches(info.Mode(), file.mode)
		if readErr == nil && !bodyChanged && !modeChanged {
			return false, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat generated file %s: %w", destination, err)
	}

	temporary, temporaryName, err := createGeneratedTemp(root, path.Dir(destination), path.Base(destination), file.mode)
	if err != nil {
		return false, err
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = root.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(file.body); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("write generated temporary file %s: %w", temporaryName, err)
	}
	if err := temporary.Chmod(file.mode); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("set generated temporary file mode %s: %w", temporaryName, err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close generated temporary file %s: %w", temporaryName, err)
	}

	if info != nil && (info.IsDir() || !info.Mode().IsRegular()) && info.Mode()&fs.ModeSymlink == 0 {
		if err := root.RemoveAll(destination); err != nil {
			return false, fmt.Errorf("replace generated file %s: %w", destination, err)
		}
	}
	if info != nil && info.Mode()&fs.ModeSymlink != 0 {
		if err := root.Remove(destination); err != nil {
			return false, fmt.Errorf("replace generated symlink %s: %w", destination, err)
		}
	}
	if err := root.Rename(temporaryName, destination); err != nil {
		return false, fmt.Errorf("replace generated file %s: %w", destination, err)
	}
	removeTemporary = false
	return true, nil
}

func createGeneratedTemp(root *os.Root, directory, base string, mode fs.FileMode) (*os.File, string, error) {
	for range 100 {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", fmt.Errorf("create generated temporary name: %w", err)
		}
		name := path.Join(directory, "."+base+".tmp-"+hex.EncodeToString(random[:]))
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return file, name, nil
		}
		if os.IsExist(err) {
			continue
		}
		return nil, "", fmt.Errorf("create generated temporary file %s: %w", name, err)
	}
	return nil, "", fmt.Errorf("create generated temporary file for %s: too many collisions", base)
}
