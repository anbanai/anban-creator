package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

type topicDailySchedule struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

func decodeTopicObject(data []byte, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return fmt.Errorf("expected JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func nextTopicRun(sub *model.TopicSubscription, after time.Time) (time.Time, error) {
	schedule := topicDailySchedule{Hour: 9}
	if err := decodeTopicObject(sub.Schedule, &schedule); err != nil {
		return time.Time{}, fmt.Errorf("%w: schedule: %v", ErrTopicSubscriptionInvalid, err)
	}
	if schedule.Hour < 0 || schedule.Hour > 23 || schedule.Minute < 0 || schedule.Minute > 59 {
		return time.Time{}, fmt.Errorf("%w: schedule hour/minute out of range", ErrTopicSubscriptionInvalid)
	}
	loc, err := time.LoadLocation(sub.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timezone", ErrTopicSubscriptionInvalid)
	}
	local := after.In(loc)
	for day := 0; day < 3; day++ {
		next := time.Date(local.Year(), local.Month(), local.Day()+day, schedule.Hour, schedule.Minute, 0, 0, loc)
		if next.After(after) && next.In(loc).Hour() == schedule.Hour && next.In(loc).Minute() == schedule.Minute {
			return next.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: no next schedule occurrence", ErrTopicSubscriptionInvalid)
}

func validateTopicSubscription(sub *model.TopicSubscription, now time.Time) error {
	if strings.TrimSpace(sub.Name) == "" || len([]rune(sub.Name)) > 120 {
		return fmt.Errorf("%w: name must be 1–120 characters", ErrTopicSubscriptionInvalid)
	}
	next, err := nextTopicRun(sub, now)
	if err != nil {
		return err
	}
	var sources struct {
		Platforms []string `json:"platforms"`
	}
	if err := decodeTopicObject(sub.SourceConfig, &sources); err != nil {
		return fmt.Errorf("%w: sources: %v", ErrTopicSubscriptionInvalid, err)
	}
	allowed := map[string]bool{}
	for _, platform := range TrendPlatforms {
		allowed[platform] = true
	}
	for _, platform := range sources.Platforms {
		if !allowed[platform] {
			return fmt.Errorf("%w: unsupported trend platform %q", ErrTopicSubscriptionInvalid, platform)
		}
	}
	var weights model.TopicScoreWeights
	if err := decodeTopicObject(sub.Weights, &weights); err != nil {
		return fmt.Errorf("%w: weights: %v", ErrTopicSubscriptionInvalid, err)
	}
	if _, err := normalizedTopicScoreWeights(weights); err != nil {
		return fmt.Errorf("%w: %v", ErrTopicSubscriptionInvalid, err)
	}
	var policy struct {
		Enabled         bool   `json:"enabled"`
		AutoGenerate    bool   `json:"auto_generate"`
		TopN            int    `json:"top_n"`
		Platform        string `json:"platform"`
		DurationSeconds int    `json:"duration_seconds"`
		AspectRatio     string `json:"aspect_ratio"`
	}
	if err := decodeTopicObject(sub.ScriptPolicy, &policy); err != nil {
		return fmt.Errorf("%w: script policy: %v", ErrTopicSubscriptionInvalid, err)
	}
	if policy.TopN < 0 || policy.TopN > 20 {
		return fmt.Errorf("%w: script top_n must be 0–20", ErrTopicSubscriptionInvalid)
	}
	if err := validateTopicScriptTarget(policy.Platform, policy.DurationSeconds, policy.AspectRatio); err != nil {
		return err
	}
	if sub.Enabled {
		sub.NextRunAt = &next
	} else {
		sub.NextRunAt = nil
	}
	return nil
}
