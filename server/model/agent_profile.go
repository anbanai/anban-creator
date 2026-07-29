package model

// AgentProfileSnapshot is the non-sensitive model identity frozen on a task.
// Credentials and provider endpoints are intentionally excluded from the
// persisted contract.
type AgentProfileSnapshot struct {
	ProfileID        string `json:"profile_id"`
	Provider         string `json:"provider"`
	ModelID          string `json:"model_id"`
	Protocol         string `json:"protocol"`
	ContextWindow    int    `json:"context_window,omitempty"`
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
	ThinkingRequired bool   `json:"thinking_required,omitempty"`
	DisplayName      string `json:"display_name"`
	// These fields are useful while constructing a runtime environment but are
	// never populated in a persisted snapshot or serialized to API responses.
	BaseURL   string `json:"-"`
	AuthToken string `json:"-"`
}
