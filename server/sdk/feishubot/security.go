package feishubot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// Security 安全相关功能

// VerifySignature 验证来自飞书的请求签名
// timestamp: Unix 秒级时间戳
// sign: 签名字符串
// secretKey: 安全设置中配置的签名密钥
func VerifySignature(timestamp string, sign, secretKey string) bool {
	if secretKey == "" || sign == "" || timestamp == "" {
		return false
	}

	// 将字符串时间戳转换为int64
	timestampInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	// The provider timestamp is Unix seconds; reject stale and future replays.
	now := time.Now().Unix()
	if timestampInt < now-600 || timestampInt > now+600 {
		return false
	}

	// 拼接待签名字符串
	stringToSign := fmt.Sprintf("%d\n%s", timestampInt, secretKey)

	// 使用HMAC-SHA256算法计算签名
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(stringToSign))
	expectedSign := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 比较签名
	return hmac.Equal([]byte(sign), []byte(expectedSign))
}

// GenerateSignature 生成签名
// timestamp: Unix 秒级时间戳
// secretKey: 安全设置中配置的签名密钥
func GenerateSignature(timestamp int64, secretKey string) string {
	if secretKey == "" {
		return ""
	}

	// 拼接待签名字符串
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secretKey)

	// 使用HMAC-SHA256算法计算签名
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}
