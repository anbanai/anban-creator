package xhs

import (
	"context"
	"fmt"
	"net/url"
)

// GetUserProfile fetches a user's public profile including their feed list.
// No login is required for public profiles.
func (c *Client) GetUserProfile(ctx context.Context, userID, xsecToken string) (*UserProfile, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	var result APIResponse[UserProfile]
	reqBody := UserProfileRequest{
		UserID:    userID,
		XsecToken: xsecToken,
	}
	if err := c.post(ctx, "/api/v1/user/profile", reqBody, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("get user profile failed: %s", result.Message)
	}
	return &result.Data, nil
}

// GetMeProfile fetches the current logged-in user's profile.
// Requires an active login session.
func (c *Client) GetMeProfile(ctx context.Context) (*UserProfile, error) {
	var result APIResponse[UserProfile]
	if err := c.get(ctx, "/api/v1/user/me", &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("get my profile failed: %s", result.Message)
	}
	return &result.Data, nil
}

// ResolveProfileURL extracts user_id and xsec_token from a profile URL.
// Supports URLs like:
//   - https://www.xiaohongshu.com/user/profile/5c9e7b2e0000000001001b8b?xsec_token=xxx
func ResolveProfileURL(rawURL string) (userID, xsecToken string, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse URL: %w", err)
	}

	// Extract user_id from path.
	// Path pattern: /user/profile/{userID}
	for _, seg := range splitPath(parsed.Path) {
		if seg != "user" && seg != "profile" && seg != "" {
			userID = seg
			break
		}
	}
	if userID == "" {
		return "", "", fmt.Errorf("no user_id found in URL: %s", rawURL)
	}

	xsecToken = parsed.Query().Get("xsec_token")
	return userID, xsecToken, nil
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	return parts
}
