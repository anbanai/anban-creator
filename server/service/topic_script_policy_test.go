package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

func TestTopicScriptPolicyRejectsUnexecutableTargets(t *testing.T) {
	for _, raw := range []string{`{"enabled":true,"platform":"unsupported"}`, `{"enabled":true,"duration_seconds":601}`, `{"enabled":true,"duration_seconds":-1}`, `{"enabled":true,"aspect_ratio":"3:4"}`} {
		sub := &model.TopicSubscription{Name: "daily", Timezone: "UTC", Schedule: datatypes.JSON(`{"hour":9}`), SourceConfig: datatypes.JSON(`{}`), Weights: datatypes.JSON(`{"traffic_potential":1}`), ScriptPolicy: datatypes.JSON(raw)}
		if err := validateTopicSubscription(sub, time.Now()); !errors.Is(err, ErrTopicSubscriptionInvalid) {
			t.Errorf("policy %s: %v", raw, err)
		}
	}
}

func TestTopicScriptRequestValidationUsesPublicInputError(t *testing.T) {
	svc := NewTopicRadarService(nil, nil, nil)
	for _, p := range []RequestTopicScriptParams{
		{}, {UserID: "u", SuggestionID: "s", TargetPlatform: "unknown"},
		{UserID: "u", SuggestionID: "s", TargetPlatform: "douyin", DurationSeconds: -1},
		{UserID: "u", SuggestionID: "s", TargetPlatform: "douyin", DurationSeconds: 601},
		{UserID: "u", SuggestionID: "s", TargetPlatform: "douyin", AspectRatio: "3:4"},
	} {
		// Invalid targets must be rejected before any database or enqueue work.
		if _, err := svc.RequestTopicScript(context.Background(), p); !errors.Is(err, ErrTopicSubscriptionInvalid) {
			t.Fatalf("request %+v: %v", p, err)
		}
	}
}
