package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/anbanai/anban-creator/server/model"
)

// Scores measure suitability on [0,1]; high cost/risk suitability means cheap
// production / low compliance risk. Missing dimensions contribute zero without
// per-candidate renormalization. Configured weights are normalized once across
// all dimensions; only frozen subscription weights determine persisted rank.
func rankTopicDigestSuggestions(snapshot []byte, candidates []topicDigestSuggestion) error {
	weights := model.DefaultTopicRadarWeights()
	var frozen topicFrozenContext
	if err := json.Unmarshal(snapshot, &frozen); err != nil {
		return fmt.Errorf("invalid frozen ranking context: %w", err)
	}
	if len(frozen.Subscription.Weights) > 0 {
		var configured model.TopicScoreWeights
		if err := json.Unmarshal(frozen.Subscription.Weights, &configured); err != nil {
			return fmt.Errorf("invalid frozen weights: %w", err)
		}
		if len(configured) > 0 {
			weights = configured
		}
	}
	weights, err := normalizedTopicScoreWeights(weights)
	if err != nil {
		return fmt.Errorf("invalid frozen weights: %w", err)
	}
	for i := range candidates {
		scores := candidates[i].Scores
		if scores == nil {
			scores = map[string]float64{}
			candidates[i].Scores = scores
		}
		total := 0.0
		for dimension := range model.DefaultTopicRadarWeights() {
			value := scores[dimension]
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
				return fmt.Errorf("suggestion %d score %s must be between zero and one", i, dimension)
			}
			total += value * weights[dimension]
		}
		// Never trust an Agent-provided total or add a bonus from external references.
		scores["score_total"] = total
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Scores["score_total"] > candidates[j].Scores["score_total"] })
	return nil
}

// Normalize the full configured weight vector, never the subset with scores.
func normalizedTopicScoreWeights(weights model.TopicScoreWeights) (model.TopicScoreWeights, error) {
	allowed := model.DefaultTopicRadarWeights()
	sum := 0.0
	for key, value := range weights {
		if _, ok := allowed[key]; !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid weight %q", key)
		}
		sum += value
	}
	if sum <= 0 || math.IsInf(sum, 0) {
		return nil, fmt.Errorf("weights must have a finite positive total")
	}
	normalized := make(model.TopicScoreWeights, len(weights))
	for key, value := range weights {
		normalized[key] = value / sum
	}
	return normalized, nil
}
