package storage

import "testing"

func TestIsUserOwnedKey(t *testing.T) {
	const userID = "user-1"
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "project", key: "uploads/projects/user-1/reference.png", want: true},
		{name: "reference", key: "uploads/references/user-1/reference.png", want: true},
		{name: "video reference", key: "uploads/video-references/user-1/reference.mp4", want: true},
		{name: "legacy channel", key: "uploads/channels/user-1/reference.png", want: true},
		{name: "other user", key: "uploads/channels/user-2/reference.png", want: false},
		{name: "prefix collision", key: "uploads/channels/user-10/reference.png", want: false},
		{name: "bare prefix", key: "uploads/channels/user-1", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsUserOwnedKey(userID, test.key); got != test.want {
				t.Fatalf("IsUserOwnedKey(%q) = %v, want %v", test.key, got, test.want)
			}
		})
	}
}
