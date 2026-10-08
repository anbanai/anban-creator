package service

import (
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

// normalizeWechatContentType maps provider draft labels and historical import
// labels onto the public task/content identities used by analytics.
func normalizeWechatContentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.TaskTypeWechatArticle, "", "news", "article", "图文", "图文消息", "公众号图文", "文章":
		return model.TaskTypeWechatArticle
	case model.TaskTypeWechatPicture, "newspic", "image", "图片", "图片消息", "贴图", "图集", "公众号贴图", "公众号图片":
		return model.TaskTypeWechatPicture
	default:
		return "unknown"
	}
}
