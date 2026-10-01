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

func TestMapOfficialPublishedItemsIncludesNewspicMediaIDs(t *testing.T) {
	items := []FreePublishBatchItem{{
		ArticleID:  "publish-picture-1",
		UpdateTime: 1723600000,
		Content: FreePublishContent{NewspicInfo: &DraftArticle{
			Title: "图片消息",
			ImageInfo: &DraftImageInfo{ImageList: []DraftImage{
				{ImageMediaID: "media-cover"},
				{ImageMediaID: "media-page-1"},
			}},
		}},
	}}

	got := mapOfficialPublishedItems(items)
	if len(got) != 1 || got[0].ArticleType != "newspic" || got[0].Title != "图片消息" {
		t.Fatalf("published picture mapping = %#v", got)
	}
	if len(got[0].ImageMediaIDs) != 2 || got[0].ImageMediaIDs[0] != "media-cover" || got[0].ImageMediaIDs[1] != "media-page-1" {
		t.Fatalf("published picture media IDs = %#v", got[0].ImageMediaIDs)
	}
}
