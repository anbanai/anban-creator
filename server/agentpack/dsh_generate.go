package agentpack

import (
	"bytes"
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
		if _, err := os.Lstat(outputRoot); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("stat generated DSH Presets: %w", err)
		}
		if err := os.RemoveAll(outputRoot); err != nil {
			return false, fmt.Errorf("remove generated DSH Presets: %w", err)
		}
		return true, nil
	}

	changed, err := ensureGeneratedDirectory(outputRoot)
	if err != nil {
		return false, err
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		destination := filepath.Join(outputRoot, filepath.FromSlash(key))
		parentChanged, err := ensureGeneratedParent(outputRoot, filepath.Dir(filepath.FromSlash(key)))
		if err != nil {
			return false, err
		}
		changed = changed || parentChanged
		fileChanged, err := writeGeneratedFileIfChanged(destination, expected[key])
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}

	expectedDirs := generatedPresetDirectories(expected)
	var staleFiles, staleDirs []string
	if err := filepath.WalkDir(outputRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == outputRoot {
			return nil
		}
		relative, err := filepath.Rel(outputRoot, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
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
		if err := os.Remove(current); err != nil {
			return false, fmt.Errorf("remove stale generated DSH Preset file %s: %w", current, err)
		}
		changed = true
	}
	for i := len(staleDirs) - 1; i >= 0; i-- {
		if err := os.Remove(staleDirs[i]); err != nil {
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
	info, err := os.Lstat(outputRoot)
	if os.IsNotExist(err) {
		if len(expected) == 0 {
			return nil
		}
		return fmt.Errorf("generated DSH Preset drift: missing %s", outputRoot)
	}
	if err != nil {
		return fmt.Errorf("stat generated DSH Presets: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("generated DSH Preset drift: %s is not a directory", outputRoot)
	}
	if len(expected) == 0 {
		return fmt.Errorf("generated DSH Preset drift: unexpected %s", outputRoot)
	}

	drift := make(map[string]bool)
	for relative, want := range expected {
		current := filepath.Join(outputRoot, filepath.FromSlash(relative))
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			drift[relative] = true
			continue
		}
		body, err := os.ReadFile(current)
		if err != nil || !bytes.Equal(body, want.body) || !generatedModeMatches(info.Mode(), want.mode) {
			drift[relative] = true
		}
	}
	expectedDirs := generatedPresetDirectories(expected)
	if err := filepath.WalkDir(outputRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == outputRoot {
			return nil
		}
		relative, err := filepath.Rel(outputRoot, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if !expectedDirs[relative] {
				drift[relative] = true
			}
			return nil
		}
		if _, ok := expected[relative]; !ok {
			drift[relative] = true
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk generated DSH Presets: %w", err)
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

func ensureGeneratedDirectory(directory string) (bool, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0o755); err != nil {
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
	if err := os.RemoveAll(directory); err != nil {
		return false, fmt.Errorf("replace generated DSH Preset directory %s: %w", directory, err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return false, fmt.Errorf("create generated DSH Preset directory %s: %w", directory, err)
	}
	return true, nil
}

func ensureGeneratedParent(root, relative string) (bool, error) {
	if relative == "." {
		return false, nil
	}
	changed := false
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		itemChanged, err := ensureGeneratedDirectory(current)
		if err != nil {
			return false, err
		}
		changed = changed || itemChanged
	}
	return changed, nil
}

func writeGeneratedFileIfChanged(destination string, file generatedPresetFile) (bool, error) {
	info, err := os.Lstat(destination)
	if err == nil && info.Mode()&fs.ModeSymlink == 0 && info.Mode().IsRegular() {
		current, err := os.ReadFile(destination)
		if err != nil {
			return false, fmt.Errorf("read generated file %s: %w", destination, err)
		}
		bodyChanged := !bytes.Equal(current, file.body)
		modeChanged := !generatedModeMatches(info.Mode(), file.mode)
		if !bodyChanged && !modeChanged {
			return false, nil
		}
		if bodyChanged {
			if err := os.WriteFile(destination, file.body, file.mode); err != nil {
				return false, fmt.Errorf("write generated file %s: %w", destination, err)
			}
		}
		if err := os.Chmod(destination, file.mode); err != nil {
			return false, fmt.Errorf("set generated file mode %s: %w", destination, err)
		}
		return true, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat generated file %s: %w", destination, err)
	}
	if err == nil {
		if err := os.RemoveAll(destination); err != nil {
			return false, fmt.Errorf("replace generated file %s: %w", destination, err)
		}
	}
	if err := os.WriteFile(destination, file.body, file.mode); err != nil {
		return false, fmt.Errorf("write generated file %s: %w", destination, err)
	}
	if err := os.Chmod(destination, file.mode); err != nil {
		return false, fmt.Errorf("set generated file mode %s: %w", destination, err)
	}
	return true, nil
}
