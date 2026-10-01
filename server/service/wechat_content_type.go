package service

import (
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

// normalizeWechatContentType maps provider draft labels and historical import
// labels onto the public task/content identities used by analytics.
func normalizeWechatContentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "news", "article", "图文", "图文消息", "文章":
		return model.TaskTypeWechatArticle
	case "newspic", "image", "图片", "图片消息", "贴图", "图集":
		return model.TaskTypeWechatPicture
	default:
		return strings.TrimSpace(value)
	}
}
