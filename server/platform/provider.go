package platform

import "context"

// PlatformProfile holds profile data fetched from a platform.
type PlatformProfile struct {
	Name        string                 `json:"name"`
	AvatarURL   string                 `json:"avatar_url"`
	Positioning string                 `json:"positioning"`
	RawData     map[string]interface{} `json:"raw_data"`
}

// PlatformDataProvider defines the interface for fetching data from a platform.
type PlatformDataProvider interface {
	// FetchProfile fetches a user's profile from the given profile URL.
	FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error)
}
