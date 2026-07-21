package model

import (
	"fmt"
	"strings"
)

const maxModelUsageAliases = 128

// ModelUsageIdentity is an explicitly configured raw-to-canonical model mapping.
// Unknown raw model names are never guessed.
type ModelUsageIdentity struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ValidateModelUsageAliases validates the shared raw-to-canonical transport contract.
func ValidateModelUsageAliases(aliases map[string]ModelUsageIdentity) error {
	if len(aliases) > maxModelUsageAliases {
		return fmt.Errorf("model usage alias count exceeds limit")
	}
	for raw, identity := range aliases {
		if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\x00\r\n=") {
			return fmt.Errorf("model usage alias %q is invalid", raw)
		}
		if identity.Provider == "" || strings.TrimSpace(identity.Provider) != identity.Provider || strings.ContainsAny(identity.Provider, "\x00\r\n/") {
			return fmt.Errorf("model usage alias %q provider is invalid", raw)
		}
		if identity.Model == "" || strings.TrimSpace(identity.Model) != identity.Model || strings.ContainsAny(identity.Model, "\x00\r\n") {
			return fmt.Errorf("model usage alias %q model is invalid", raw)
		}
	}
	return nil
}
