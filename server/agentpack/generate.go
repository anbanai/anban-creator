package agentpack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Generate(pluginRoot, outputRoot string) (GenerateResult, error) {
	return generateTo(pluginRoot, filepath.Join(outputRoot, "agents"), filepath.Join(outputRoot, "catalog.generated.json"))
}

func GenerateRepository(pluginRoot, catalogPath string) (GenerateResult, error) {
	return generateTo(pluginRoot, filepath.Join(pluginRoot, "agents"), catalogPath)
}

func generateTo(pluginRoot, agentsDir, catalogPath string) (GenerateResult, error) {
	catalog, err := LoadCatalog(pluginRoot)
	if err != nil {
		return GenerateResult{}, err
	}
	changed := false
	for _, pack := range catalog.Packs {
		for source, destination := range map[string]string{
			pack.Agent.ClaudeSource: filepath.Join(agentsDir, pack.Agent.Name+".md"),
			pack.Agent.CodexSource:  filepath.Join(agentsDir, pack.Agent.Name+".toml"),
		} {
			body, err := os.ReadFile(filepath.Join(pack.dir, source))
			if err != nil {
				return GenerateResult{}, fmt.Errorf("read Agent source: %w", err)
			}
			fileChanged, err := writeFileIfChanged(destination, body)
			if err != nil {
				return GenerateResult{}, err
			}
			changed = changed || fileChanged
		}
	}
	catalogJSON, err := catalog.JSON()
	if err != nil {
		return GenerateResult{}, fmt.Errorf("encode generated Catalog: %w", err)
	}
	catalogJSON = append(catalogJSON, '\n')
	fileChanged, err := writeFileIfChanged(catalogPath, catalogJSON)
	if err != nil {
		return GenerateResult{}, err
	}
	changed = changed || fileChanged
	digest := sha256.Sum256(catalogJSON)
	return GenerateResult{Changed: changed, CatalogDigest: hex.EncodeToString(digest[:])}, nil
}

func CheckRepository(pluginRoot, catalogPath string) error {
	catalog, err := LoadCatalog(pluginRoot)
	if err != nil {
		return err
	}
	expectedAgents := make(map[string][]byte, len(catalog.Packs)*2)
	for _, pack := range catalog.Packs {
		for source, name := range map[string]string{
			pack.Agent.ClaudeSource: pack.Agent.Name + ".md",
			pack.Agent.CodexSource:  pack.Agent.Name + ".toml",
		} {
			body, err := os.ReadFile(filepath.Join(pack.dir, source))
			if err != nil {
				return err
			}
			expectedAgents[name] = body
		}
	}
	var drift []string
	for name, want := range expectedAgents {
		got, err := os.ReadFile(filepath.Join(pluginRoot, "agents", name))
		if err != nil || !bytes.Equal(got, want) {
			drift = append(drift, name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(pluginRoot, "agents"))
	if err != nil {
		return fmt.Errorf("read generated Agents: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".md") && !strings.HasSuffix(entry.Name(), ".toml")) {
			continue
		}
		if _, ok := expectedAgents[entry.Name()]; !ok {
			drift = append(drift, entry.Name())
		}
	}
	if len(drift) > 0 {
		sort.Strings(drift)
		return fmt.Errorf("generated Agent drift: %s", strings.Join(drift, ", "))
	}
	catalogJSON, err := catalog.JSON()
	if err != nil {
		return err
	}
	catalogJSON = append(catalogJSON, '\n')
	current, err := os.ReadFile(catalogPath)
	if err != nil || !bytes.Equal(current, catalogJSON) {
		return fmt.Errorf("generated Catalog drift: %s", catalogPath)
	}
	return nil
}

func writeFileIfChanged(path string, body []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, body) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read generated file %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create generated directory: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, fmt.Errorf("write generated file %s: %w", path, err)
	}
	return true, nil
}
