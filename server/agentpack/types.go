package agentpack

import (
	"encoding/json"
	"strings"
)

const (
	KindPlugin  = "plugin"
	KindManaged = "managed"

	AdapterStandard    = "standard"
	AdapterOpenMontage = "openmontage"
)

type Manifest struct {
	ID string `yaml:"id" json:"id"`
	// Channel is the single output channel owned by this Pack. Plugin-only
	// Packs leave it empty; managed product Packs must declare exactly one.
	Channel string `yaml:"channel,omitempty" json:"channel,omitempty"`
	// PlanTaskKind is the single task kind used when this Pack is selected from
	// the plan composer. It keeps plan selection user-facing while channel and
	// task routing remain server-owned catalog data.
	PlanTaskKind        string                    `yaml:"plan_task_kind,omitempty" json:"plan_task_kind,omitempty"`
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
	DataDeliveries      []DataDeliverySpec        `yaml:"data_delivery" json:"data_delivery,omitempty"`
	DeliveryByTaskType  map[string][]DeliverySpec `yaml:"delivery_by_task_type" json:"delivery_by_task_type,omitempty"`
	SchemaFiles         SchemaRefs                `yaml:"schemas" json:"-"`
	Schemas             *SchemaDocuments          `yaml:"-" json:"schemas,omitempty"`
	UI                  UISpec                    `yaml:"ui" json:"ui,omitempty"`
	Digest              string                    `yaml:"-" json:"digest"`

	dir string
}

func (m Manifest) SupportsTaskKind(taskKind string) bool {
	for _, supported := range m.Bindings.TaskKinds {
		if supported == taskKind {
			return true
		}
	}
	return false
}

func (m Manifest) SupportsPlan() bool {
	if m.Kind != KindManaged || strings.TrimSpace(m.Channel) == "" || strings.TrimSpace(m.PlanTaskKind) == "" {
		return false
	}
	for _, surface := range m.Surfaces {
		if surface == "plan" {
			return m.SupportsTaskKind(m.PlanTaskKind)
		}
	}
	return false
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
	TaskKinds []string `yaml:"task_kinds" json:"task_kinds,omitempty"`
	// Deprecated source fields are accepted only while old plugin manifests are
	// converted; generated catalogs never emit them.
	ProjectPlatforms []string `yaml:"project_platforms,omitempty" json:"-"`
	TaskTypes        []string `yaml:"task_types,omitempty" json:"-"`
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
	Role         string `yaml:"role" json:"role"`
	Path         string `yaml:"path" json:"path"`
	MIMEType     string `yaml:"mime_type" json:"mime_type"`
	InternalOnly bool   `yaml:"internal_only" json:"internal_only,omitempty"`
}

// DataDeliverySpec describes a server-side structured handoff that is not a
// task file artifact. Profile analysis uses this contract to signal that its
// required result is submitted through MCP and persisted by the Server.
type DataDeliverySpec struct {
	Role     string `yaml:"role" json:"role"`
	Type     string `yaml:"type" json:"type"`
	Required bool   `yaml:"required" json:"required"`
}

type SchemaRefs struct {
	TaskInput string `yaml:"task_input" json:"task_input,omitempty"`
	UI        string `yaml:"ui" json:"ui,omitempty"`
	Output    string `yaml:"output" json:"output,omitempty"`
}

type SchemaDocuments struct {
	TaskInput json.RawMessage `json:"task_input,omitempty"`
	UI        json.RawMessage `json:"ui,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

type UISpec struct {
	Renderer string `yaml:"renderer" json:"renderer,omitempty"`
}

type Catalog struct {
	Packs []Manifest `json:"packs"`

	byID       map[string]int
	byAgentID  map[string]int
	byTaskKind map[string]int
	byChannel  map[string]int
}

// DeliveryForTaskType returns the task-type-specific delivery contract when
// one is declared, otherwise the Pack-wide contract.
func (m Manifest) DeliveryForTaskType(taskType string) []DeliverySpec {
	if delivery, ok := m.DeliveryByTaskType[taskType]; ok {
		return delivery
	}
	return m.Delivery
}

func (m Manifest) DeliveryForTaskKind(taskKind string) []DeliverySpec {
	return m.DeliveryForTaskType(taskKind)
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

func (m Manifest) RequiredArtifactsForTaskKind(taskKind string) ([]ArtifactSpec, error) {
	return m.RequiredArtifactsForTaskType(taskKind)
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

// ForAgent resolves the stable Agent Pack identity.
func (c *Catalog) ForAgent(agentID string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byAgentID[strings.TrimSpace(agentID)]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

// ForChannel resolves the Pack that owns channel. A channel is unique across
// channel-bound Packs, so this lookup is deterministic.
func (c *Catalog) ForChannel(channel string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byChannel[strings.TrimSpace(channel)]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

// PlanPacks returns the managed Packs exposed by the plan composer. The
// catalog is already deterministically sorted, so callers can render this
// slice without inventing a second product ordering.
func (c *Catalog) PlanPacks() []Manifest {
	if c == nil {
		return nil
	}
	packs := make([]Manifest, 0)
	for _, pack := range c.Packs {
		if pack.SupportsPlan() {
			packs = append(packs, pack)
		}
	}
	return packs
}

// ForTaskKind resolves a workflow task kind to its owning Pack.
func (c *Catalog) ForTaskKind(taskKind string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	i, ok := c.byTaskKind[strings.TrimSpace(taskKind)]
	if !ok {
		return Manifest{}, false
	}
	return c.Packs[i], true
}

// ForTaskType resolves a canonical task type. Task types are also the task
// kinds for managed product Packs; this method remains named for callers that
// operate on the persisted Task.Type field.
func (c *Catalog) ForTaskType(taskType string) (Manifest, bool) {
	switch strings.TrimSpace(taskType) {
	case "wechat-article":
		return c.ForAgent("wechat-article")
	case "wechat-picture":
		return c.ForAgent("wechat-picture")
	}
	if pack, ok := c.ForTaskKind(taskType); ok {
		return pack, true
	}
	if pack, ok := c.Pack(taskType); ok {
		return pack, true
	}
	if taskType == "viral_analysis" {
		return c.Pack("seednote")
	}
	if taskType == "profile_analysis" {
		return c.Pack("profile-analysis")
	}
	return c.ForChannel(taskType)
}

func (c *Catalog) BillingOperation(taskType string) (string, bool) {
	pack, ok := c.ForTaskType(taskType)
	if !ok {
		return "", false
	}
	if operation := strings.TrimSpace(pack.BillingOperations[taskType]); operation != "" {
		return operation, true
	}
	// Managed packs persist a canonical task type but declare billing by the
	// bound task kind. Resolve that indirection without requiring every pack to
	// duplicate the same operation under both identities.
	if len(pack.Bindings.TaskKinds) == 1 {
		operation := strings.TrimSpace(pack.BillingOperations[pack.Bindings.TaskKinds[0]])
		return operation, operation != ""
	}
	return "", false
}

type GenerateResult struct {
	Changed       bool
	CatalogDigest string
}
