package wechat

import (
	"testing"

	"github.com/silenceper/wechat/v2/officialaccount/freepublish"
)

func TestMapPublishedItemsIncludesEveryArticleInAMultiArticlePublish(t *testing.T) {
	items := []freepublish.ArticleListItem{{
		ArticleID:  "publish-1",
		UpdateTime: 1723600000,
		Content: freepublish.ArticleListContent{NewsItem: []freepublish.Article{
			{Title: "头条", URL: "https://mp.weixin.qq.com/s/first"},
			{Title: "次条", URL: "https://mp.weixin.qq.com/s/second"},
		}},
	}}

	got := mapPublishedItems(items)
	if len(got) != 2 {
		t.Fatalf("item count = %d, want 2", len(got))
	}
	if got[1].ArticleID != "publish-1" || got[1].Title != "次条" || got[1].URL != "https://mp.weixin.qq.com/s/second" {
		t.Fatalf("second article = %+v", got[1])
	}
}
