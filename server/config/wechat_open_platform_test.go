package config

import "testing"

func TestWechatOpenPlatformConfigIsOptionalButRequiresAllSecretsWhenEnabled(t *testing.T) {
	if err := (WechatOpenPlatformConfig{}).Validate(); err != nil {
		t.Fatalf("empty optional config: %v", err)
	}
	partial := WechatOpenPlatformConfig{ComponentAppID: "component-app"}
	if err := partial.Validate(); err == nil {
		t.Fatal("partial Open Platform config was accepted")
	}
	valid := WechatOpenPlatformConfig{
		ComponentAppID: "component-app", ComponentAppSecret: "component-secret",
		MessageToken: "callback-token", EncodingAESKey: "1234567890123456789012345678901234567890123",
		TokenEncryptionKey: "0123456789abcdef0123456789abcdef",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Open Platform config: %v", err)
	}
}

func TestWechatOpenPlatformConfigRejectsWeakRefreshTokenEncryptionKey(t *testing.T) {
	config := WechatOpenPlatformConfig{
		ComponentAppID: "component-app", ComponentAppSecret: "component-secret",
		MessageToken: "callback-token", EncodingAESKey: "1234567890123456789012345678901234567890123",
		TokenEncryptionKey: "too-short",
	}
	if err := config.Validate(); err == nil {
		t.Fatal("weak token encryption key was accepted")
	}
}

func TestWechatOpenPlatformConfigRejectsMalformedEncodingAESKey(t *testing.T) {
	config := WechatOpenPlatformConfig{
		ComponentAppID: "component-app", ComponentAppSecret: "component-secret",
		MessageToken: "callback-token", EncodingAESKey: "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!",
		TokenEncryptionKey: "0123456789abcdef0123456789abcdef",
	}
	if err := config.Validate(); err == nil {
		t.Fatal("malformed EncodingAESKey was accepted")
	}
}
