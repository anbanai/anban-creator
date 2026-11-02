package service

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestClaimTopicNotificationPreventsConcurrentSenders(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	item := &model.TopicNotificationDelivery{ID: "claim-delivery", DigestID: "claim-digest", ChannelID: "claim-channel", PayloadVersion: "v1", Payload: []byte(`{"suggestions":[]}`), Status: model.TopicNotificationPending}
	if err := repo.TopicNotificationDeliveries().CreateBatch(ctx, []*model.TopicNotificationDelivery{item}); err != nil {
		t.Fatal(err)
	}
	first, err := repo.TopicNotificationDeliveries().Claim(ctx, item.ID, "token-1", time.Now().Add(time.Minute))
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v", first, err)
	}
	second, err := repo.TopicNotificationDeliveries().Claim(ctx, item.ID, "token-2", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("second claim should be rejected")
	}
}
