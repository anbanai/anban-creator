package robot

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"
)

// NewMarkdownMessage builds a WeCom markdown webhook payload.
func NewMarkdownMessage(markdown string) fiber.Map {
	return fiber.Map{"msgtype": "markdown", "markdown": fiber.Map{"content": markdown}}
}

func (robot *Robot) SendMarkdown(markdown string) error {
	return robot.SendMarkdownContext(context.Background(), markdown)
}

func (robot *Robot) SendMarkdownContext(ctx context.Context, markdown string) error {
	if _, err := robot.SendMessageContext(ctx, NewMarkdownMessage(markdown)); err != nil {
		return fmt.Errorf("failed to send markdown message, error: %w", err)
	}
	return nil
}
