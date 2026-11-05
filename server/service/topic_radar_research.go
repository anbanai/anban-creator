package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/anbanai/anban-creator/server/model"
)

// Only successful, exact-execution receipts can extend immutable input evidence.
// Decode the capability's typed result, never arbitrary nested provider metadata.
func (s *TopicRadarService) topicResearchSources(ctx context.Context, digest *model.TopicDigest) ([]model.TopicSourceRef, int, error) {
	receipts, err := s.repo.SearchOperations().ListSucceeded(ctx, digest.UserID, digest.ProjectID, *digest.AgentTaskID, *digest.AgentExecutionID)
	if err != nil {
		return nil, 0, err
	}
	refs := []model.TopicSourceRef{}
	for _, receipt := range receipts {
		var items []SearchResultItem
		switch receipt.Operation {
		case "mcp.search_web":
			var response WebSearchResponse
			if err := json.Unmarshal(receipt.ResponseSnapshot, &response); err != nil {
				return nil, 0, fmt.Errorf("invalid research receipt: %w", err)
			}
			items = response.Results
		case "mcp.fetch_web_page":
			var response PageFetchResponse
			if err := json.Unmarshal(receipt.ResponseSnapshot, &response); err != nil {
				return nil, 0, fmt.Errorf("invalid research receipt: %w", err)
			}
			items = []SearchResultItem{{Title: response.Title, URL: response.URL}}
		}
		for _, item := range items {
			u, err := url.Parse(item.URL)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				continue
			}
			refs = append(refs, model.TopicSourceRef{Type: "search", Label: "本次研究", RefID: receipt.ID, Title: item.Title, URL: item.URL, FetchedAt: receipt.UpdatedAt, Freshness: "current_execution"})
		}
	}
	return refs, len(receipts), nil
}
