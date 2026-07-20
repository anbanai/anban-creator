package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
)

type failingVideoCostRepository struct {
	repository.BillingCostRepository
}

func (r failingVideoCostRepository) AppendEvent(context.Context, *model.BillingProviderCostEvent) (*model.BillingProviderCostEvent, error) {
	return nil, errors.New("provider cost database unavailable")
}

func TestRecordVideoGenerationProviderCostCreatesTypedUnreconciledEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		references string
		duration   int64
		resolution string
		reason     model.BillingExecutionCostReasonCode
		video      bool
		audio      bool
	}{
		{name: "video input", references: `[{"type":"video_url","url":"https://example.com/in.mp4"}]`, duration: 5, resolution: "720p", reason: model.BillingExecutionCostReasonMissingProviderUsage, video: true},
		{name: "audio input", references: `[{"type":"audio_url","url":"https://example.com/in.mp3"}]`, duration: 5, resolution: "720p", reason: model.BillingExecutionCostReasonMissingProviderUsage, audio: true},
		{name: "missing output metadata", references: `[]`, duration: 0, resolution: "", reason: model.BillingExecutionCostReasonMissingOutputMetadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldSvcs := svcs
			t.Cleanup(func() { svcs = oldSvcs })
			db := repositoryTestDB(t)
			bundle, err := serverbilling.LoadBundle("../billing")
			if err != nil {
				t.Fatal(err)
			}
			svcs = &Services{ProviderCostSvc: service.NewProviderCostService(repository.NewBillingCostRepository(db), bundle)}
			gen := &model.VideoGeneration{ID: uuid.NewString(), TaskID: uuid.NewString(), ArkTaskID: "ark-" + tc.name, References: datatypes.JSON(tc.references)}
			recordVideoGenerationProviderCost(context.Background(), gen, &service.VideoGenerationTaskResult{
				VideoTaskID: gen.ArkTaskID, Status: "succeeded", Model: "doubao-seedance-2-0-260128",
				Duration: tc.duration, Resolution: tc.resolution,
			})
			var event model.BillingProviderCostEvent
			if err := db.First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.Status != model.BillingProviderCostStatusUnreconciled {
				t.Fatalf("event status = %s", event.Status)
			}
			var evidence struct {
				ReasonCode      model.BillingExecutionCostReasonCode `json:"reason_code"`
				DurationSeconds int64                                `json:"duration_seconds"`
				Resolution      string                               `json:"resolution"`
				HasVideoInput   bool                                 `json:"has_video_input"`
				HasAudioInput   bool                                 `json:"has_audio_input"`
			}
			if err := json.Unmarshal(event.UsageEvidence, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.ReasonCode != tc.reason || evidence.DurationSeconds != tc.duration || evidence.Resolution != tc.resolution || evidence.HasVideoInput != tc.video || evidence.HasAudioInput != tc.audio {
				t.Fatalf("evidence = %#v", evidence)
			}
		})
	}
}

func TestQueryVideoGenerationJobRecordsSegmentCostAndCostFailureIsNonfatal(t *testing.T) {
	for _, failCost := range []bool{false, true} {
		t.Run(map[bool]string{false: "records segment", true: "cost failure nonfatal"}[failCost], func(t *testing.T) {
			oldSvcs := svcs
			t.Cleanup(func() { svcs = oldSvcs })
			ctx := context.Background()
			db := repositoryTestDB(t)
			repo := repository.New(db)
			logger := zerolog.New(io.Discard)
			gen := &model.VideoGeneration{UserID: uuid.NewString(), ProjectID: uuid.NewString(), TaskID: uuid.NewString(), Status: "submitted", References: datatypes.JSON(`[]`)}
			if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
				t.Fatal(err)
			}
			segment := &model.VideoGenerationSegment{VideoGenerationID: gen.ID, TaskID: gen.TaskID, Index: 0, ArkTaskID: "cgt-video-1", Status: "submitted", Duration: 5}
			if err := repo.VideoGenerations().CreateSegment(ctx, segment); err != nil {
				t.Fatal(err)
			}
			bundle, err := serverbilling.LoadBundle("../billing")
			if err != nil {
				t.Fatal(err)
			}
			baseCostRepo := repository.NewBillingCostRepository(db)
			var costRepo repository.BillingCostRepository = baseCostRepo
			if failCost {
				costRepo = failingVideoCostRepository{BillingCostRepository: baseCostRepo}
			}
			ark := newArkTaskServer(t)
			defer ark.Close()
			svcs = &Services{
				TaskSvc:         service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil),
				VideoSvc:        service.NewVideoService(&config.VideoAPIConfig{Key: "test", BaseURL: ark.URL, Timeout: time.Second}),
				ProviderCostSvc: service.NewProviderCostService(costRepo, bundle),
			}
			args, _ := json.Marshal(map[string]any{"video_generation_id": gen.ID})
			result, callErr := queryVideoGenerationJobHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
			if callErr != nil || result == nil || result.IsError || !strings.Contains(callToolText(result), `"status":"succeeded"`) {
				t.Fatalf("query result=%#v err=%v text=%s", result, callErr, callToolText(result))
			}
			updated, err := repo.VideoGenerations().FindSegmentByArkTaskID(ctx, "cgt-video-1")
			if err != nil || updated.Status != "succeeded" {
				t.Fatalf("segment=%#v err=%v", updated, err)
			}
			var count int64
			if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if failCost {
				want = 0
			}
			if count != want {
				t.Fatalf("provider cost count = %d, want %d", count, want)
			}
		})
	}
}
