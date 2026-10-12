package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecRuntimeFactoryPassesInteractiveCredentialsToBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	output := filepath.Join(t.TempDir(), "env.txt")
	script := "printf '%s\\n%s\\n%s\\n%s\\n' \"$ANBAN_INTERACTIVE_SESSION_ID\" \"$ANBAN_INTERACTIVE_RUNTIME_TOKEN\" \"$ANBAN_INTERACTIVE_DSH_PATCH\" \"$ANBAN_INTERACTIVE_DSH_HOME\" > \"$1\"; read line"
	factory := ExecRuntimeFactory{
		Command:  "sh",
		Args:     []string{"-c", script, "bridge", output},
		DSHPatch: "/app/harness/dsh/project-creator.patch.yml",
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	deadline := time.Now().Add(time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(output)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "session-1" || lines[1] != "runtime-secret" || lines[2] != factory.DSHPatch || lines[3] == "" {
		t.Fatalf("bridge environment = %#v, want session/token/patch/home", lines)
	}
	if _, err := os.Stat(lines[3]); err != nil {
		t.Fatalf("interactive DSH home does not exist while runtime is live: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(lines[3]); !os.IsNotExist(err) {
		t.Fatalf("interactive DSH home still exists after runtime close: err=%v", err)
	}
}

func TestExecRuntimeFactoryPassesOnlyConfiguredProviderCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	t.Setenv("ANBAN_DATABASE_DSN", "mysql://root:server-secret@db/anban")
	t.Setenv("ANBAN_JWT_SECRET_KEY", "server-jwt-secret")
	t.Setenv("DEEPSEEK_API_KEY", "provider-secret")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "unselected-provider-secret")
	output := filepath.Join(t.TempDir(), "env.txt")
	script := "printf '%s\\n%s\\n%s\\n%s\\n%s\\n' \"${ANBAN_DATABASE_DSN-}\" \"${ANBAN_JWT_SECRET_KEY-}\" \"${DEEPSEEK_API_KEY-}\" \"${ANTHROPIC_AUTH_TOKEN-}\" \"$HOME\" > \"$1\"; read line"
	factory := ExecRuntimeFactory{
		Command:   "sh",
		Args:      []string{"-c", script, "bridge", output},
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	deadline := time.Now().Add(time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(output)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 5 || lines[0] != "" || lines[1] != "" || lines[2] != "provider-secret" || lines[3] != "" || lines[4] == "" {
		t.Fatalf("bridge environment = %#v, want only configured provider credential and isolated home", lines)
	}
}

func TestExecRuntimeFactoryDoesNotMapServerDeepSeekCredentialForDSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	t.Setenv("ANBAN_DEEPSEEK_API_KEY", "server-provider-secret")
	_ = os.Unsetenv("DEEPSEEK_API_KEY")
	output := filepath.Join(t.TempDir(), "env.txt")
	script := "printf '%s\\n' \"${DEEPSEEK_API_KEY-}\" > \"$1\"; read line"
	factory := ExecRuntimeFactory{
		Command:   "sh",
		Args:      []string{"-c", script, "bridge", output},
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	deadline := time.Now().Add(time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(output)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := strings.TrimSpace(string(data)); got != "" {
		t.Fatalf("DSH credential unexpectedly mapped from Server variable: %q", got)
	}
}

func TestExecRuntimeFactoryPassesNodeExtraCACertsToBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	t.Setenv("NODE_EXTRA_CA_CERTS", "/var/run/secrets/anban-internal-ca/ca.crt")
	output := filepath.Join(t.TempDir(), "env.txt")
	script := "printf '%s\\n' \"${NODE_EXTRA_CA_CERTS-}\" > \"$1\"; read line"
	factory := ExecRuntimeFactory{
		Command:   "sh",
		Args:      []string{"-c", script, "bridge", output},
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	deadline := time.Now().Add(time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(output)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := strings.TrimSpace(string(data)); got != "/var/run/secrets/anban-internal-ca/ca.crt" {
		t.Fatalf("NODE_EXTRA_CA_CERTS = %q, want mounted internal CA path", got)
	}
}

func TestExecRuntimeFactoryUsesAnIsolatedHomePerSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	makeRuntime := func(sessionID string) (Runtime, string) {
		output := filepath.Join(t.TempDir(), "env.txt")
		factory := ExecRuntimeFactory{
			Command: "sh",
			Args:    []string{"-c", "printf '%s\\n' \"$ANBAN_INTERACTIVE_DSH_HOME\" > \"$1\"; read line", "bridge", output},
		}
		rt, err := factory.Start(context.Background(), sessionID, "project-creator", "runtime-secret", nil)
		if err != nil {
			t.Fatalf("Start(%s) error = %v", sessionID, err)
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if data, readErr := os.ReadFile(output); readErr == nil && strings.TrimSpace(string(data)) != "" {
				return rt, strings.TrimSpace(string(data))
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("runtime %s did not report its DSH home", sessionID)
		return nil, ""
	}
	first, firstHome := makeRuntime("session-1")
	second, secondHome := makeRuntime("session-2")
	if firstHome == secondHome {
		t.Fatalf("sessions share DSH home %q", firstHome)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	for _, home := range []string{firstHome, secondHome} {
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("DSH home %q still exists after close: err=%v", home, err)
		}
	}
}

func TestExecRuntimeCommandWaitsForBridgeResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	factory := ExecRuntimeFactory{
		Command: "sh",
		Args:    []string{"-c", `read line; sleep 0.05; printf '%s\n' '{"id":1,"result":{"ok":true}}'; sleep 1`, "bridge"},
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	started := time.Now()
	if err := rt.Command(context.Background(), "session/start", map[string]any{}); err != nil {
		t.Fatalf("Command() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("Command() returned before Bridge response: elapsed=%s", elapsed)
	}
}

func TestExecRuntimeCommandReturnsBridgeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	factory := ExecRuntimeFactory{
		Command: "sh",
		Args:    []string{"-c", `read line; printf '%s\n' '{"id":1,"error":"bridge rejected command"}'`, "bridge"},
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	err = rt.Command(context.Background(), "session/start", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "bridge rejected command") {
		t.Fatalf("Command() error = %v, want Bridge error", err)
	}
}

func TestExecRuntimeCommandHonorsContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	factory := ExecRuntimeFactory{
		Command: "sh",
		Args:    []string{"-c", `read line; sleep 1`, "bridge"},
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = rt.Command(ctx, "session/start", map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command() error = %v, want context deadline", err)
	}
}

func TestExecRuntimeCommandIncludesProcessExitDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	factory := ExecRuntimeFactory{
		Command: "sh",
		Args:    []string{"-c", `read line; printf '%s\n' 'fatal bridge initialization' >&2; exit 7`, "bridge"},
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if err := rt.Command(context.Background(), "session/start", map[string]any{}); err == nil || !strings.Contains(err.Error(), "fatal bridge initialization") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("Command() error = %v, want process stderr and exit status", err)
	}
}

func TestExecRuntimeCommandRedactsSecretsFromDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bridge process test uses a POSIX shell")
	}
	t.Setenv("DEEPSEEK_API_KEY", "provider-secret")
	factory := ExecRuntimeFactory{
		Command:   "sh",
		Args:      []string{"-c", `read line; printf '%s %s\n' "$ANBAN_INTERACTIVE_RUNTIME_TOKEN" "$DEEPSEEK_API_KEY" >&2; exit 7`, "bridge"},
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	rt, err := factory.Start(context.Background(), "session-1", "project-creator", "runtime-secret", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if err := rt.Command(context.Background(), "session/start", map[string]any{}); err == nil || strings.Contains(err.Error(), "runtime-secret") || strings.Contains(err.Error(), "provider-secret") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("Command() error = %v, want redacted diagnostic", err)
	}
}
