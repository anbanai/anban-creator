package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

func cloneModelUsageAliases(source map[string]ModelUsageIdentity) map[string]ModelUsageIdentity {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]ModelUsageIdentity, len(source))
	for raw, identity := range source {
		result[raw] = identity
	}
	return result
}

func ValidateModelUsageAliases(aliases map[string]ModelUsageIdentity) error {
	return model.ValidateModelUsageAliases(aliases)
}

func FormatModelUsageAliases(aliases map[string]ModelUsageIdentity) ([]string, error) {
	if err := ValidateModelUsageAliases(aliases); err != nil {
		return nil, err
	}
	rawModels := make([]string, 0, len(aliases))
	for raw := range aliases {
		rawModels = append(rawModels, raw)
	}
	sort.Strings(rawModels)
	result := make([]string, 0, len(rawModels))
	for _, raw := range rawModels {
		identity := aliases[raw]
		result = append(result, raw+"="+identity.Provider+"/"+identity.Model)
	}
	return result, nil
}

func ParseModelUsageAliases(values []string) (map[string]ModelUsageIdentity, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]ModelUsageIdentity, len(values))
	for _, value := range values {
		raw, target, ok := strings.Cut(value, "=")
		provider, canonical, targetOK := strings.Cut(target, "/")
		if !ok || !targetOK {
			return nil, fmt.Errorf("model-usage-alias %q must use raw=provider/model", value)
		}
		if _, duplicate := result[raw]; duplicate {
			return nil, fmt.Errorf("model-usage-alias %q is duplicated", raw)
		}
		result[raw] = ModelUsageIdentity{Provider: provider, Model: canonical}
	}
	if err := ValidateModelUsageAliases(result); err != nil {
		return nil, fmt.Errorf("model-usage-alias: %w", err)
	}
	return result, nil
}
