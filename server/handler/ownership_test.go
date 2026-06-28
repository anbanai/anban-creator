package handler

import "testing"

// TestIsUserOwnedStorageKey pins the allowed-prefix set for the centralized
// ownership check. The whole point of consolidating the file/task/project
// handlers into one helper is that this set must not drift — any future change
// that widens or narrows access is a privilege-boundary regression, and this
// table is the backstop that catches it.
func TestIsUserOwnedStorageKey(t *testing.T) {
	const uid = "user-1"

	tests := []struct {
		name   string
		key    string
		extras []string
		want   bool
	}{
		// Shared base prefixes: uploads/{projects,references,channels}/{userID}/.
		// "channels" is the legacy prefix retained after the channel→project rename.
		{name: "projects upload owned", key: "uploads/projects/" + uid + "/ref.png", want: true},
		{name: "references upload owned", key: "uploads/references/" + uid + "/source.png", want: true},
		{name: "legacy channels upload owned", key: "uploads/channels/" + uid + "/legacy.png", want: true},

		// Caller-specific extra prefixes.
		{name: "designer path via extra", key: uid + "/designer/gen-1/0.png", extras: []string{uid + "/designer/"}, want: true},
		{name: "bare user workspace via extra", key: uid + "/workspace/note.md", extras: []string{uid + "/"}, want: true},

		// Negatives: another user's objects must never match.
		{name: "other user projects", key: "uploads/projects/user-2/ref.png", want: false},
		{name: "other user references", key: "uploads/references/user-2/source.png", want: false},
		{name: "other user legacy channels", key: "uploads/channels/user-2/legacy.png", want: false},
		{name: "other user designer not in caller extras", key: "user-2/designer/gen-2/0.png", extras: []string{uid + "/designer/"}, want: false},

		// The trailing slash on every prefix is load-bearing: it stops a short
		// userID from prefix-matching a longer one (no IDOR via "user-1" vs
		// "user-10"), and requires an actual subpath rather than the bare prefix.
		{name: "no collision with user-10 projects", key: "uploads/projects/user-10/ref.png", want: false},
		{name: "owned prefix needs trailing-slash segment", key: "uploads/projects/" + uid, want: false},

		// Unrelated / degenerate keys.
		{name: "bare uploads root", key: "uploads/foo", want: false},
		{name: "uploads projects without user segment", key: "uploads/projects/ref.png", want: false},
		{name: "empty key", key: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUserOwnedStorageKey(uid, tt.key, tt.extras...)
			if got != tt.want {
				t.Fatalf("isUserOwnedStorageKey(%q, extras=%v) = %v, want %v", tt.key, tt.extras, got, tt.want)
			}
		})
	}
}
