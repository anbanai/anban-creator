package service

import (
	"fmt"
	"math"
)

type ArticleScoreRequest struct {
	ReadCount    int64
	LikeCount    int64
	ShareCount   int64
	CommentCount int64
	CollectCount int64
	Topic        string
}

type ArticleScoreResult struct {
	Score           float64  `json:"score"`
	Level           string   `json:"level"`
	Topic           string   `json:"topic"`
	ReadCount       int64    `json:"read_count"`
	LikeCount       int64    `json:"like_count"`
	ShareCount      int64    `json:"share_count"`
	CommentCount    int64    `json:"comment_count"`
	CollectCount    int64    `json:"collect_count"`
	EngagementRate  float64  `json:"engagement_rate"`
	ShareRate       float64  `json:"share_rate"`
	LikeRate        float64  `json:"like_rate"`
	CommentRate     float64  `json:"comment_rate"`
	Recommendations []string `json:"recommendations"`
}

type ArticleScoreService struct{}

func NewArticleScoreService() *ArticleScoreService {
	return &ArticleScoreService{}
}

func (s *ArticleScoreService) Score(req ArticleScoreRequest) (*ArticleScoreResult, error) {
	if req.ReadCount <= 0 {
		return nil, fmt.Errorf("read_count must be positive")
	}
	if req.LikeCount < 0 {
		return nil, fmt.Errorf("like_count must be non-negative")
	}

	engagementRate := float64(req.LikeCount+req.ShareCount+req.CommentCount) / float64(req.ReadCount)
	shareRate := float64(req.ShareCount) / float64(req.ReadCount)
	likeRate := float64(req.LikeCount) / float64(req.ReadCount)
	commentRate := float64(req.CommentCount) / float64(req.ReadCount)
	score := articleViralScore(req.ReadCount, engagementRate, shareRate, commentRate)

	return &ArticleScoreResult{
		Score: score, Level: articleViralLevel(score), Topic: req.Topic,
		ReadCount: req.ReadCount, LikeCount: req.LikeCount, ShareCount: req.ShareCount,
		CommentCount: req.CommentCount, CollectCount: req.CollectCount,
		EngagementRate: engagementRate, ShareRate: shareRate, LikeRate: likeRate,
		CommentRate:     commentRate,
		Recommendations: articleScoreRecommendations(engagementRate, shareRate, likeRate, commentRate),
	}, nil
}

func articleViralScore(readCount int64, engagementRate, shareRate, commentRate float64) float64 {
	readScore := math.Min(math.Log10(float64(readCount))*10, 100)
	score := readScore*0.3 + math.Min(engagementRate*1000, 30) +
		math.Min(shareRate*2500, 25) + math.Min(commentRate*3750, 15)
	return math.Min(score, 100)
}

func articleViralLevel(score float64) string {
	switch {
	case score >= 90:
		return "超级爆款"
	case score >= 80:
		return "热门爆款"
	case score >= 70:
		return "优质内容"
	case score >= 60:
		return "潜力内容"
	case score >= 50:
		return "普通内容"
	default:
		return "待优化"
	}
}

func articleScoreRecommendations(engagementRate, shareRate, likeRate, commentRate float64) []string {
	var recommendations []string
	if engagementRate < 0.02 {
		recommendations = append(recommendations, "互动率偏低，建议增加互动引导或话题讨论点")
	}
	if shareRate < 0.01 {
		recommendations = append(recommendations, "分享率偏低，建议增加实用价值或情感共鸣点")
	}
	if commentRate < likeRate*0.05 {
		recommendations = append(recommendations, "评论率偏低，建议增加争议性或思考性内容")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "各项指标表现良好，继续保持！")
	}
	return recommendations
}
