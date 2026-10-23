package model

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProjectProfileSetupNormalizesOptionalEaselAnswersAndSources(t *testing.T) {
	setup := NewProjectProfileSetup()
	setup.PlatformAccounts = []ProjectProfileAccount{{Platform: PlatformWechat, ProfileURL: " https://mp.weixin.qq.com/s/account ", AccountName: " 账号 ", FollowerRange: " 1万—5万 ", Source: "[链接分析]"}}
	setup.Answers.Intent.Strengths, setup.Answers.Intent.Resources = " 原创经验 ", " 采访资源 "
	setup.Answers.Content.Favorites, setup.Answers.Content.ReferenceAccounts = " 深度报道 ", " 参考账号 "
	setup.Normalize()
	if err := setup.Validate(); err != nil {
		t.Fatal(err)
	}
	if setup.PlatformAccounts[0].Source != ProfileSourceUserConfirmed || setup.PlatformAccounts[0].AccountName != "账号" || setup.Answers.Intent.Resources != "采访资源" || setup.Answers.Content.Favorites != "深度报道" {
		t.Fatalf("normalized setup = %+v", setup)
	}
	setup.Answers.Content.Favorites = strings.Repeat("x", MaxProfileAnswerBytes+1)
	if err := setup.Validate(); err == nil {
		t.Fatal("oversized optional answer was accepted")
	}
}

func TestProjectProfileSetupAndAgentSelectionPersistWithoutCrossProjectLeakage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}); err != nil {
		t.Fatal(err)
	}
	setup := NewProjectProfileSetup()
	setup.Answers.Intent.Strengths = "独有的项目资料"
	first := Project{ID: uuid.NewString(), UserID: "owner", Platform: PlatformWechat, Name: "first", AgentIDs: []string{AgentIDArticle, AgentIDMontage}, ProfileSetup: &setup}
	second := Project{ID: uuid.NewString(), UserID: "owner", Platform: PlatformWechat, Name: "second", AgentIDs: []string{}}
	for _, project := range []*Project{&first, &second} {
		if err := db.Create(project).Error; err != nil {
			t.Fatal(err)
		}
	}
	var loadedFirst, loadedSecond Project
	if err := db.First(&loadedFirst, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&loadedSecond, "id = ?", second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(loadedFirst.AgentIDs) != 2 || loadedFirst.ProfileSetup == nil || loadedFirst.ProfileSetup.Answers.Intent.Strengths != setup.Answers.Intent.Strengths {
		t.Fatalf("persisted setup was lost: %+v", loadedFirst)
	}
	if loadedSecond.AgentIDs == nil || len(loadedSecond.AgentIDs) != 0 || loadedSecond.ProfileSetup != nil {
		t.Fatalf("empty selection or project isolation changed: %+v", loadedSecond)
	}
}

func TestProjectAgentSelectionRejectsDuplicatesAndUnsupportedPacks(t *testing.T) {
	for _, ids := range [][]string{{AgentIDArticle, AgentIDArticle}, {AgentIDProfileAnalysis}, {"unknown"}} {
		if err := ValidateProjectAgentIDs(ids); err == nil {
			t.Fatalf("accepted unsupported selection %v", ids)
		}
	}
	if err := ValidateProjectAgentIDs([]string{AgentIDArticle, AgentIDWechatPicture, AgentIDSeednote, AgentIDMontage, AgentIDHypit, AgentIDWhiteboard, AgentIDLocalLife, AgentIDVideoEdit}); err != nil {
		t.Fatal(err)
	}
}

func TestVideoEditIsSelectableAndDefaultForItsProjectPlatform(t *testing.T) {
	if got := DefaultProjectAgentIDs(PlatformVideoEdit); len(got) != 1 || got[0] != AgentIDVideoEdit {
		t.Fatalf("video-edit project default Agent IDs = %v", got)
	}
	project := &Project{AgentIDs: []string{AgentIDArticle}}
	if ProjectAllowsAgent(project, AgentIDVideoEdit) {
		t.Fatal("video-edit Agent was allowed when not selected")
	}
}

func TestProfileSetupSupportsPublicAccountsAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformWechat, PlatformSeednote, "douyin", "bilibili", "weibo", "zhihu", "kuaishou", "channels"} {
		t.Run(platform, func(t *testing.T) {
			setup := NewProjectProfileSetup()
			setup.PlatformAccounts = []ProjectProfileAccount{{Platform: platform, ProfileURL: "https://example.com/one"}, {Platform: platform, ProfileURL: "https://example.com/two"}}
			setup.Normalize()
			if err := setup.Validate(); err != nil {
				t.Fatalf("multiple public account sources rejected: %v", err)
			}
		})
	}
}
