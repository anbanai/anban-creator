package config

// NewDefaultConfig 返回带有推荐默认值的 Config，用于生成配置文件模板
func NewDefaultConfig() *Config {
	c := &Config{}

	// 账号基本信息
	c.Name = "your_account_name"
	c.Keywords = []string{"keyword1", "keyword2"}
	c.Positioning = "your_account_positioning"

	// 微信公众号认证
	c.Wechat.AppID = "your_wechat_appid"
	c.Wechat.Secret = "your_wechat_secret"

	// 图文文章
	c.Wechat.Article.Byline = "your_author_name"
	c.Wechat.Article.WriterKey = DefaultArticleWriterKey
	c.Wechat.Article.Theme = DefaultArticleTheme
	// 文章封面图
	c.Wechat.Article.Cover.Image.Provider = DefaultImageProvider
	c.Wechat.Article.Cover.Image.Key = "your_image_api_key"
	c.Wechat.Article.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Article.Cover.Image.Size = DefaultArticleImageSize
	c.Wechat.Article.Cover.Image.Compress = true
	c.Wechat.Article.Cover.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Article.Cover.Image.MaxSizeMB = DefaultImageMaxSizeMB
	// 文章内容配图
	c.Wechat.Article.Content.Image.Provider = DefaultImageProvider
	c.Wechat.Article.Content.Image.Key = "your_image_api_key"
	c.Wechat.Article.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Article.Content.Image.Size = DefaultArticleImageSize
	c.Wechat.Article.Content.Image.Compress = true
	c.Wechat.Article.Content.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Article.Content.Image.MaxSizeMB = DefaultImageMaxSizeMB

	// 种草笔记（可选平台）
	c.Seednote = new(SeednoteConfig)
	c.Seednote.VisualStyle = "cute-doodle"
	c.Seednote.Cover.Image.Provider = DefaultImageProvider
	c.Seednote.Cover.Image.Key = "your_image_api_key"
	c.Seednote.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Seednote.Cover.Image.Size = DefaultSeednoteImageSize
	c.Seednote.Cover.Image.Refer = "path/to/refer.png"
	c.Seednote.Cover.Image.Compress = true
	c.Seednote.Content.Image.Provider = DefaultImageProvider
	c.Seednote.Content.Image.Key = "your_image_api_key"
	c.Seednote.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Seednote.Content.Image.Size = DefaultSeednoteImageSize
	c.Seednote.Content.Image.Refer = "path/to/refer.png"
	c.Seednote.Content.Image.Compress = true
	c.Seednote.Content.Count = DefaultSeednoteImageCount

	return c
}
