package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/seednote"
)

type seednoteCapabilityClient interface {
	CheckLoginStatus(context.Context) (bool, error)
	GetLoginQRCode(context.Context) (string, error)
	SearchFeeds(context.Context, *seednote.SearchRequest) ([]seednote.Feed, error)
	GetFeedDetail(context.Context, *seednote.FeedDetailRequest) (*seednote.FeedDetail, error)
	GetUserProfile(context.Context, string, string) (*seednote.UserProfile, error)
}

type SeednoteCapabilityResult[T any] struct {
	Available bool
	Message   string
	Value     T
}

type SeednoteLoginStatusRequest struct{}
type SeednoteLoginQRCodeRequest struct{}

type SeednoteSearchFeedsRequest struct {
	Keyword     string
	SortBy      string
	NoteType    string
	PublishTime string
}

type SeednoteFeedDetailRequest struct {
	FeedID          string
	XsecToken       string
	LoadAllComments bool
}

type SeednoteUserProfileRequest struct {
	UserID    string
	XsecToken string
}

type SeednoteCapabilityService struct {
	client    seednoteCapabilityClient
	readiness Readiness
}

func NewSeednoteCapabilityService(client seednoteCapabilityClient, readiness Readiness) *SeednoteCapabilityService {
	return &SeednoteCapabilityService{client: client, readiness: readiness}
}

func (s *SeednoteCapabilityService) LoginStatus(ctx context.Context, _ SeednoteLoginStatusRequest) (*SeednoteCapabilityResult[bool], error) {
	if available, message := s.availability(); !available {
		return &SeednoteCapabilityResult[bool]{Message: message}, nil
	}
	value, err := s.client.CheckLoginStatus(ctx)
	return &SeednoteCapabilityResult[bool]{Available: true, Value: value}, err
}

func (s *SeednoteCapabilityService) LoginQRCode(ctx context.Context, _ SeednoteLoginQRCodeRequest) (*SeednoteCapabilityResult[string], error) {
	if available, message := s.availability(); !available {
		return &SeednoteCapabilityResult[string]{Message: message}, nil
	}
	value, err := s.client.GetLoginQRCode(ctx)
	return &SeednoteCapabilityResult[string]{Available: true, Value: value}, err
}

func (s *SeednoteCapabilityService) SearchFeeds(ctx context.Context, req SeednoteSearchFeedsRequest) (*SeednoteCapabilityResult[[]seednote.Feed], error) {
	if available, message := s.availability(); !available {
		return &SeednoteCapabilityResult[[]seednote.Feed]{Message: message}, nil
	}
	if req.Keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}
	value, err := s.client.SearchFeeds(ctx, &seednote.SearchRequest{
		Keyword: req.Keyword,
		Filters: seednote.SearchFilters{
			SortBy: req.SortBy, NoteType: req.NoteType, PublishTime: req.PublishTime,
		},
	})
	return &SeednoteCapabilityResult[[]seednote.Feed]{Available: true, Value: value}, err
}

func (s *SeednoteCapabilityService) FeedDetail(ctx context.Context, req SeednoteFeedDetailRequest) (*SeednoteCapabilityResult[*seednote.FeedDetail], error) {
	if available, message := s.availability(); !available {
		return &SeednoteCapabilityResult[*seednote.FeedDetail]{Message: message}, nil
	}
	if req.FeedID == "" {
		return nil, fmt.Errorf("feed_id is required")
	}
	value, err := s.client.GetFeedDetail(ctx, &seednote.FeedDetailRequest{
		FeedID: req.FeedID, XsecToken: req.XsecToken, LoadAllComments: req.LoadAllComments,
	})
	return &SeednoteCapabilityResult[*seednote.FeedDetail]{Available: true, Value: value}, err
}

func (s *SeednoteCapabilityService) UserProfile(ctx context.Context, req SeednoteUserProfileRequest) (*SeednoteCapabilityResult[*seednote.UserProfile], error) {
	if available, message := s.availability(); !available {
		return &SeednoteCapabilityResult[*seednote.UserProfile]{Message: message}, nil
	}
	if req.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	value, err := s.client.GetUserProfile(ctx, req.UserID, req.XsecToken)
	return &SeednoteCapabilityResult[*seednote.UserProfile]{Available: true, Value: value}, err
}

func (s *SeednoteCapabilityService) availability() (bool, string) {
	if s == nil || s.client == nil {
		return false, "Seednote sidecar 未配置或不可用"
	}
	if s.readiness != nil && !s.readiness.Ready() {
		return false, "Seednote sidecar 暂不可用，正在后台连接"
	}
	return true, ""
}
