package service

import "context"

// ProgressEvent represents a task progress or status update published via Redis
// pub/sub (or the polling fallback).
type ProgressEvent struct {
	TaskID     string `json:"task_id"`
	Progress   int    `json:"progress"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	IsComplete bool   `json:"is_complete,omitempty"`
}

// TaskProgressNotifier is the interface for publishing and subscribing to
// task progress events. The primary implementation uses Redis pub/sub; a
// polling fallback is used when Redis is unavailable.
type TaskProgressNotifier interface {
	// Subscribe opens a channel that receives progress events for the given
	// task. The returned cleanup function MUST be called when the subscriber
	// is done (e.g. when the SSE connection closes) to release resources.
	Subscribe(ctx context.Context, taskID string) (<-chan *ProgressEvent, func(), error)

	// Publish sends a progress event to all subscribers of the given task.
	Publish(ctx context.Context, event *ProgressEvent) error
}
