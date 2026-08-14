package platform

import "testing"

func TestNormalizeWechatArticleURL(t *testing.T) {
	got, err := NormalizeWechatArticleURL("https://mp.weixin.qq.com/s?__biz=biz&mid=1&idx=2&sn=abc&chksm=share#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mp.weixin.qq.com/s?__biz=biz&idx=2&mid=1&sn=abc" {
		t.Fatalf("normalized URL = %q", got)
	}
	for _, raw := range []string{
		"http://mp.weixin.qq.com/s/article",
		"https://example.com/s/article",
		"https://user@mp.weixin.qq.com/s/article",
		"https://mp.weixin.qq.com:443/s/article",
		"https://mp.weixin.qq.com/profile/article",
	} {
		if _, err := NormalizeWechatArticleURL(raw); err == nil {
			t.Errorf("NormalizeWechatArticleURL(%q) succeeded", raw)
		}
	}
}

func TestLatestWechatMetricUsesLatestStatDate(t *testing.T) {
	got, ok := LatestWechatMetric([]WechatArticleMetric{{StatDate: "2026-08-13", IntPageReadCount: 20}, {StatDate: "2026-08-12", IntPageReadCount: 10}})
	if !ok || got.IntPageReadCount != 20 {
		t.Fatalf("latest metric = %+v, ok=%v", got, ok)
	}
}
