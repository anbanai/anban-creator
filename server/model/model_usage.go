package model

// ModelUsageIdentity is an explicitly configured raw-to-canonical model mapping.
// Unknown raw model names are never guessed.
type ModelUsageIdentity struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
