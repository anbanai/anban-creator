package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/app/image"
)

func TestDesignerGenerationErrorMessageHidesProviderInternals(t *testing.T) {
	err := &image.GenerateError{
		Provider: "OpenAI",
		Code:     "url_download_error",
		Message:  "OpenAI 图片接口返回了 URL，但下载失败: https://files.example.com/generated.png",
		HintMsg:  `RevisedPrompt=""`,
		Original: errors.New("403 Forbidden"),
	}

	got := designerGenerationUserError(err)
	if got == "" {
		t.Fatal("designerGenerationUserError returned empty message")
	}
	for _, leaked := range []string{"https://files.example.com", "RevisedPrompt", "[OpenAI]", "403 Forbidden"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("message %q leaked internal detail %q", got, leaked)
		}
	}
	if !strings.Contains(got, "保存") {
		t.Fatalf("message = %q, want save failure guidance", got)
	}
}

func TestDesignerGenerationErrorMessageCleansStructuredLogs(t *testing.T) {
	raw := `{"level":"error","error":"[OpenAI] OpenAI 图片接口返回了 URL，但下载失败: https://files.example.com/a.png\n提示: RevisedPrompt=\"\"","message":"designer: image generation failed"}<br/>{"level":"error","error":"same"}`

	got := designerGenerationUserError(errors.New(raw))
	if got == "" {
		t.Fatal("designerGenerationUserError returned empty message")
	}
	for _, leaked := range []string{"{\"level\"", "<br/>", "https://files.example.com", "RevisedPrompt"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("message %q leaked internal detail %q", got, leaked)
		}
	}
}

func TestDesignerGenerationErrorMessageUsesCapabilityTerminology(t *testing.T) {
	got := designerGenerationUserError(&image.GenerateError{Code: "endpoint_protocol"})
	if got != "图像能力配置异常，请联系管理员" {
		t.Fatalf("message = %q, want capability configuration guidance", got)
	}
	if strings.Contains(got, "模型") {
		t.Fatalf("message = %q, must not expose model terminology", got)
	}
}
