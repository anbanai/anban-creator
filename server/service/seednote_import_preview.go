package service

import (
	"context"
	"errors"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"time"

	"github.com/anbanai/anban-creator/server/repository"
)

type SeednoteImportPreview struct {
	FileName  string                     `json:"file_name"`
	TotalRows int                        `json:"total_rows"`
	Rows      []SeednoteImportPreviewRow `json:"rows"`
}

type SeednoteImportPreviewRow struct {
	ParsedSeednoteRow
	ContentType string           `json:"content_type"`
	TargetTitle string           `json:"target_title,omitempty"`
	MatchStatus string           `json:"match_status"`
	Target      *AnalyticsTarget `json:"target,omitempty"`
}

func (s *SeednoteImportService) Preview(ctx context.Context, req SeednoteImportRequest) (*SeednoteImportPreview, error) {
	asset, _, parsed, _, err := s.loadWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	candidates, err := loadAnalyticsCandidates(ctx, s.repo, req.UserID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	result := &SeednoteImportPreview{FileName: asset.FileName, TotalRows: len(parsed.Rows), Rows: make([]SeednoteImportPreviewRow, 0, len(parsed.Rows))}
	for _, row := range parsed.Rows {
		item := SeednoteImportPreviewRow{ParsedSeednoteRow: row, ContentType: row.Genre, MatchStatus: "unmatched"}
		if item.ContentType == "" {
			item.ContentType = "unknown"
		}
		if row.ParseError != "" {
			item.MatchStatus = "invalid"
		} else {
			candidate, ambiguous, err := matchSeednoteImportCandidate(ctx, s.repo, row, candidates)
			if err != nil {
				return nil, err
			}
			if ambiguous {
				item.MatchStatus = "needs_review"
			} else if candidate != nil {
				target := candidate.Target
				item.Target, item.MatchStatus = &target, "matched"
				item.TargetTitle = candidate.Title
			}
		}
		result.Rows = append(result.Rows, item)
	}
	return result, nil
}

func matchSeednoteImportCandidate(ctx context.Context, repo repository.Repository, row ParsedSeednoteRow, candidates []AnalyticsCandidate) (*AnalyticsCandidate, bool, error) {
	aliases, err := repo.SeednotePostAliases().FindBySignature(ctx, row.NormalizedTitle, row.FirstPublishedAt)
	if err != nil {
		return nil, false, err
	}
	// Include learned titles in the same ambiguity check as current titles.
	// Candidate aliases keep older post IDs scoped to their canonical task.
	matching := append([]AnalyticsCandidate(nil), candidates...)
	for i := range matching {
		candidate := &matching[i]
		for _, alias := range aliases {
			samePost := candidate.Post != nil && candidate.Post.ID == alias.PostID
			for _, identity := range candidate.aliases {
				samePost = samePost || identity == (AnalyticsTarget{Kind: "seednote_post", ID: alias.PostID})
			}
			if samePost {
				candidate.titles = append(append([]string(nil), candidate.titles...), row.Title)
				if alias.FirstPublishedAt != nil {
					candidate.matchDate = alias.FirstPublishedAt
				}
			}
		}
	}
	candidate, ambiguous := matchAnalyticsCandidate(row.Title, row.FirstPublishedAt, "", matching)
	return candidate, ambiguous, nil
}

// Import-owned publication dates are evidence only while their source is active.
func reliableSeednotePostPublishedAt(ctx context.Context, repo repository.Repository, post *model.SeednotePost) (*time.Time, error) {
	if post.FirstPublishedAt == nil || post.FirstPublishedAtBatchID == "" {
		return post.FirstPublishedAt, nil
	}
	batch, err := repo.SeednoteImports().FindBatchByID(ctx, post.ProjectID, post.FirstPublishedAtBatchID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if batch.RevokedAt != nil {
		return nil, nil
	}
	return post.FirstPublishedAt, nil
}
