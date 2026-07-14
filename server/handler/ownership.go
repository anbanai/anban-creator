package handler

import "github.com/anbanai/anban-creator/server/storage"

// isUserOwnedStorageKey reports whether cleanKey points at a storage object the
// user is allowed to access.
//
// The shared base allows the user's own uploads under
// uploads/{projects,references,video-references,channels}/{userID}/.
// "uploads/channels/" is the legacy prefix from the channel→project rename and
// is kept so existing user uploads remain accessible.
//
// Callers pass any additional user-scoped prefixes they also permit, e.g.
// "{userID}/designer/" for designer-generated images or "{userID}/" for task
// workspace files. Centralizing the base set + legacy prefix here keeps the
// ownership rules from drifting across the file/task/project handlers.
func isUserOwnedStorageKey(userID, cleanKey string, extraPrefixes ...string) bool {
	return storage.IsUserOwnedKey(userID, cleanKey, extraPrefixes...)
}
