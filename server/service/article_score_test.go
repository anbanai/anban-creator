package service

import "testing"

func TestArticleScoreCalculatesThresholdsAndRecommendations(t *testing.T) {
	svc := NewArticleScoreService()

	result, err := svc.Score(ArticleScoreRequest{
		ReadCount: 1_000_000_000, LikeCount: 100_000_000, ShareCount: 50_000_000,
		CommentCount: 30_000_000, CollectCount: 20_000_000, Topic: "tea",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != "超级爆款" || result.Score < 90 || result.Score > 100 {
		t.Fatalf("score = %.2f level = %q", result.Score, result.Level)
	}
	if result.Topic != "tea" || result.ReadCount != 1_000_000_000 || result.CollectCount != 20_000_000 {
		t.Fatalf("result did not preserve semantic inputs: %#v", result)
	}
	if len(result.Recommendations) != 1 || result.Recommendations[0] != "各项指标表现良好，继续保持！" {
		t.Fatalf("recommendations = %#v", result.Recommendations)
	}
}

func TestArticleScoreRejectsInvalidCounts(t *testing.T) {
	svc := NewArticleScoreService()
	if _, err := svc.Score(ArticleScoreRequest{ReadCount: 0}); err == nil {
		t.Fatal("expected zero reads to fail")
	}
	if _, err := svc.Score(ArticleScoreRequest{ReadCount: 1, LikeCount: -1}); err == nil {
		t.Fatal("expected negative likes to fail")
	}
}
