package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

func validateWhiteboardAnimationInputs(attachments []model.EntryAttachment) error {
	var subtitles []model.EntryAttachment
	styleReferences := 0
	for _, attachment := range attachments {
		if strings.EqualFold(filepath.Ext(strings.TrimSpace(attachment.FileName)), ".srt") {
			subtitles = append(subtitles, attachment)
			continue
		}
		if attachment.Type != "image" {
			return fmt.Errorf("白板动画任务仅支持一个 SRT 字幕文件和一张可选风格参考图")
		}
		styleReferences++
		if styleReferences > 1 {
			return fmt.Errorf("白板动画任务必须且只能包含一个 SRT 字幕文件，并且最多包含一张风格参考图")
		}
	}
	if len(subtitles) != 1 {
		return fmt.Errorf("白板动画任务必须且只能包含一个 SRT 字幕文件")
	}
	subtitle := subtitles[0]
	if subtitle.Type != "document" && subtitle.Type != "text" {
		return fmt.Errorf("SRT 字幕附件类型必须是 document 或 text")
	}
	if strings.TrimSpace(subtitle.Text) == "" && strings.TrimSpace(subtitle.AssetID) == "" &&
		strings.TrimSpace(subtitle.UploadID) == "" && strings.TrimSpace(subtitle.Key) == "" && strings.TrimSpace(subtitle.URL) == "" {
		return fmt.Errorf("SRT 字幕附件没有可读取内容")
	}
	return nil
}
