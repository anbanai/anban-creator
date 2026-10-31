package robot

import (
	"context"
	"fmt"
	"io"

	"github.com/gofiber/fiber/v3"
)

func (robot *Robot) SendFile(filename string, file io.WriterTo) error {
	return robot.SendFileContext(context.Background(), filename, file)
}

func (robot *Robot) SendFileContext(ctx context.Context, filename string, file io.WriterTo) error {
	res, err := robot.UploadMediaContext(ctx, filename, func(w io.Writer) error {
		if _, err := file.WriteTo(w); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("upload media error: %w", err)
	}

	msg := fiber.Map{"msgtype": "file", "file": fiber.Map{"media_id": res.MediaID}}
	if _, err := robot.SendMessageContext(ctx, msg); err != nil {
		return fmt.Errorf("send message error: %w", err)
	}
	return nil
}
