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
	c.Wechat.Article.Author = "your_author_name"
	c.Wechat.Article.Style = DefaultArticleStyle
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

	// 小绿书
	c.Wechat.Xls.Style = "flat-vector"
	// 小绿书封面图
	c.Wechat.Xls.Cover.Image.Provider = DefaultImageProvider
	c.Wechat.Xls.Cover.Image.Key = "your_image_api_key"
	c.Wechat.Xls.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Xls.Cover.Image.Size = DefaultXlsImageSize
	c.Wechat.Xls.Cover.Image.Refer = "path/to/refer.png"
	c.Wechat.Xls.Cover.Image.Compress = true
	c.Wechat.Xls.Cover.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Xls.Cover.Image.MaxSizeMB = DefaultImageMaxSizeMB
	// 小绿书内容图
	c.Wechat.Xls.Content.Count = DefaultXlsImageCount
	c.Wechat.Xls.Content.Image.Provider = DefaultImageProvider
	c.Wechat.Xls.Content.Image.Key = "your_image_api_key"
	c.Wechat.Xls.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Xls.Content.Image.Size = DefaultXlsImageSize
	c.Wechat.Xls.Content.Image.Refer = "path/to/refer.png"
	c.Wechat.Xls.Content.Image.Compress = true
	c.Wechat.Xls.Content.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Xls.Content.Image.MaxSizeMB = DefaultImageMaxSizeMB

	// 种草笔记（可选平台）
	c.Seednote = new(SeednoteConfig)
	c.Seednote.Style = "cute-doodle"
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

	// 花卉图片生成（可选）
	c.Flower = &FlowerConfig{}
	c.Flower.Content.Image.Provider = DefaultImageProvider
	c.Flower.Content.Image.Key = "your_image_api_key"
	c.Flower.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Flower.Content.Image.Size = DefaultFlowerImageSize
	c.Flower.Content.Image.Refer = "path/to/refer.png"
	c.Flower.Content.Image.Compress = true
	c.Flower.Content.Image.MaxWidth = DefaultImageMaxWidth
	c.Flower.Content.Image.MaxSizeMB = DefaultImageMaxSizeMB
	c.Flower.Content.Count = DefaultFlowerImageCount

	return c
}
