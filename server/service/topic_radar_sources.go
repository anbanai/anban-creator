package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

type topicFrozenContext struct {
	SchemaVersion string                           `json:"schema_version"`
	CapturedAt    time.Time                        `json:"captured_at"`
	Subscription  model.TopicSubscription          `json:"subscription"`
	Project       map[string]any                   `json:"project"`
	SourceRefs    []model.TopicSourceRef           `json:"source_refs"`
	Performance   []repository.AnalyticsContentRow `json:"performance"`
	MissingFields []string                         `json:"missing_fields"`
}

// collectTopicInputs reads persisted Server evidence, never calls providers or
// runs Agent research. The resulting snapshot is immutable for every retry.
func (s *TopicRadarService) collectTopicInputs(ctx context.Context, sub *model.TopicSubscription, project *model.Project) (*topicFrozenContext, error) {
	now := s.now().UTC()
	profile := project.Profile.Data()
	frozen := &topicFrozenContext{SchemaVersion: "1.0", CapturedAt: now, Subscription: *sub,
		Project:    map[string]any{"id": project.ID, "platform": project.Platform, "name": project.Name, "instructions": project.Instructions, "keywords": project.Keywords, "profile": profile},
		SourceRefs: []model.TopicSourceRef{}, Performance: []repository.AnalyticsContentRow{}, MissingFields: []string{}}
	var config struct {
		Platforms []string `json:"platforms"`
	}
	if err := json.Unmarshal(sub.SourceConfig, &config); err != nil {
		return nil, fmt.Errorf("decode topic sources: %w", err)
	}
	if len(config.Platforms) == 0 {
		config.Platforms = append([]string(nil), TrendPlatforms...)
	}
	sort.Strings(config.Platforms)
	seen := map[string]bool{}
	add := func(ref model.TopicSourceRef) {
		key := ref.Type + ":" + ref.Platform + ":" + ref.RefID + ":" + ref.URL
		if ref.Resolvable() && !seen[key] {
			seen[key] = true
			frozen.SourceRefs = append(frozen.SourceRefs, ref)
		}
	}
	for _, platform := range config.Platforms {
		snapshot, err := s.repo.TrendSnapshots().FindByPlatform(ctx, platform)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			frozen.MissingFields = append(frozen.MissingFields, "trends."+platform)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load topic trends: %w", err)
		}
		var items []TrendItem
		if err := json.Unmarshal(snapshot.Items, &items); err != nil {
			return nil, fmt.Errorf("decode topic trends: %w", err)
		}
		freshness := "fresh"
		if now.Sub(snapshot.FetchedAt) > 24*time.Hour {
			freshness = "stale"
		}
		if len(items) == 0 {
			frozen.MissingFields = append(frozen.MissingFields, "trends."+platform)
		}
		for i, item := range items {
			if i >= 50 {
				break
			}
			if strings.TrimSpace(item.Title) == "" {
				continue
			}
			hot, _ := strconv.ParseFloat(strings.ReplaceAll(item.Hot, ",", ""), 64)
			add(model.TopicSourceRef{Type: "trend", Label: trendLabels[platform], Platform: platform, RefID: fmt.Sprintf("%d:%d", snapshot.ID, i), Title: item.Title, URL: item.URL, Rank: item.Rank, Hot: hot, FetchedAt: snapshot.FetchedAt, Freshness: freshness})
		}
	}
	topics, err := s.repo.TopicPools().ListHistoryByProject(ctx, sub.UserID, project.ID, 100)
	if err != nil {
		return nil, fmt.Errorf("load topic history: %w", err)
	}
	for _, topic := range topics {
		if topic.UserID == sub.UserID {
			add(model.TopicSourceRef{Type: "topic_pool", Label: "历史选题", Platform: project.Platform, RefID: strconv.FormatUint(uint64(topic.ID), 10), Title: topic.Topic, FetchedAt: topic.CreatedAt, Freshness: "historical"})
		}
	}
	if len(topics) == 0 {
		frozen.MissingFields = append(frozen.MissingFields, "historical_topics")
	}
	receipts, err := s.repo.SearchOperations().ListRecentByProject(ctx, sub.UserID, project.ID, 50)
	if err != nil {
		return nil, fmt.Errorf("load topic search history: %w", err)
	}
	searchCount := 0
	for _, receipt := range receipts {
		var root any
		if err := json.Unmarshal(receipt.ResponseSnapshot, &root); err != nil {
			return nil, fmt.Errorf("decode search receipt %s: %w", receipt.ID, err)
		}
		var visit func(any)
		visit = func(value any) {
			if searchCount >= 100 {
				return
			}
			switch item := value.(type) {
			case map[string]any:
				raw, _ := item["url"].(string)
				title, _ := item["title"].(string)
				parsed, err := url.Parse(raw)
				if err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
					add(model.TopicSourceRef{Type: "search", Label: "历史搜索", RefID: receipt.ID, Title: title, URL: raw, FetchedAt: receipt.CreatedAt, Freshness: "historical"})
					searchCount++
				}
				keys := make([]string, 0, len(item))
				for key := range item {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					visit(item[key])
				}
			case []any:
				for _, child := range item {
					visit(child)
				}
			}
		}
		visit(root)
	}
	if searchCount == 0 {
		frozen.MissingFields = append(frozen.MissingFields, "search_history")
	}
	if profile.Version > 0 {
		add(model.TopicSourceRef{Type: "profile", Label: "项目 Profile", Platform: project.Platform, RefID: fmt.Sprintf("%s:%d", project.ID, profile.Version), Title: project.Name, Freshness: "current"})
	} else {
		frozen.MissingFields = append(frozen.MissingFields, "profile")
	}
	state, err := s.repo.Analytics().State(ctx, project.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load topic analytics state: %w", err)
	}
	if err == nil && state != nil {
		rows, _, err := s.repo.Analytics().Contents(ctx, project.ID, state.ActiveGeneration, "lifetime", "", "", repository.AnalyticsContentFilter{Limit: 50})
		if err != nil {
			return nil, fmt.Errorf("load topic analytics: %w", err)
		}
		frozen.Performance = rows
		for _, row := range rows {
			add(model.TopicSourceRef{Type: "performance", Label: "历史表现", Platform: row.Channel, RefID: row.ID, Title: row.Title, URL: row.URL, Freshness: "historical"})
		}
	}
	if len(frozen.Performance) == 0 {
		frozen.MissingFields = append(frozen.MissingFields, "historical_performance")
	}
	return frozen, nil
}
