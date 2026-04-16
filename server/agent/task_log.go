package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskLogWriter writes human-readable agent diagnostics to a per-task log file.
// It is safe for concurrent use from multiple goroutines.
type TaskLogWriter struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	taskID  string
	started time.Time
	closed  bool
}

// NewTaskLogWriter creates the log file (and parent directories) and writes the header.
func NewTaskLogWriter(path, taskID string) (*TaskLogWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	return &TaskLogWriter{
		file:    f,
		path:    path,
		taskID:  taskID,
		started: time.Now(),
	}, nil
}

// WriteHeader writes the task metadata header block.
func (w *TaskLogWriter) WriteHeader(taskType, topic, model string, maxTurns int) {
	w.writeLine("=== Task %s started at %s ===", w.taskID, w.started.Format(time.RFC3339))
	w.writeLine("Type: %s | Topic: %q | MaxTurns: %d", taskType, topic, maxTurns)
	if model != "" {
		w.writeLine("Model: %s", model)
	}
	w.writeLine("")
}

// WriteAssistantText writes an assistant message block.
func (w *TaskLogWriter) WriteAssistantText(turn int, text string) {
	w.writeLine("[%s] [ASSISTANT] Turn %d", w.timeStr(), turn)
	// Write each line of text indented by 2 spaces.
	for _, line := range splitLines(text) {
		w.writeLine("  %s", line)
	}
}

// WriteToolUse writes a tool call block.
func (w *TaskLogWriter) WriteToolUse(turn int, toolName, inputJSON string) {
	w.writeLine("[%s] [TOOL_USE] Turn %d | tool: %s", w.timeStr(), turn, toolName)
	for _, line := range splitLines(inputJSON) {
		w.writeLine("  input: %s", line)
	}
}

// WriteToolResult writes a tool result block.
func (w *TaskLogWriter) WriteToolResult(toolUseID, content string) {
	w.writeLine("[%s] [TOOL_RESULT] tool_use_id: %s", w.timeStr(), toolUseID)
	for _, line := range splitLines(content) {
		w.writeLine("  %s", line)
	}
}

// WriteStderr writes a stderr line from the CLI.
func (w *TaskLogWriter) WriteStderr(line string) {
	w.writeLine("[%s] [STDERR] %s", w.timeStr(), line)
}

// WriteRawStdout writes a raw stdout line (JSON from Claude CLI) for real-time visibility.
func (w *TaskLogWriter) WriteRawStdout(line string) {
	w.writeLine("[%s] [RAW_STDOUT] %s", w.timeStr(), line)
}

// WriteError writes an error block.
func (w *TaskLogWriter) WriteError(msg string) {
	w.writeLine("[%s] [ERROR] %s", w.timeStr(), msg)
}

// WriteResult writes the execution summary block.
func (w *TaskLogWriter) WriteResult(success bool, durationMs, numTurns int, costUSD *float64, tokenUsage *TokenUsage) {
	status := "Success"
	if !success {
		status = "Failed"
	}
	w.writeLine("")
	w.writeLine("[%s] [RESULT] %s", w.timeStr(), status)
	w.writeLine("  Duration: %dms | Turns: %d", durationMs, numTurns)
	if costUSD != nil {
		w.writeLine("  Cost: $%.4f", *costUSD)
	}
	if tokenUsage != nil {
		w.writeLine("  Input tokens: %d | Output tokens: %d", tokenUsage.InputTokens, tokenUsage.OutputTokens)
		if tokenUsage.CacheReadTokens > 0 {
			w.writeLine("  Cache read: %d | Cache creation: %d", tokenUsage.CacheReadTokens, tokenUsage.CacheCreationTokens)
		}
	}
	w.writeLine("")
	w.writeLine("=== Task %s completed at %s ===", w.taskID, time.Now().Format(time.RFC3339))
}

// Close flushes and closes the log file. Safe to call multiple times.
func (w *TaskLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.file.Close()
}

// Path returns the log file path.
func (w *TaskLogWriter) Path() string {
	return w.path
}

func (w *TaskLogWriter) writeLine(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	fmt.Fprintf(w.file, format+"\n", args...)
}

func (w *TaskLogWriter) timeStr() string {
	return time.Now().Format("15:04:05")
}

// splitLines splits a string into lines for formatted output.
func splitLines(s string) []string {
	if len(s) == 0 {
		return nil
	}
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
