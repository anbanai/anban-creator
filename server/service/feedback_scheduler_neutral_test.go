package service

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

type neutralFeedbackQueue struct{}

func (neutralFeedbackQueue) EnqueueUnique(string, []byte, string) (bool, error) { return true, nil }

func TestFeedbackSchedulerNeutralChannels(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "scheduled", true: "manual"}[manual], func(t *testing.T) {
			repo, db := openFeedbackJobTestRepo(t)
			if err := db.AutoMigrate(&model.Project{}, &model.ProjectChannelConfig{}, &model.Task{}, &model.AnalyticsState{}); err != nil {
				t.Fatal(err)
			}
			p := &model.Project{ID: "neutral", UserID: "owner", Name: "Shared", Status: model.ProjectStatusActive}
			if err := db.Create(p).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.AnalyticsState{ProjectID: p.ID, Revision: 3, Status: "ready", ActiveGeneration: 1}).Error; err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
			date := at.Add(-24 * time.Hour)
			metric := int64(10)
			for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture, model.ChannelSeednote} {
				platform := model.PlatformForChannel(channel)
				if err := db.Create(&model.AnalyticsContent{ID: channel, ProjectID: p.ID, Channel: channel, Platform: platform, Date: &date}).Error; err != nil {
					t.Fatal(err)
				}
				metrics := model.AnalyticsMetrics{ReadUsers: &metric}
				if channel == model.ChannelSeednote {
					metrics = model.AnalyticsMetrics{ViewCount: &metric}
				}
				if err := db.Create(&model.AnalyticsObservation{ID: channel, ProjectID: p.ID, ContentID: channel, MetricBasis: "cumulative", StatDate: "2026-09-07", AnalyticsMetrics: metrics}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.ProjectChannelConfig{ID: channel, ProjectID: p.ID, Channel: channel, Config: datatypes.NewJSONType(map[string]any{"wechat_app_id": "wx-" + channel})}).Error; err != nil {
					t.Fatal(err)
				}
			}
			s := NewFeedbackScheduler(repo, neutralFeedbackQueue{})
			s.maxPerProject = 3
			var result FeedbackScanResult
			var err error
			if manual {
				result, err = s.RunProjectCadence(context.Background(), p.ID, FeedbackCadenceDaily, at)
			} else {
				result, err = s.RunCadence(context.Background(), FeedbackCadenceDaily, at)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Enqueued != 3 {
				t.Fatalf("enqueued = %d, want 3 independent channels", result.Enqueued)
			}
			var jobs []model.FeedbackJob
			if err := db.Find(&jobs).Error; err != nil {
				t.Fatal(err)
			}
			for _, job := range jobs {
				if job.SampleCount != 1 {
					t.Fatalf("%s mixed channel sample: %d", job.Platform, job.SampleCount)
				}
				expected := "wechat:wx-" + job.Platform
				if job.Platform == model.ChannelSeednote {
					expected = "seednote:project:" + p.ID
				}
				if job.AccountID != expected {
					t.Fatalf("%s account = %q want %q", job.Platform, job.AccountID, expected)
				}
				summary, _, err := loadFeedbackMetricSummary(context.Background(), repo, &job)
				if err != nil {
					t.Fatal(err)
				}
				if summary.SampleCount != 1 {
					t.Fatalf("%s execution mixed channel sample: %d", job.Platform, summary.SampleCount)
				}
			}
			var stored model.Project
			if err := db.First(&stored, "id = ?", p.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Platform != "" {
				t.Fatalf("scheduler mutated project identity: %q", stored.Platform)
			}
		})
	}
}
