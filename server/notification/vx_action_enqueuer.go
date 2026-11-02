package notification

import "context"

// VxActionEnqueuer 把"通过 GeWeChat 设备转发一条文本消息"的能力抽象为接口，
// 供微信渠道（联系人/群）sender 调用。由 handler/schedule.Schedule 实现，在 main.go 注入，
// 以避免 notification 与 handler/schedule 形成循环依赖（schedule 已依赖 notification）。
type VxActionEnqueuer interface {
	EnqueueVxActionText(ctx context.Context, deviceID, deviceVx, receiver, content string) error
}
