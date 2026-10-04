package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"os"
	pathpkg "path"
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

var supportedChannels = map[string]struct{}{
	"wechat-article":       {},
	"seednote":             {},
	"wechat-picture":       {},
	"profile-analysis":     {},
	"feedback":             {},
	"whiteboard-animation": {},
	"hypit":                {},
	"montage":              {},
}

func isSupportedChannel(channel string) bool {
	_, ok := supportedChannels[channel]
	return ok
}

func LoadCatalog(pluginRoot string) (*Catalog, error) {
	packsRoot := filepath.Join(pluginRoot, "packs")
	entries, err := os.ReadDir(packsRoot)
	if err != nil {
		return nil, fmt.Errorf("read Agent Pack directory: %w", err)
	}

	catalog := &Catalog{
		byID:       make(map[string]int),
		byAgentID:  make(map[string]int),
		byTaskKind: make(map[string]int),
		byChannel:  make(map[string]int),
	}
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
		if previous, exists := catalog.byAgentID[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate Agent ID %q declared by %q and %q", manifest.ID, catalog.Packs[previous].ID, manifest.ID)
		}
		if previous, exists := catalog.byAgentID[manifest.Agent.Name]; exists {
			return nil, fmt.Errorf("duplicate Agent ID %q; agent name %q is declared by both %q and %q", manifest.Agent.Name, manifest.Agent.Name, catalog.Packs[previous].ID, manifest.ID)
		}
		if manifest.Channel != "" {
			if previous, exists := catalog.byChannel[manifest.Channel]; exists {
				return nil, fmt.Errorf("channel %q is bound by both %q and %q", manifest.Channel, catalog.Packs[previous].ID, manifest.ID)
			}
		}
		catalog.Packs = append(catalog.Packs, manifest)
		index := len(catalog.Packs) - 1
		catalog.byAgentID[manifest.ID] = index
		catalog.byAgentID[manifest.Agent.Name] = index
		for _, taskKind := range manifest.Bindings.TaskKinds {
			// Task kinds are scoped to an Agent/channel and may be shared by
			// multiple Packs. Keep the first deterministic default for the
			// compatibility ForTaskKind lookup; new code resolves via ForAgent.
			if previous, exists := catalog.byTaskKind[taskKind]; exists {
				// Product task kinds may intentionally be implemented by multiple
				// channel-specific Agents. Keep rejecting unknown duplicate kinds
				// so malformed third-party Packs fail closed.
				if taskKind != "content_generation" && taskKind != "viral_analysis" {
					return nil, fmt.Errorf("task kind %q is bound by both %q and %q", taskKind, catalog.Packs[previous].ID, manifest.ID)
				}
			} else {
				catalog.byTaskKind[taskKind] = index
			}
		}
		if manifest.Channel != "" {
			catalog.byChannel[manifest.Channel] = index
		}
	}

	sort.Slice(catalog.Packs, func(i, j int) bool {
		if catalog.Packs[i].ID == "ecommerce" {
			return catalog.Packs[j].ID != "ecommerce"
		}
		if catalog.Packs[j].ID == "ecommerce" {
			return false
		}
		return catalog.Packs[i].ID < catalog.Packs[j].ID
	})
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
	if len(manifest.Bindings.TaskKinds) == 0 && len(manifest.Bindings.TaskTypes) > 0 {
		manifest.Bindings.TaskKinds = append([]string(nil), manifest.Bindings.TaskTypes...)
	}
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
	if manifest.Agent.DSHSource != "" {
		path, err := securePackFile(manifest.dir, manifest.Agent.DSHSource)
		if err != nil {
			return fmt.Errorf("invalid DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
		}
		if err := validateDSHAgentSource(path, body); err != nil {
			return fmt.Errorf("DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
		}
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
		if strings.TrimSpace(manifest.Channel) == "" {
			return fmt.Errorf("managed Pack requires exactly one channel")
		}
		if !isSupportedChannel(manifest.Channel) {
			return fmt.Errorf("unsupported channel %q", manifest.Channel)
		}
		if len(manifest.Bindings.TaskKinds) == 0 {
			return fmt.Errorf("managed Pack requires at least one task kind")
		}
		if hasSurface(manifest.Surfaces, "plan") {
			if strings.TrimSpace(manifest.PlanTaskKind) == "" {
				return fmt.Errorf("managed Pack with plan surface requires plan_task_kind")
			}
			if !containsString(manifest.Bindings.TaskKinds, manifest.PlanTaskKind) {
				return fmt.Errorf("plan_task_kind %q must be declared in bindings.task_kinds", manifest.PlanTaskKind)
			}
		} else if strings.TrimSpace(manifest.PlanTaskKind) != "" {
			return fmt.Errorf("plan_task_kind requires plan surface")
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
	} else {
		if len(manifest.Bindings.TaskKinds) > 0 {
			return fmt.Errorf("plugin Pack must not bind task types (task kinds)")
		}
		if strings.TrimSpace(manifest.Channel) != "" {
			return fmt.Errorf("plugin Pack must not bind a channel")
		}
	}
	for _, taskKind := range manifest.Bindings.TaskKinds {
		if strings.TrimSpace(taskKind) == "" {
			return fmt.Errorf("task kind must not be empty")
		}
	}
	taskKinds := make(map[string]bool, len(manifest.Bindings.TaskKinds))
	for _, taskKind := range manifest.Bindings.TaskKinds {
		if taskKinds[taskKind] {
			return fmt.Errorf("duplicate task kind %q", taskKind)
		}
		taskKinds[taskKind] = true
	}
	for taskKind, operation := range manifest.BillingOperations {
		if manifest.Kind == KindManaged && !taskKinds[taskKind] {
			return fmt.Errorf("billing operation references unbound task kind %q", taskKind)
		}
		if strings.TrimSpace(operation) == "" {
			return fmt.Errorf("billing operation for task kind %q must not be empty", taskKind)
		}
	}
	allowedSurfaces := map[string]bool{"plugin": true, "task": true, "plan": true}
	hasProductSurface := false
	for _, surface := range manifest.Surfaces {
		if !allowedSurfaces[surface] {
			return fmt.Errorf("unsupported surface %q", surface)
		}
		if surface == "task" || surface == "plan" {
			hasProductSurface = true
		}
	}
	if manifest.Kind == KindManaged && hasProductSurface {
		for _, taskKind := range manifest.Bindings.TaskKinds {
			if strings.TrimSpace(manifest.BillingOperations[taskKind]) == "" {
				return fmt.Errorf("product surfaces require billing operation for task kind %q", taskKind)
			}
		}
	}
	schemaRefs := []struct {
		ref       string
		extension bool
	}{
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
	if err := validateArtifactContract(manifest.Artifacts); err != nil {
		return err
	}
	if manifest.Kind == KindManaged {
		for taskKind, artifacts := range manifest.ArtifactsByTaskType {
			if !taskKinds[taskKind] {
				return fmt.Errorf("artifact override references unbound task type %q (task kind)", taskKind)
			}
			if len(artifacts) == 0 {
				return fmt.Errorf("artifact override for task type %q must not be empty (task kind)", taskKind)
			}
			if err := validateArtifactContract(artifacts); err != nil {
				return fmt.Errorf("artifact override for task type %q: %w (task kind)", taskKind, err)
			}
		}
	}
	if err := validateDeliveryContract(manifest.Delivery); err != nil {
		return err
	}
	if manifest.Kind == KindManaged {
		for taskKind, deliveries := range manifest.DeliveryByTaskType {
			if !taskKinds[taskKind] {
				return fmt.Errorf("delivery override references unbound task kind %q", taskKind)
			}
			if len(deliveries) == 0 {
				return fmt.Errorf("delivery override for task kind %q must not be empty", taskKind)
			}
			if err := validateDeliveryContract(deliveries); err != nil {
				return fmt.Errorf("delivery override for task kind %q: %w", taskKind, err)
			}
		}
	}
	if manifest.Kind == KindManaged {
		for _, taskKind := range manifest.Bindings.TaskKinds {
			allowsProfileData := taskKind == "profile_analysis" && manifest.hasRequiredDataDelivery("profile_result")
			if len(manifest.DeliveryForTaskType(taskKind)) == 0 && !allowsProfileData {
				return fmt.Errorf("managed Pack task kind %q requires a non-empty delivery contract", taskKind)
			}
			required, err := manifest.RequiredArtifactsForTaskType(taskKind)
			if err != nil {
				return fmt.Errorf("managed Pack task kind %q: %w", taskKind, err)
			}
			if len(required) == 0 && !allowsProfileData {
				return fmt.Errorf("managed Pack task kind %q requires at least one required artifact", taskKind)
			}
		}
	}
	return nil
}

func hasSurface(surfaces []string, wanted string) bool {
	for _, surface := range surfaces {
		if surface == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (m Manifest) hasRequiredDataDelivery(role string) bool {
	for _, delivery := range m.DataDeliveries {
		if delivery.Required && strings.TrimSpace(delivery.Role) == role && strings.TrimSpace(delivery.Type) != "" {
			return true
		}
	}
	return false
}

func validateArtifactContract(artifacts []ArtifactSpec) error {
	paths := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Role) == "" || validateOutputArtifactPath(artifact.Path) != nil {
			return fmt.Errorf("invalid artifact contract for %q", artifact.Path)
		}
		if err := validateContractMIMEType(artifact.MIMEType); err != nil {
			return fmt.Errorf("invalid artifact contract for %q: %w", artifact.Path, err)
		}
		artifactPath := strings.TrimSpace(strings.ReplaceAll(artifact.Path, "\\", "/"))
		if _, exists := paths[artifactPath]; exists {
			return fmt.Errorf("duplicate artifact path %q", artifactPath)
		}
		paths[artifactPath] = struct{}{}
	}
	return nil
}

func validateDeliveryContract(deliveries []DeliverySpec) error {
	seen := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		if err := validateDeliverySpec(delivery); err != nil {
			return err
		}
		current := strings.TrimSpace(strings.ReplaceAll(delivery.Path, "\\", "/"))
		for _, previous := range seen {
			if current == previous {
				return fmt.Errorf("duplicate delivery path %q", current)
			}
			if deliveryPatternsOverlap(previous, current) {
				return fmt.Errorf("overlapping delivery paths %q and %q", previous, current)
			}
		}
		seen = append(seen, current)
	}
	return nil
}

func deliveryPatternsOverlap(left, right string) bool {
	leftGlob := strings.Contains(left, "*")
	rightGlob := strings.Contains(right, "*")
	switch {
	case !leftGlob && !rightGlob:
		return left == right
	case !leftGlob:
		matched, _ := pathpkg.Match(right, left)
		return matched
	case !rightGlob:
		matched, _ := pathpkg.Match(left, right)
		return matched
	}
	leftPrefix, leftSuffix := splitDeliveryPattern(left)
	rightPrefix, rightSuffix := splitDeliveryPattern(right)
	prefixesCompatible := strings.HasPrefix(leftPrefix, rightPrefix) || strings.HasPrefix(rightPrefix, leftPrefix)
	suffixesCompatible := strings.HasSuffix(leftSuffix, rightSuffix) || strings.HasSuffix(rightSuffix, leftSuffix)
	return prefixesCompatible && suffixesCompatible
}

func splitDeliveryPattern(pattern string) (string, string) {
	index := strings.IndexByte(pattern, '*')
	if index < 0 {
		return pattern, ""
	}
	return pattern[:index], pattern[index+1:]
}

func validateDeliverySpec(delivery DeliverySpec) error {
	if strings.TrimSpace(delivery.Role) == "" {
		return fmt.Errorf("invalid delivery contract for %q", delivery.Path)
	}
	if err := validateContractMIMEType(delivery.MIMEType); err != nil {
		return fmt.Errorf("invalid delivery contract for %q: %w", delivery.Path, err)
	}
	if err := validateOutputArtifactPath(delivery.Path); err != nil {
		return fmt.Errorf("invalid delivery contract for %q: delivery path must be under output/", delivery.Path)
	}
	if _, err := pathpkg.Match(delivery.Path, delivery.Path); err != nil {
		return fmt.Errorf("invalid delivery contract for %q: invalid glob: %w", delivery.Path, err)
	}
	pattern := strings.TrimSpace(strings.ReplaceAll(delivery.Path, "\\", "/"))
	if strings.Count(pattern, "*") > 1 || strings.ContainsAny(pattern, "?[") {
		return fmt.Errorf("invalid delivery contract for %q: only one basename wildcard is supported", delivery.Path)
	}
	if wildcard := strings.IndexByte(pattern, '*'); wildcard >= 0 && wildcard < strings.LastIndexByte(pattern, '/') {
		return fmt.Errorf("invalid delivery contract for %q: wildcard must be in the basename", delivery.Path)
	}
	return nil
}

func validateContractMIMEType(value string) error {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid MIME type %q: %w", value, err)
	}
	typeName, subtype, ok := strings.Cut(mediaType, "/")
	if !ok || typeName == "" || subtype == "" {
		return fmt.Errorf("invalid MIME type %q: type and subtype are required", value)
	}
	if strings.Contains(mediaType, "*") {
		return fmt.Errorf("invalid MIME type %q: wildcards are not allowed", value)
	}
	return nil
}

func validateOutputArtifactPath(raw string) error {
	if strings.TrimSpace(raw) == "" || filepath.IsAbs(raw) || strings.Contains(raw, "\\") {
		return fmt.Errorf("required artifact must be under output/")
	}
	for _, component := range strings.Split(raw, "/") {
		if component == ".." {
			return fmt.Errorf("required artifact must be under output/")
		}
	}
	clean := pathpkg.Clean(raw)
	if clean != raw || pathpkg.IsAbs(clean) || clean == "." || clean == "output" || !strings.HasPrefix(clean, "output/") {
		return fmt.Errorf("required artifact must be under output/")
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
	paths := []string{manifest.Agent.ClaudeSource, manifest.Agent.CodexSource, manifest.Agent.DSHSource, manifest.SchemaFiles.TaskInput, manifest.SchemaFiles.UI, manifest.SchemaFiles.Output}
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
	c.byAgentID = make(map[string]int, len(c.Packs)*2)
	c.byTaskKind = make(map[string]int)
	c.byChannel = make(map[string]int)
	for i := range c.Packs {
		c.byID[c.Packs[i].ID] = i
		c.Packs[i].Channel = manifestChannel(c.Packs[i])
		c.byAgentID[c.Packs[i].ID] = i
		c.byAgentID[c.Packs[i].Agent.Name] = i
		for _, taskKind := range c.Packs[i].Bindings.TaskKinds {
			if _, exists := c.byTaskKind[taskKind]; !exists {
				c.byTaskKind[taskKind] = i
			}
		}
		if c.Packs[i].Channel != "" {
			c.byChannel[c.Packs[i].Channel] = i
		}
	}
}

func manifestChannel(pack Manifest) string {
	return pack.Channel
}

func (c *Catalog) JSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}
