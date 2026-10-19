package service

import (
	"context"
	"errors"
	"strings"
)

var ErrImageSearchUnavailable = errors.New("image search provider unavailable")

type ImageSearchService struct {
	enabled  bool
	provider ImageSearchProvider
}

func NewImageSearchService(enabled bool, provider ImageSearchProvider) *ImageSearchService {
	return &ImageSearchService{enabled: enabled, provider: provider}
}

func (s *ImageSearchService) SearchImages(ctx context.Context, req ImageSearchImagesRequest) (*ImageSearchResponse, error) {
	if s == nil || !s.enabled || s.provider == nil {
		return nil, ErrImageSearchUnavailable
	}
	if (strings.TrimSpace(req.Query) == "") == (strings.TrimSpace(req.ImageURL) == "") {
		return nil, errors.New("exactly one of query or image_url is required")
	}
	if imageURL := strings.TrimSpace(req.ImageURL); imageURL != "" {
		if err := ValidatePublicHTTPURL(imageURL); err != nil {
			return nil, err
		}
	}
	return s.provider.SearchImages(ctx, req)
}
