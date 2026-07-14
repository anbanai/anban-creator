package storage

import "strings"

// IsUserOwnedKey reports whether key belongs to a user-scoped storage prefix.
// Callers must clean and validate the key before invoking this ownership check.
func IsUserOwnedKey(userID, key string, extraPrefixes ...string) bool {
	prefixes := append([]string{
		"uploads/projects/" + userID + "/",
		"uploads/references/" + userID + "/",
		"uploads/video-references/" + userID + "/",
		"uploads/channels/" + userID + "/",
	}, extraPrefixes...)
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
