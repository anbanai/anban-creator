package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/spf13/cobra"
)

// ViralMetrics 爆款评估指标
type ViralMetrics struct {
	ReadCount      int64   `json:"read_count"`
	LikeCount      int64   `json:"like_count"`
	ShareCount     int64   `json:"share_count"`
	CommentCount   int64   `json:"comment_count"`
	CollectCount   int64   `json:"collect_count"`
	EngagementRate float64 `json:"engagement_rate"`
	ShareRate      float64 `json:"share_rate"`
	LikeRate       float64 `json:"like_rate"`
	CommentRate    float64 `json:"comment_rate"`
	ViralScore     float64 `json:"viral_score"`
	ViralLevel     string  `json:"viral_level"`
}

// ScoreRequest 评分请求
type ScoreRequest struct {
	Metrics  ViralMetrics `json:"metrics"`
	Topic    string       `json:"topic"`
	Platform string       `json:"platform"`
	Domain   string       `json:"domain"`
}

func scoreCmd() *cobra.Command {
	var (
		inputFile string
		topic     string
		domain    string
	)

	cmd := &cobra.Command{
		Use:   "score",
		Short: "基于互动数据计算文章爆款潜力评分",
		Long: `基于真实互动数据计算文章爆款潜力评分

输入真实的文章互动统计数据（阅读数、点赞数、分享数、评论数、收藏数），
计算综合爆款评分及各维度互动率，并给出优化建议。

支持的指标：
- 阅读量、点赞量、分享量、评论量、收藏量
- 互动率、分享率、点赞率、评论率等衍生指标
- 综合爆款潜力评级

注意：需要提供真实互动数据，不是预测性话题评分。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScore(inputFile, topic, domain)
		},
	}

	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "输入文件路径（JSON格式），留空则从stdin读取")
	cmd.Flags().StringVarP(&topic, "topic", "t", "", "话题名称")
	cmd.Flags().StringVarP(&domain, "domain", "d", "tea", "领域配置 (tea, tech, lifestyle)")

	return cmd
}

func runScore(inputFile, topic, domain string) error {
	if inputFile == "" && isTerminal() {
		return fmt.Errorf("需要提供评分数据。用法:\n" +
			"  wechatwriter score -i metrics.json\n" +
			"  echo '{\"read_count\":8500,\"like_count\":350}' | wechatwriter score -t \"话题\"\n\n" +
			"JSON 格式示例:\n" +
			"  {\"read_count\":8500,\"like_count\":350,\"share_count\":120,\"comment_count\":80}")
	}

	var input io.Reader = os.Stdin
	if inputFile != "" {
		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("无法打开输入文件: %w", err)
		}
		defer f.Close()
		input = f
	}

	data, err := io.ReadAll(input)
	if err != nil {
		return fmt.Errorf("读取输入失败: %w", err)
	}

	var request ScoreRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return fmt.Errorf("解析输入失败: %w", err)
	}

	// 支持平铺格式：{"read_count": 8500, ...}
	if request.Metrics.ReadCount == 0 {
		_ = json.Unmarshal(data, &request.Metrics)
	}

	// 设置话题和领域
	if request.Topic == "" {
		request.Topic = topic
	}
	if request.Domain == "" {
		request.Domain = domain
	}

	// 计算评分
	if request.Metrics.ReadCount > 0 {
		request.Metrics.EngagementRate = float64(request.Metrics.LikeCount+request.Metrics.ShareCount+request.Metrics.CommentCount) / float64(request.Metrics.ReadCount)
		request.Metrics.ShareRate = float64(request.Metrics.ShareCount) / float64(request.Metrics.ReadCount)
		request.Metrics.LikeRate = float64(request.Metrics.LikeCount) / float64(request.Metrics.ReadCount)
		request.Metrics.CommentRate = float64(request.Metrics.CommentCount) / float64(request.Metrics.ReadCount)
	}

	request.Metrics.ViralScore = calculateViralScore(request)
	request.Metrics.ViralLevel = getViralLevel(request.Metrics.ViralScore)

	// 输出结果
	result := map[string]any{
		"score":           request.Metrics.ViralScore,
		"level":           request.Metrics.ViralLevel,
		"topic":           request.Topic,
		"domain":          request.Domain,
		"engagement_rate": request.Metrics.EngagementRate,
		"share_rate":      request.Metrics.ShareRate,
		"metrics":         request.Metrics,
		"recommendations": generateRecommendations(request.Metrics),
	}

	responseSuccess(result)
	return nil
}

func calculateViralScore(req ScoreRequest) float64 {
	score := 0.0

	// 阅读量评分 (30%)
	if req.Metrics.ReadCount > 0 {
		readScore := math.Min(math.Log10(float64(req.Metrics.ReadCount))*10, 100)
		score += readScore * 0.3
	}

	// 互动率评分 (30%)
	engagementScore := math.Min(req.Metrics.EngagementRate*1000, 30)
	score += engagementScore

	// 分享率评分 (25%)
	shareScore := math.Min(req.Metrics.ShareRate*2500, 25)
	score += shareScore

	// 评论率评分 (15%)
	commentScore := math.Min(req.Metrics.CommentRate*3750, 15)
	score += commentScore

	return math.Min(score, 100)
}

func getViralLevel(score float64) string {
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

func generateRecommendations(metrics ViralMetrics) []string {
	var recs []string

	if metrics.EngagementRate < 0.02 {
		recs = append(recs, "互动率偏低，建议增加互动引导或话题讨论点")
	}

	if metrics.ShareRate < 0.01 {
		recs = append(recs, "分享率偏低，建议增加实用价值或情感共鸣点")
	}

	if metrics.CommentRate < metrics.LikeRate*0.05 {
		recs = append(recs, "评论率偏低，建议增加争议性或思考性内容")
	}

	if len(recs) == 0 {
		recs = append(recs, "各项指标表现良好，继续保持！")
	}

	return recs
}
