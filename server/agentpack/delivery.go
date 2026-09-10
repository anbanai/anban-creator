package agentpack

import (
	pathpkg "path"
	"strings"
)

// MatchDeliveryPath matches a task file's canonical output path against a Pack
// delivery contract and returns the declared role.
func MatchDeliveryPath(contract []DeliverySpec, rawPath string) (string, bool) {
	spec, ok := MatchDeliverySpec(contract, rawPath)
	if !ok {
		return "", false
	}
	return spec.Role, true
}

// MatchDeliverySpec resolves a canonical relative output path against a Pack
// delivery contract without trusting caller-supplied MIME metadata.
func MatchDeliverySpec(contract []DeliverySpec, rawPath string) (DeliverySpec, bool) {
	path := strings.TrimSpace(strings.ReplaceAll(rawPath, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") {
		return DeliverySpec{}, false
	}
	cleaned := pathpkg.Clean(path)
	if cleaned != path || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return DeliverySpec{}, false
	}
	var matchedSpec DeliverySpec
	matchedCount := 0
	for _, spec := range contract {
		pattern := strings.TrimSpace(strings.ReplaceAll(spec.Path, "\\", "/"))
		if pattern == "" {
			continue
		}
		matched, matchErr := pathpkg.Match(pattern, cleaned)
		if matchErr == nil && matched {
			matchedSpec = spec
			matchedCount++
			if matchedCount > 1 {
				return DeliverySpec{}, false
			}
		}
	}
	return matchedSpec, matchedCount == 1
}
