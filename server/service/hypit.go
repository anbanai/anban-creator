package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"net/url"
	"strings"
)

var ErrHypitInput = errors.New("视频复刻输入无效")

type HypitCapabilityService struct{ config config.HypitConfig }

func NewHypitCapabilityService(c config.HypitConfig) *HypitCapabilityService {
	c.ApplyDefaults()
	return &HypitCapabilityService{c}
}

type HypitCapabilities struct {
	Enabled              bool               `json:"enabled"`
	Configured           bool               `json:"configured"`
	MissingConfiguration []string           `json:"missing_configuration"`
	Limits               config.HypitLimits `json:"limits"`
}

func (s *HypitCapabilityService) Catalog() HypitCapabilities {
	m := s.config.MissingConfiguration()
	return HypitCapabilities{s.config.Enabled, len(m) == 0, m, s.config.Limits}
}
func (s *TaskService) SetHypitConfig(c config.HypitConfig) {
	s.hypitCapabilities = NewHypitCapabilityService(c)
}
func (s *ProjectService) SetHypitCapabilityService(c *HypitCapabilityService) {
	s.hypitCapabilities = c
}
func (s *PlanService) SetHypitCapabilityService(c *HypitCapabilityService) { s.hypitCapabilities = c }
func (s *HypitCapabilityService) validatePreferences(p model.HypitPreferences) error {
	if p.DurationSeconds != nil && (*p.DurationSeconds < 0 || *p.DurationSeconds > s.config.Limits.MaxDurationSeconds) {
		return fmt.Errorf("%w: duration_seconds outside limits", ErrHypitInput)
	}
	switch p.AspectRatio {
	case "", "source", "9:16", "16:9", "1:1":
	default:
		return fmt.Errorf("%w: invalid aspect_ratio", ErrHypitInput)
	}
	return nil
}
func (s *HypitCapabilityService) NormalizeAndValidateInput(in *model.HypitInput, d model.HypitDefaults, reuse bool) error {
	if s == nil || !s.config.Enabled {
		return fmt.Errorf("%w: video replication is disabled", ErrHypitInput)
	}
	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("%w: invalid runtime configuration", ErrHypitInput)
	}
	if len(s.config.MissingConfiguration()) > 0 {
		return fmt.Errorf("%w: video replication is not configured", ErrHypitInput)
	}
	if in == nil || strings.TrimSpace(in.Brief) == "" {
		return fmt.Errorf("%w: brief is required", ErrHypitInput)
	}
	in.Brief = strings.TrimSpace(in.Brief)
	if in.Preferences.DurationSeconds == nil {
		if d.Preferences.DurationSeconds != nil {
			duration := *d.Preferences.DurationSeconds
			in.Preferences.DurationSeconds = &duration
		}
	}
	if in.Preferences.AspectRatio == "" {
		in.Preferences.AspectRatio = d.Preferences.AspectRatio
	}
	if in.Preferences.Language == "" {
		in.Preferences.Language = d.Preferences.Language
	}
	if err := s.validatePreferences(in.Preferences); err != nil {
		return err
	}
	if in.Reference == nil && !reuse {
		return fmt.Errorf("%w: reference video is required", ErrHypitInput)
	}
	if in.Reference != nil && in.Reference.Type != "video" && in.Reference.Type != "video_url" {
		return fmt.Errorf("%w: reference must be video", ErrHypitInput)
	}
	assets := hypitAssets(in)
	if len(in.SourceAssets) > s.config.Limits.MaxAssets {
		return fmt.Errorf("%w: too many assets", ErrHypitInput)
	}
	var total int64
	for _, a := range assets {
		switch a.Type {
		case "image", "video", "video_url", "audio":
		default:
			return fmt.Errorf("%w: invalid asset type", ErrHypitInput)
		}
		if (a.URL == "") == (a.TaskFileID == "") {
			return fmt.Errorf("%w: asset requires exactly one locator", ErrHypitInput)
		}
		if a.URL != "" {
			u, e := url.Parse(a.URL)
			if !strings.HasPrefix(a.URL, "/api/v1/files/") && !strings.HasPrefix(a.URL, "/files/") && (e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil) {
				return fmt.Errorf("%w: invalid media URL", ErrHypitInput)
			}
		}
		if a.FileSize < 0 || a.FileSize > s.config.Limits.MaxAssetBytes {
			return fmt.Errorf("%w: asset exceeds size limit", ErrHypitInput)
		}
		total += a.FileSize
	}
	if total > s.config.Limits.MaxInputBytes {
		return fmt.Errorf("%w: input exceeds size limit", ErrHypitInput)
	}
	return nil
}
func hypitAssets(in *model.HypitInput) []model.HypitAsset {
	if in == nil {
		return nil
	}
	a := append([]model.HypitAsset(nil), in.SourceAssets...)
	if in.Reference != nil {
		a = append(a, *in.Reference)
	}
	return a
}
func validateHypitTaskFiles(ctx context.Context, repo repository.Repository, user, project, provider string, in *model.HypitInput, l config.HypitLimits, trust ...montageSourceTaskTrust) error {
	var total int64
	for _, a := range hypitAssets(in) {
		if a.TaskFileID == "" {
			total += a.FileSize
			continue
		}
		m := model.MontageAsset{Type: a.Type, TaskFileID: a.TaskFileID}
		if err := ValidateMontageSourceTaskFiles(ctx, repo, user, project, []model.MontageAsset{m}, trust...); err != nil {
			return fmt.Errorf("%w: %v", ErrHypitInput, err)
		}
		f, err := repo.TaskFiles().FindByID(ctx, a.TaskFileID)
		if err != nil || f == nil || f.StorageProvider != provider || f.OSSKey == "" || f.FileSize <= 0 || f.FileSize > l.MaxAssetBytes || f.State != model.TaskFileStateDelivered || !lowercaseSHA256.MatchString(f.ContentHash) {
			return fmt.Errorf("%w: source asset unavailable or too large", ErrHypitInput)
		}
		total += f.FileSize
	}
	if total > l.MaxInputBytes {
		return fmt.Errorf("%w: aggregate input too large", ErrHypitInput)
	}
	return nil
}
func validateHypitProject(p *model.Project, s *HypitCapabilityService) error {
	if !model.IsHypitPlatform(p.Platform) {
		if p.HypitDefaultsSet {
			return fmt.Errorf("%w: hypit_defaults 仅适用于视频复刻项目", ErrHypitInput)
		}
		return nil
	}
	if s == nil || !s.config.Enabled {
		return fmt.Errorf("%w: video replication is disabled", ErrHypitInput)
	}
	return s.validatePreferences(p.HypitDefaults.Data().Preferences)
}

func (s *TaskService) HypitCapabilitiesForSource(ctx context.Context, userID, sourceID string) (HypitCapabilities, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sourceID) == "" {
		return HypitCapabilities{}, ErrHypitInput
	}
	capabilities, err := s.hypitAdmissionCapabilities(ctx, CreateManualParams{UserID: userID, InputSourceTaskID: sourceID})
	if err != nil {
		return HypitCapabilities{}, err
	}
	return capabilities.Catalog(), nil
}
