package resources

import "embed"

//go:embed themes/*.yaml
var ThemesFS embed.FS

//go:embed writers/*.yaml
var WritersFS embed.FS

//go:embed layouts/*.yaml
var LayoutsFS embed.FS

//go:embed article_templates/*.yaml
var ArticleTemplatesFS embed.FS
