package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	kebabIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	semverPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

func LoadCatalog(pluginRoot string) (*Catalog, error) {
	packsRoot := filepath.Join(pluginRoot, "packs")
	entries, err := os.ReadDir(packsRoot)
	if err != nil {
		return nil, fmt.Errorf("read Agent Pack directory: %w", err)
	}

	catalog := &Catalog{byID: make(map[string]int), byTaskType: make(map[string]int), byProjectPlatform: make(map[string]int)}
	byAgentName := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(packsRoot, entry.Name(), "agent-pack.yaml")
		manifest, err := loadManifest(pluginRoot, manifestPath)
		if err != nil {
			return nil, err
		}
		if manifest.ID != entry.Name() {
			return nil, fmt.Errorf("Agent Pack directory %q does not match id %q", entry.Name(), manifest.ID)
		}
		if _, exists := catalog.byID[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate Agent Pack id %q", manifest.ID)
		}
		if previous, exists := byAgentName[manifest.Agent.Name]; exists {
			return nil, fmt.Errorf("agent name %q is declared by both %q and %q", manifest.Agent.Name, previous, manifest.ID)
		}
		for _, taskType := range manifest.Bindings.TaskTypes {
			if previous, exists := catalog.byTaskType[taskType]; exists {
				return nil, fmt.Errorf("task type %q is bound by both %q and %q", taskType, catalog.Packs[previous].ID, manifest.ID)
			}
		}
		for _, platform := range manifest.Bindings.ProjectPlatforms {
			if previous, exists := catalog.byProjectPlatform[platform]; exists {
				return nil, fmt.Errorf("project platform %q is bound by both %q and %q", platform, catalog.Packs[previous].ID, manifest.ID)
			}
		}
		catalog.Packs = append(catalog.Packs, manifest)
		byAgentName[manifest.Agent.Name] = manifest.ID
		index := len(catalog.Packs) - 1
		for _, taskType := range manifest.Bindings.TaskTypes {
			catalog.byTaskType[taskType] = index
		}
		for _, platform := range manifest.Bindings.ProjectPlatforms {
			catalog.byProjectPlatform[platform] = index
		}
	}

	sort.Slice(catalog.Packs, func(i, j int) bool { return catalog.Packs[i].ID < catalog.Packs[j].ID })
	catalog.reindex()
	return catalog, nil
}

func loadManifest(pluginRoot, path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read Agent Pack manifest %s: %w", path, err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode Agent Pack manifest %s: %w", path, err)
	}
	manifest.dir = filepath.Dir(path)
	if err := validateManifest(pluginRoot, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("Agent Pack %q: %w", manifest.ID, err)
	}
	if err := resolveSchemas(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("Agent Pack %q: %w", manifest.ID, err)
	}
	digest, err := digestManifest(pluginRoot, manifest, data)
	if err != nil {
		return Manifest{}, fmt.Errorf("Agent Pack %q digest: %w", manifest.ID, err)
	}
	manifest.Digest = digest
	return manifest, nil
}

func validateManifest(pluginRoot string, manifest *Manifest) error {
	if !kebabIDPattern.MatchString(manifest.ID) {
		return fmt.Errorf("id must use kebab-case")
	}
	if !semverPattern.MatchString(manifest.Version) {
		return fmt.Errorf("version must use MAJOR.MINOR.PATCH")
	}
	if manifest.Kind != KindPlugin && manifest.Kind != KindManaged {
		return fmt.Errorf("kind must be %q or %q", KindPlugin, KindManaged)
	}
	if strings.TrimSpace(manifest.DisplayName) == "" || strings.TrimSpace(manifest.Description) == "" {
		return fmt.Errorf("display_name and description are required")
	}
	if !kebabIDPattern.MatchString(manifest.Agent.Name) {
		return fmt.Errorf("agent.name must use kebab-case")
	}
	if manifest.Agent.MaxTurns <= 0 {
		return fmt.Errorf("agent.max_turns must be positive")
	}
	for _, source := range []string{manifest.Agent.ClaudeSource, manifest.Agent.CodexSource} {
		if _, err := securePackFile(manifest.dir, source); err != nil {
			return fmt.Errorf("invalid Agent source %q: %w", source, err)
		}
	}
	if err := validateNativeAgentSources(manifest); err != nil {
		return err
	}
	for _, skill := range manifest.Agent.Skills {
		if !kebabIDPattern.MatchString(skill) {
			return fmt.Errorf("Skill %q must use kebab-case", skill)
		}
		if _, err := os.Stat(filepath.Join(pluginRoot, "skills", skill, "SKILL.md")); err != nil {
			return fmt.Errorf("missing Skill %q: %w", skill, err)
		}
	}
	if manifest.Kind == KindManaged {
		if len(manifest.Bindings.TaskTypes) == 0 {
			return fmt.Errorf("managed Pack requires at least one task type")
		}
		if strings.TrimSpace(manifest.Runtime.Profile) == "" {
			return fmt.Errorf("managed Pack requires runtime.profile")
		}
		if manifest.Runtime.MaxTurns <= 0 {
			return fmt.Errorf("managed Pack requires positive runtime.max_turns")
		}
		if manifest.Runtime.Adapter != AdapterStandard && manifest.Runtime.Adapter != AdapterOpenMontage {
			return fmt.Errorf("unsupported runtime adapter %q", manifest.Runtime.Adapter)
		}
	} else if len(manifest.Bindings.TaskTypes) > 0 {
		return fmt.Errorf("plugin Pack must not bind task types")
	}
	for _, taskType := range manifest.Bindings.TaskTypes {
		if strings.TrimSpace(taskType) == "" {
			return fmt.Errorf("task type must not be empty")
		}
	}
	taskTypes := make(map[string]bool, len(manifest.Bindings.TaskTypes))
	for _, taskType := range manifest.Bindings.TaskTypes {
		taskTypes[taskType] = true
	}
	for taskType, operation := range manifest.BillingOperations {
		if !taskTypes[taskType] {
			return fmt.Errorf("billing operation references unbound task type %q", taskType)
		}
		if strings.TrimSpace(operation) == "" {
			return fmt.Errorf("billing operation for task type %q must not be empty", taskType)
		}
	}
	allowedSurfaces := map[string]bool{"plugin": true, "project": true, "task": true, "plan": true}
	hasProductSurface := false
	for _, surface := range manifest.Surfaces {
		if !allowedSurfaces[surface] {
			return fmt.Errorf("unsupported surface %q", surface)
		}
		if surface == "project" || surface == "task" || surface == "plan" {
			hasProductSurface = true
		}
	}
	if manifest.Kind == KindManaged && hasProductSurface {
		for _, taskType := range manifest.Bindings.TaskTypes {
			if strings.TrimSpace(manifest.BillingOperations[taskType]) == "" {
				return fmt.Errorf("product surfaces require billing operation for task type %q", taskType)
			}
		}
	}
	schemaRefs := []struct {
		ref       string
		extension bool
	}{
		{manifest.SchemaFiles.ProjectConfig, true},
		{manifest.SchemaFiles.TaskInput, true},
		{manifest.SchemaFiles.UI, false},
		{manifest.SchemaFiles.Output, false},
	}
	for _, item := range schemaRefs {
		if item.ref == "" {
			continue
		}
		path, err := securePackFile(manifest.dir, item.ref)
		if err != nil {
			return fmt.Errorf("invalid Schema %q: %w", item.ref, err)
		}
		var schema any
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Schema %q: %w", item.ref, err)
		}
		if err := json.Unmarshal(body, &schema); err != nil {
			return fmt.Errorf("decode Schema %q: %w", item.ref, err)
		}
		if item.extension {
			allowComplex := strings.HasPrefix(manifest.UI.Renderer, "custom:")
			if err := validateExtensionSchemaDocument(body, item.ref, allowComplex); err != nil {
				return err
			}
		}
	}
	for _, artifact := range manifest.Artifacts {
		clean := filepath.ToSlash(filepath.Clean(artifact.Path))
		if artifact.Role == "" || artifact.MIMEType == "" || !strings.HasPrefix(clean, "output/") || strings.Contains(clean, "../") {
			return fmt.Errorf("invalid artifact contract for %q", artifact.Path)
		}
	}
	return nil
}

func securePackFile(packDir, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path must be relative")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes Pack directory")
	}
	path := filepath.Join(packDir, clean)
	current := packDir
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains symlink component")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("path must be a regular file")
	}
	return path, nil
}

func digestManifest(pluginRoot string, manifest Manifest, manifestData []byte) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write(manifestData)
	paths := []string{manifest.Agent.ClaudeSource, manifest.Agent.CodexSource, manifest.SchemaFiles.ProjectConfig, manifest.SchemaFiles.TaskInput, manifest.SchemaFiles.UI, manifest.SchemaFiles.Output}
	for _, relative := range paths {
		if relative == "" {
			continue
		}
		if err := hashAgentPackFile(hash, filepath.ToSlash(relative), filepath.Join(manifest.dir, relative)); err != nil {
			return "", err
		}
	}
	for _, skill := range manifest.Agent.Skills {
		skillRoot := filepath.Join(pluginRoot, "skills", skill)
		if err := filepath.WalkDir(skillRoot, func(path string, entry fs.DirEntry, walkErr error) error {
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
				return fmt.Errorf("referenced Skill %q contains symlink %q", skill, path)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("referenced Skill %q contains non-regular file %q", skill, path)
			}
			relative, err := filepath.Rel(pluginRoot, path)
			if err != nil {
				return err
			}
			return hashAgentPackFile(hash, filepath.ToSlash(relative), path)
		}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func hashAgentPackFile(hash interface{ Write([]byte) (int, error) }, relative, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, _ = hash.Write([]byte("\x00" + relative + "\x00"))
	_, _ = hash.Write(body)
	return nil
}

func resolveSchemas(manifest *Manifest) error {
	refs := []struct {
		ref    string
		assign func(*SchemaDocuments, json.RawMessage)
	}{
		{manifest.SchemaFiles.ProjectConfig, func(s *SchemaDocuments, raw json.RawMessage) { s.ProjectConfig = raw }},
		{manifest.SchemaFiles.TaskInput, func(s *SchemaDocuments, raw json.RawMessage) { s.TaskInput = raw }},
		{manifest.SchemaFiles.UI, func(s *SchemaDocuments, raw json.RawMessage) { s.UI = raw }},
		{manifest.SchemaFiles.Output, func(s *SchemaDocuments, raw json.RawMessage) { s.Output = raw }},
	}
	for _, item := range refs {
		if item.ref == "" {
			continue
		}
		if manifest.Schemas == nil {
			manifest.Schemas = &SchemaDocuments{}
		}
		path, err := securePackFile(manifest.dir, item.ref)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		item.assign(manifest.Schemas, json.RawMessage(body))
	}
	return nil
}

func (c *Catalog) reindex() {
	c.byID = make(map[string]int, len(c.Packs))
	c.byTaskType = make(map[string]int)
	c.byProjectPlatform = make(map[string]int)
	for i := range c.Packs {
		c.byID[c.Packs[i].ID] = i
		for _, taskType := range c.Packs[i].Bindings.TaskTypes {
			c.byTaskType[taskType] = i
		}
		for _, platform := range c.Packs[i].Bindings.ProjectPlatforms {
			c.byProjectPlatform[platform] = i
		}
	}
}

func (c *Catalog) JSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}
