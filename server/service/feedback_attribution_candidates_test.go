package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestBuildAttributionCandidateJobsCreatesHighAndLowJobsFromMatureAccountBaseline(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 18, 4, 30, 0, 0, time.UTC)
	project := &model.Project{ID: "project-1", UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Timezone: "Asia/Shanghai"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("content-%02d", i)
		published := at.AddDate(0, 0, -10)
		publicationID := "publication-" + id
		if err := db.Create(&model.WechatPublication{ID: publicationID, TaskID: "task-" + id, UserID: project.UserID, ProjectID: project.ID, Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublished, PublishedAt: &published}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AnalyticsContent{ID: id, ProjectID: project.ID, Channel: model.ChannelArticle, Platform: model.PlatformWechat, PublicationID: publicationID, Date: &published}).Error; err != nil {
			t.Fatal(err)
		}
		readers := int64(100)
		engagement := int64(i)
		if err := db.Create(&model.AnalyticsObservation{ID: "obs-" + id, ProjectID: project.ID, ContentID: id, MetricBasis: "cumulative", StatDate: at.AddDate(0, 0, -1).Format("2006-01-02"), ReadUsers: &readers, ShareUsers: &engagement}).Error; err != nil {
			t.Fatal(err)
		}
	}
	scope := feedbackProjectScope{Project: project, Channel: model.ChannelArticle, AccountID: "wechat:wx-1"}
	state := &model.AnalyticsState{ProjectID: project.ID, Revision: 12, Status: "ready"}
	jobs, err := buildAttributionCandidateJobs(context.Background(), repo, scope, at, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %#v, want one high and one low job", jobs)
	}
	if jobs[0].TargetContentID != "content-09" || jobs[0].AttributionDirection != AttributionDirectionHigh || jobs[0].PerformanceMetric != "engagement_rate" || jobs[0].SampleCount != 10 {
		t.Fatalf("high job = %#v", jobs[0])
	}
	if jobs[1].TargetContentID != "content-00" || jobs[1].AttributionDirection != AttributionDirectionLow {
		t.Fatalf("low job = %#v", jobs[1])
	}
	if jobs[0].AnalyticsRevision != 12 || jobs[0].ProfileRevision != projectProfileFor(project).Version || jobs[0].Trigger != FeedbackTriggerAutoAttribution {
		t.Fatalf("frozen job revisions/trigger = %#v", jobs[0])
	}
	if jobs[0].FrozenContextJSON == "" || jobs[1].FrozenContextJSON == "" {
		t.Fatal("attribution jobs must persist frozen analysis context")
	}
}

func TestBuildAttributionCandidateJobsRejectsUnpublishedAndPrematurityObservations(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 18, 4, 30, 0, 0, time.UTC)
	project := &model.Project{ID: "project-1", UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Timezone: "Asia/Shanghai"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("content-%02d", i)
		published := at.AddDate(0, 0, -10)
		pubID := "publication-" + id
		status := model.WechatPublicationStatusPublished
		if i == 0 {
			status = model.WechatPublicationStatusDrafted
		}
		if err := db.Create(&model.WechatPublication{ID: pubID, TaskID: "task-" + id, UserID: project.UserID, ProjectID: project.ID, Source: model.WechatPublicationSourceAnbanAPI, Status: status, PublishedAt: &published}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AnalyticsContent{ID: id, ProjectID: project.ID, Channel: model.ChannelArticle, Platform: model.PlatformWechat, PublicationID: pubID, Date: &published}).Error; err != nil {
			t.Fatal(err)
		}
		readers, engagement := int64(100), int64(i)
		statDate := at.AddDate(0, 0, -1).Format("2006-01-02")
		if i == 1 {
			statDate = published.AddDate(0, 0, 3).Format("2006-01-02")
		}
		if err := db.Create(&model.AnalyticsObservation{ID: "obs-" + id, ProjectID: project.ID, ContentID: id, MetricBasis: "cumulative", StatDate: statDate, ReadUsers: &readers, ShareUsers: &engagement}).Error; err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := buildAttributionCandidateJobs(context.Background(), repo, feedbackProjectScope{Project: project, Channel: model.ChannelArticle, AccountID: "wechat:wx-1"}, at, &model.AnalyticsState{ProjectID: project.ID, Revision: 12, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.TargetContentID == "content-00" || job.TargetContentID == "content-01" {
			t.Fatalf("ineligible content selected: %#v", job)
		}
	}
}

func TestBuildAttributionCandidateJobsSkipsSmallCohort(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 18, 4, 30, 0, 0, time.UTC)
	project := &model.Project{ID: "project-1", UserID: "user-1", Name: "test", Status: model.ProjectStatusActive}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	jobs, err := buildAttributionCandidateJobs(context.Background(), repo, feedbackProjectScope{Project: project, Channel: model.ChannelArticle, AccountID: "wechat:wx-1"}, at, &model.AnalyticsState{ProjectID: project.ID, Revision: 4, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != model.FeedbackJobSkipped || jobs[0].SkipReason != model.FeedbackSkipSampleTooSmall || jobs[0].SampleCount != 0 {
		t.Fatalf("small cohort jobs = %#v", jobs)
	}
}
