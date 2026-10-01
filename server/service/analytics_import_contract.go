package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func validateAnalyticsImport(platform, basis, key string, at *time.Time) error {
	if at == nil || at.IsZero() || at.Year() < 2000 || at.Year() > 9999 {
		return errors.New("请明确填写数据统计日期，文件修改时间不能代替统计日期")
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 {
		return errors.New("idempotency_key 必须为 1 到 128 个字符")
	}
	if basis != "cumulative" && !((platform == model.PlatformWechat || platform == model.ChannelArticle) && basis == "") {
		return errors.New("内容数据必须使用累计口径 cumulative")
	}
	return nil
}
func analyticsRequestFingerprint(fileHash, basis string, at time.Time, selections []AnalyticsSelection) string {
	ordered := append([]AnalyticsSelection(nil), selections...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].SourceRow < ordered[j].SourceRow })
	payload, _ := json.Marshal(struct {
		FileHash, Basis, At string
		Selections          []AnalyticsSelection
	}{fileHash, basis, at.UTC().Format(time.RFC3339Nano), ordered})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
