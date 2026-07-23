package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/seednote"
)

type seednoteCapabilityReadiness bool

func (r seednoteCapabilityReadiness) Ready() bool { return bool(r) }

type fakeSeednoteCapabilityClient struct {
	loginStatus bool
	qrCode      string
	feeds       []seednote.Feed
	detail      *seednote.FeedDetail
	profile     *seednote.UserProfile

	loginCalls   int
	qrCalls      int
	searchCalls  int
	detailCalls  int
	profileCalls int
	searchReq    *seednote.SearchRequest
	detailReq    *seednote.FeedDetailRequest
	profileUser  string
	profileToken string
}

func (f *fakeSeednoteCapabilityClient) CheckLoginStatus(context.Context) (bool, error) {
	f.loginCalls++
	return f.loginStatus, nil
}

func (f *fakeSeednoteCapabilityClient) GetLoginQRCode(context.Context) (string, error) {
	f.qrCalls++
	return f.qrCode, nil
}

func (f *fakeSeednoteCapabilityClient) SearchFeeds(_ context.Context, req *seednote.SearchRequest) ([]seednote.Feed, error) {
	f.searchCalls++
	f.searchReq = req
	return f.feeds, nil
}

func (f *fakeSeednoteCapabilityClient) GetFeedDetail(_ context.Context, req *seednote.FeedDetailRequest) (*seednote.FeedDetail, error) {
	f.detailCalls++
	f.detailReq = req
	return f.detail, nil
}

func (f *fakeSeednoteCapabilityClient) GetUserProfile(_ context.Context, userID, xsecToken string) (*seednote.UserProfile, error) {
	f.profileCalls++
	f.profileUser = userID
	f.profileToken = xsecToken
	return f.profile, nil
}

func TestSeednoteCapabilityStopsBeforeRemoteCallWhenNotReady(t *testing.T) {
	client := &fakeSeednoteCapabilityClient{}
	svc := NewSeednoteCapabilityService(client, seednoteCapabilityReadiness(false))

	result, err := svc.SearchFeeds(context.Background(), SeednoteSearchFeedsRequest{Keyword: "tea"})
	if err != nil {
		t.Fatalf("SearchFeeds: %v", err)
	}
	if result.Available || result.Message == "" {
		t.Fatalf("result = %#v, want unavailable readiness result", result)
	}
	if client.searchCalls != 0 {
		t.Fatalf("SearchFeeds remote calls = %d, want 0", client.searchCalls)
	}
}

func TestSeednoteCapabilityDelegatesEveryAtomicOperation(t *testing.T) {
	client := &fakeSeednoteCapabilityClient{
		loginStatus: true,
		qrCode:      "png-base64",
		feeds:       []seednote.Feed{{ID: "feed-1"}},
		detail:      &seednote.FeedDetail{Note: seednote.FeedNote{NoteID: "feed-1"}},
		profile:     &seednote.UserProfile{UserBasicInfo: seednote.UserBasicInfo{Nickname: "author"}},
	}
	svc := NewSeednoteCapabilityService(client, seednoteCapabilityReadiness(true))
	ctx := context.Background()

	login, err := svc.LoginStatus(ctx, SeednoteLoginStatusRequest{})
	if err != nil || !login.Available || !login.Value {
		t.Fatalf("LoginStatus = %#v, %v", login, err)
	}
	qr, err := svc.LoginQRCode(ctx, SeednoteLoginQRCodeRequest{})
	if err != nil || qr.Value != "png-base64" {
		t.Fatalf("LoginQRCode = %#v, %v", qr, err)
	}
	feeds, err := svc.SearchFeeds(ctx, SeednoteSearchFeedsRequest{
		Keyword: "tea", SortBy: "最新", NoteType: "图文", PublishTime: "一周内",
	})
	if err != nil || len(feeds.Value) != 1 || feeds.Value[0].ID != "feed-1" {
		t.Fatalf("SearchFeeds = %#v, %v", feeds, err)
	}
	detail, err := svc.FeedDetail(ctx, SeednoteFeedDetailRequest{
		FeedID: "feed-1", XsecToken: "token", LoadAllComments: true,
	})
	if err != nil || detail.Value.Note.NoteID != "feed-1" {
		t.Fatalf("FeedDetail = %#v, %v", detail, err)
	}
	profile, err := svc.UserProfile(ctx, SeednoteUserProfileRequest{UserID: "user-1", XsecToken: "token"})
	if err != nil || profile.Value.UserBasicInfo.Nickname != "author" {
		t.Fatalf("UserProfile = %#v, %v", profile, err)
	}

	if client.loginCalls != 1 || client.qrCalls != 1 || client.searchCalls != 1 || client.detailCalls != 1 || client.profileCalls != 1 {
		t.Fatalf("remote calls = login:%d qr:%d search:%d detail:%d profile:%d", client.loginCalls, client.qrCalls, client.searchCalls, client.detailCalls, client.profileCalls)
	}
	if client.searchReq.Keyword != "tea" || client.searchReq.Filters.SortBy != "最新" ||
		client.searchReq.Filters.NoteType != "图文" || client.searchReq.Filters.PublishTime != "一周内" {
		t.Fatalf("search request = %#v", client.searchReq)
	}
	if client.detailReq.FeedID != "feed-1" || client.detailReq.XsecToken != "token" || !client.detailReq.LoadAllComments {
		t.Fatalf("detail request = %#v", client.detailReq)
	}
	if client.profileUser != "user-1" || client.profileToken != "token" {
		t.Fatalf("profile request = %q, %q", client.profileUser, client.profileToken)
	}
}

func TestSeednoteCapabilityReportsMissingClient(t *testing.T) {
	svc := NewSeednoteCapabilityService(nil, nil)
	result, err := svc.LoginStatus(context.Background(), SeednoteLoginStatusRequest{})
	if err != nil {
		t.Fatalf("LoginStatus: %v", err)
	}
	if result.Available || result.Message == "" {
		t.Fatalf("result = %#v, want unavailable missing-client result", result)
	}
}
