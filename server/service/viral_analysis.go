package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
)

const ViralAnalysisTaskType = "viral:analyze"

// ViralNoteFetcher abstracts fetching Xiaohongshu note content.
type ViralNoteFetcher interface {
	FetchNoteContent(ctx context.Context, noteURL string) (*platform.RednoteNoteContent, error)
}

// ViralAnalysisService handles viral content analysis business logic.
type ViralAnalysisService struct {
	repo     repository.Repository
	fetcher  ViralNoteFetcher
	llm      LLMClient
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
}

// NewViralAnalysisService creates a new ViralAnalysisService.
func NewViralAnalysisService(repo repository.Repository, fetcher ViralNoteFetcher, llm LLMClient, enqueuer TaskEnqueuer, logger *zerolog.Logger) *ViralAnalysisService {
	return &ViralAnalysisService{
		repo:     repo,
		fetcher:  fetcher,
		llm:      llm,
		enqueuer: enqueuer,
		logger:   logger,
	}
}

// Create creates a new viral analysis record and enqueues it for execution.
func (s *ViralAnalysisService) Create(ctx context.Context, userID, sourceType, sourceURL string) (*model.ViralAnalysis, error) {
	analysis := &model.ViralAnalysis{
		ID:         uuid.New().String(),
		UserID:     userID,
		SourceType: sourceType,
		SourceURL:  sourceURL,
		Status:     "pending",
	}

	if err := s.repo.ViralAnalyses().Create(ctx, analysis); err != nil {
		return nil, fmt.Errorf("create viral analysis: %w", err)
	}

	s.logger.Info().
		Str("analysis_id", analysis.ID).
		Str("user_id", userID).
		Str("source_type", sourceType).
		Msg("viral analysis created")

	if s.enqueuer != nil {
		payload, err := json.Marshal(map[string]string{"analysis_id": analysis.ID})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		if err := s.enqueuer.Enqueue(ViralAnalysisTaskType, payload); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", analysis.ID).Msg("failed to enqueue viral analysis")
		}
	} else {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error().
						Str("analysis_id", analysis.ID).
						Interface("panic", r).
						Msg("panic recovered in viral analysis goroutine")
				}
			}()
			fallbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := s.ExecuteAnalysis(fallbackCtx, analysis.ID); err != nil {
				s.logger.Error().Err(err).Str("analysis_id", analysis.ID).Msg("fallback viral analysis failed")
			}
		}()
	}

	return analysis, nil
}

// GetByID returns a viral analysis by ID, verifying ownership.
func (s *ViralAnalysisService) GetByID(ctx context.Context, id, userID string) (*model.ViralAnalysis, error) {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("viral analysis not found: %s", id)
	}

	if analysis.UserID != userID {
		return nil, fmt.Errorf("viral analysis does not belong to user: %s", id)
	}

	return analysis, nil
}

// ListByUserID returns paginated viral analyses for a user.
func (s *ViralAnalysisService) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, int64, error) {
	analyses, err := s.repo.ViralAnalyses().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list viral analyses: %w", err)
	}

	total, err := s.repo.ViralAnalyses().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count viral analyses: %w", err)
	}

	return analyses, total, nil
}

// ExecuteAnalysis runs the full viral analysis pipeline.
func (s *ViralAnalysisService) ExecuteAnalysis(ctx context.Context, analysisID string) error {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, analysisID)
	if err != nil {
		return fmt.Errorf("find analysis: %w", err)
	}

	if analysis.Status != "pending" {
		s.logger.Info().Str("analysis_id", analysisID).Str("status", analysis.Status).Msg("analysis not in pending state, skipping")
		return nil
	}

	if err := s.StartAnalysis(ctx, analysisID); err != nil {
		return fmt.Errorf("start analysis: %w", err)
	}

	if s.fetcher == nil {
		return s.FailAnalysis(ctx, analysisID, "note fetcher unavailable")
	}

	content, err := s.fetcher.FetchNoteContent(ctx, analysis.SourceURL)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("fetch note content: %v", err))
	}

	// Store fetched source data.
	sourceData, err := json.Marshal(content)
	if err != nil {
		s.logger.Warn().Err(err).Str("analysis_id", analysisID).Msg("failed to marshal source data")
	} else {
		if err := s.repo.ViralAnalyses().UpdateSourceData(ctx, analysisID, sourceData); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", analysisID).Msg("failed to store source data")
		}
	}

	if s.llm == nil {
		return s.FailAnalysis(ctx, analysisID, "AI analysis service unavailable")
	}

	result, err := s.analyzeWithLLM(ctx, content)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("AI analysis failed: %v", err))
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("marshal result: %v", err))
	}

	return s.CompleteAnalysis(ctx, analysisID, resultJSON)
}

func (s *ViralAnalysisService) analyzeWithLLM(ctx context.Context, content *platform.RednoteNoteContent) (json.RawMessage, error) {
	noteData, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal note content: %w", err)
	}

	systemPrompt := `你是小红书爆款内容分析专家。请从标题、封面、文案、标签、互动五个维度深度分析这篇笔记，返回严格的 JSON 格式。

分析要求：
1. 每个维度的 score 为 0-100 的整数
2. 标题分析：拆解标题技巧（如数字、悬念、情绪、对比等），给出改写建议
3. 封面分析：分析封面风格和关键元素
4. 文案分析：分析文案结构、使用的公式和字数
5. 标签分析：提取使用的标签，并建议更优标签
6. 互动分析：分析互动技巧和 CTA 类型
7. viral_factors 各项为 0-100 的整数，表示该维度的爆款潜力
8. overall_score 为综合爆款分数 0-100
9. suggestions 为 3-5 条具体的优化建议

只返回 JSON，不要返回任何其他文字。JSON 格式如下：
{
  "title_analysis": {"score": 0, "technique": "", "breakdown": "", "rewrite_suggestions": []},
  "cover_analysis": {"score": 0, "style": "", "breakdown": "", "key_elements": []},
  "copywriting_analysis": {"score": 0, "structure": "", "breakdown": "", "formulas_used": [], "word_count": 0},
  "tag_analysis": {"score": 0, "tags": [], "breakdown": "", "suggested_tags": []},
  "interaction_analysis": {"score": 0, "techniques": [], "breakdown": "", "cta_type": ""},
  "overall_score": 0,
  "viral_factors": {"topic": 0, "title": 0, "content": 0, "visual": 0, "interaction": 0},
  "suggestions": []
}`

	userPrompt := string(noteData)

	resp, err := s.llm.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Strip markdown code fences if present.
	trimmed := trimCodeFences(resp)

	var result map[string]any
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return nil, fmt.Errorf("parse LLM response as JSON: %w", err)
	}

	return []byte(trimmed), nil
}

// trimCodeFences removes markdown ```json ... ``` wrappers from LLM output.
func trimCodeFences(s string) string {
	if len(s) >= 7 && s[:7] == "```json" {
		s = s[7:]
	}
	if len(s) >= 3 && s[:3] == "```" {
		s = s[3:]
	}
	if len(s) >= 3 && s[len(s)-3:] == "```" {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// StartAnalysis sets the viral analysis status to "analyzing".
func (s *ViralAnalysisService) StartAnalysis(ctx context.Context, id string) error {
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "analyzing"); err != nil {
		return fmt.Errorf("start analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis started")
	return nil
}

// CompleteAnalysis stores the analysis result and sets status to "completed".
func (s *ViralAnalysisService) CompleteAnalysis(ctx context.Context, id string, result json.RawMessage) error {
	if err := s.repo.ViralAnalyses().UpdateResult(ctx, id, result); err != nil {
		return fmt.Errorf("update analysis result: %w", err)
	}
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "completed"); err != nil {
		return fmt.Errorf("complete analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis completed")
	return nil
}

// FailAnalysis sets the viral analysis status to "failed" with an error message.
func (s *ViralAnalysisService) FailAnalysis(ctx context.Context, id, errMsg string) error {
	if err := s.repo.ViralAnalyses().UpdateStatusAndError(ctx, id, "failed", errMsg); err != nil {
		return fmt.Errorf("fail analysis: %w", err)
	}

	s.logger.Warn().Str("analysis_id", id).Str("error", errMsg).Msg("viral analysis failed")
	return nil
}

// CleanupOldCompleted deletes viral analyses completed more than 90 days ago.
func (s *ViralAnalysisService) CleanupOldCompleted(ctx context.Context) error {
	analyses, err := s.repo.ViralAnalyses().FindCompletedOlderThan(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil {
		return fmt.Errorf("find old viral analyses: %w", err)
	}
	for _, a := range analyses {
		s.logger.Info().Str("analysis_id", a.ID).Msg("cleaning up old viral analysis")
		if err := s.repo.ViralAnalyses().Delete(ctx, a.ID); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", a.ID).Msg("failed to delete old viral analysis")
		}
	}
	return nil
}
