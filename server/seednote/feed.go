package seednote

import (
	"context"
	"fmt"
)

// GetFeedDetail fetches a single note's full content and comments.
// No login is required for public notes.
func (c *Client) GetFeedDetail(ctx context.Context, req *FeedDetailRequest) (*FeedDetail, error) {
	if req.FeedID == "" {
		return nil, fmt.Errorf("feed_id is required")
	}

	var result APIResponse[FeedDetail]
	if err := c.post(ctx, "/api/v1/feeds/detail", req, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("get feed detail failed: %s", result.Message)
	}
	return &result.Data, nil
}

// ListFeeds returns the homepage feed. Requires an active login session.
func (c *Client) ListFeeds(ctx context.Context) ([]Feed, error) {
	var result APIResponse[[]Feed]
	if err := c.get(ctx, "/api/v1/feeds/list", &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("list feeds failed: %s", result.Message)
	}
	return result.Data, nil
}

// SearchFeeds searches Seednote by keyword. Requires an active login session.
func (c *Client) SearchFeeds(ctx context.Context, req *SearchRequest) ([]Feed, error) {
	if req.Keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}

	var result APIResponse[[]Feed]
	if err := c.post(ctx, "/api/v1/feeds/search", req, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("search feeds failed: %s", result.Message)
	}
	return result.Data, nil
}
