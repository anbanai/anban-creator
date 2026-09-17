package agentpack

import "encoding/json"

const (
	KindPlugin  = "plugin"
	KindManaged = "managed"

	AdapterStandard    = "standard"
	AdapterOpenMontage = "openmontage"
)

type Manifest struct {
	ID                  string                    `yaml:"id" json:"id"`
	Version             string                    `yaml:"version" json:"version"`
	Kind                string                    `yaml:"kind" json:"kind"`
	DisplayName         string                    `yaml:"display_name" json:"display_name"`
	Description         string                    `yaml:"description" json:"description"`
	Agent               AgentSpec                 `yaml:"agent" json:"agent"`
	Bindings            Bindings                  `yaml:"bindings" json:"bindings"`
	Runtime             RuntimeSpec               `yaml:"runtime" json:"runtime"`
	Surfaces            []string                  `yaml:"surfaces" json:"surfaces"`
	Features            []string                  `yaml:"features" json:"features,omitempty"`
	BillingOperations   map[string]string         `yaml:"billing_operations" json:"billing_operations,omitempty"`
	Artifacts           []ArtifactSpec            `yaml:"artifacts" json:"artifacts,omitempty"`
	ArtifactsByTaskType map[string][]ArtifactSpec `yaml:"artifacts_by_task_type" json:"artifacts_by_task_type,omitempty"`
	Delivery            []DeliverySpec            `yaml:"delivery" json:"delivery,omitempty"`
	DeliveryByTaskType  map[string][]DeliverySpec `yaml:"delivery_by_task_type" json:"delivery_by_task_type,omitempty"`
	SchemaFiles         SchemaRefs                `yaml:"schemas" json:"-"`
	Schemas             *SchemaDocuments          `yaml:"-" json:"schemas,omitempty"`
	UI                  UISpec                    `yaml:"ui" json:"ui,omitempty"`
	Digest              string                    `yaml:"-" json:"digest"`

	dir string
}

type AgentSpec struct {
	Name         string   `yaml:"name" json:"name"`
	ClaudeSource string   `yaml:"claude_source" json:"-"`
	CodexSource  string   `yaml:"codex_source" json:"-"`
	DSHSource    string   `yaml:"dsh_source" json:"-"`
	Skills       []string `yaml:"skills" json:"skills,omitempty"`
	MaxTurns     int      `yaml:"max_turns" json:"max_turns,omitempty"`
}

type Bindings struct {
	ProjectPlatforms []string `yaml:"project_platforms" json:"project_platforms,omitempty"`
	TaskTypes        []string `yaml:"task_types" json:"task_types,omitempty"`
}

type RuntimeSpec struct {
	Profile  string `yaml:"profile" json:"profile,omitempty"`
	Adapter  string `yaml:"adapter" json:"adapter,omitempty"`
	MaxTurns int    `yaml:"max_turns" json:"max_turns,omitempty"`
}

type ArtifactSpec struct {
	Role     string `yaml:"role" json:"role"`
	Path     string `yaml:"path" json:"path"`
	MIMEType string `yaml:"mime_type" json:"mime_type"`
	Required bool   `yaml:"required" json:"required"`
}

// DeliverySpec declares a file that should be presented and downloadable as
// part of a user-facing task result. Path accepts an exact output path or a
// path.Match-compatible pattern.
type DeliverySpec struct {
	Role     string `yaml:"role" json:"role"`
	Path     string `yaml:"path" json:"path"`
	MIMEType string `yaml:"mime_type" json:"mime_type"`
}

type SchemaRefs struct {
	ProjectConfig string `yaml:"project_config" json:"project_config,omitempty"`
	TaskInput     string `yaml:"task_input" json:"task_input,omitempty"`
	UI            string `yaml:"ui" json:"ui,omitempty"`
	Output        string `yaml:"output" json:"output,omitempty"`
}

type SchemaDocuments struct {
	ProjectConfig json.RawMessage `json:"project_config,omitempty"`
	TaskInput     json.RawMessage `json:"task_input,omitempty"`
	UI            json.RawMessage `json:"ui,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"`
}

type UISpec struct {
	Renderer string `yaml:"renderer" json:"renderer,omitempty"`
}

type Catalog struct {
	Packs []Manifest `json:"packs"`

	byID              map[string]int
	byTaskType        map[string]int
	byProjectPlatform map[string]int
}

// DeliveryForTaskType returns the task-type-specific delivery contract when
// one is declared, otherwise the Pack-wide contract.
func (m Manifest) DeliveryForTaskType(taskType string) []DeliverySpec {
	if delivery, ok := m.DeliveryByTaskType[taskType]; ok {
		return delivery
	}
	return m.Delivery
}

// ArtifactsForTaskType returns a complete task-type override when present.
func (m Manifest) ArtifactsForTaskType(taskType string) []ArtifactSpec {
	if artifacts, ok := m.ArtifactsByTaskType[taskType]; ok {
		return artifacts
	}
	return m.Artifacts
}

// RequiredArtifactsForTaskType derives final delivery requirements only from
// the selected artifact contract. Runtime stages never affect validation.
func (m Manifest) RequiredArtifactsForTaskType(taskType string) ([]ArtifactSpec, error) {
	required := make([]ArtifactSpec, 0)
	for _, artifact := range m.ArtifactsForTaskType(taskType) {
		if artifact.Required {
			required = append(required, artifact)
		}
	}
	return required, nil
}

func (c *Catalog) Pack(id string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byID[id]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

func (c *Catalog) ForTaskType(taskType string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byTaskType[taskType]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

func (c *Catalog) ForProjectPlatform(platform string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byProjectPlatform[platform]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

func (c *Catalog) BillingOperation(taskType string) (string, bool) {
	pack, ok := c.ForTaskType(taskType)
	if !ok {
		return "", false
	}
	operation := pack.BillingOperations[taskType]
	return operation, operation != ""
}

type GenerateResult struct {
	Changed       bool
	CatalogDigest string
}
