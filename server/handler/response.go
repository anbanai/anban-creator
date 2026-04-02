package handler

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
)

// Response is the standard JSON envelope for all API responses.
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// Success returns a JSON response with code 0 and the given data.
func Success(c fiber.Ctx, data interface{}) error {
	return c.JSON(Response{Code: 0, Msg: "success", Data: data})
}

// Error returns a JSON error response with the given HTTP status and message.
func Error(c fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(Response{Code: status*100, Msg: msg})
}

// Errorf returns a JSON error response with a formatted message.
func Errorf(c fiber.Ctx, status int, format string, args ...interface{}) error {
	return c.Status(status).JSON(Response{Code: status*100, Msg: fmt.Sprintf(format, args...)})
}
