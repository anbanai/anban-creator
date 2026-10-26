package config

import (
	"encoding/base64"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type WechatOpenPlatformConfig struct {
	ComponentAppID     string `yaml:"component_app_id"`
	ComponentAppSecret string `yaml:"component_app_secret"`
	MessageToken       string `yaml:"message_token"`
	EncodingAESKey     string `yaml:"encoding_aes_key"`
	TokenEncryptionKey string `yaml:"token_encryption_key"`
}

func (c *WechatOpenPlatformConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := validateYAMLMappingFields(value, "wechat_open_platform", map[string]bool{
		"component_app_id": true, "component_app_secret": true, "message_token": true,
		"encoding_aes_key": true, "token_encryption_key": true,
	}); err != nil {
		return err
	}
	type plain WechatOpenPlatformConfig
	return value.Decode((*plain)(c))
}

func (c WechatOpenPlatformConfig) Enabled() bool {
	return strings.TrimSpace(c.ComponentAppID) != ""
}

func (c WechatOpenPlatformConfig) Validate() error {
	values := []string{c.ComponentAppID, c.ComponentAppSecret, c.MessageToken, c.EncodingAESKey, c.TokenEncryptionKey}
	configured := false
	for _, value := range values {
		configured = configured || strings.TrimSpace(value) != ""
	}
	if !configured {
		return nil
	}
	for index, name := range []string{"component_app_id", "component_app_secret", "message_token", "encoding_aes_key", "token_encryption_key"} {
		if strings.TrimSpace(values[index]) == "" {
			return fmt.Errorf("wechat_open_platform.%s is required when Open Platform authorization is enabled", name)
		}
	}
	aesKey := strings.TrimSpace(c.EncodingAESKey)
	if len(aesKey) != 43 {
		return fmt.Errorf("wechat_open_platform.encoding_aes_key must be 43 characters")
	}
	decodedAESKey, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil || len(decodedAESKey) != 32 {
		return fmt.Errorf("wechat_open_platform.encoding_aes_key must be valid base64 encoding of 32 bytes")
	}
	key := strings.TrimSpace(c.TokenEncryptionKey)
	if len(key) == 32 {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("wechat_open_platform.token_encryption_key must be 32 bytes or base64-encoded 32 bytes")
	}
	return nil
}
