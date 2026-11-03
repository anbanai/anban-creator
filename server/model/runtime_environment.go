package model

import (
	"fmt"
	"regexp"
	"strings"
)

// Managed runtime environment contract.
//
// The Server is the only authority on which values an Agent receives: it
// resolves them per Agent Pack from operator configuration. The runtime never
// gates a value on business identity (task type or Agent Pack); it enforces
// these transport invariants before a value can reach the Agent SDK, an MCP
// server, or a child process. Both sides validate independently so a Server
// defect cannot widen the Agent's privileges and so adding an Agent Pack never
// requires changing runtime code.
//
// Keep these rules in sync with runtime-claude/src/runtime-environment.ts and
// the runtime image contract tests.
const (
	MaxRuntimeEnvironmentEntries    = 128
	MaxRuntimeEnvironmentKeyBytes   = 255
	MaxRuntimeEnvironmentValueBytes = 16 << 10
)

var runtimeEnvironmentKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// reservedRuntimeEnvironmentPrefixes belong to the Anban control plane, the
// model provider, the host toolchain, or the POSIX process. An Agent Pack must
// never be able to override them.
var reservedRuntimeEnvironmentPrefixes = []string{
	"ANBAN_",
	"ANTHROPIC_",
	"CLAUDE_",
	"CODEX_",
	"NODE_",
	"BUN_",
	"npm_",
	"NPM_",
	"LD_",
	"DYLD_",
	"PYTHON",
	"GIT_",
	"SSH_",
}

// forbiddenRuntimeEnvironmentKeys would let a configured value hijack command
// resolution, shell startup, temporary files, or credential discovery.
var forbiddenRuntimeEnvironmentKeys = map[string]struct{}{
	"PATH":                           {},
	"HOME":                           {},
	"SHELL":                          {},
	"PWD":                            {},
	"OLDPWD":                         {},
	"TMPDIR":                         {},
	"TMP":                            {},
	"TEMP":                           {},
	"ENV":                            {},
	"BASH_ENV":                       {},
	"ZDOTDIR":                        {},
	"IFS":                            {},
	"GOOGLE_APPLICATION_CREDENTIALS": {},
}

// NormalizeRuntimeEnvironment returns a copy without unset entries. Operator
// configuration routinely materializes unset variables as empty strings, so an
// empty value means "not configured" rather than "configured as empty".
func NormalizeRuntimeEnvironment(env map[string]string) map[string]string {
	normalized := make(map[string]string, len(env))
	for key, value := range env {
		if strings.TrimSpace(value) == "" {
			continue
		}
		normalized[key] = value
	}
	return normalized
}

// ValidateRuntimeEnvironment enforces the transport invariants for a normalized
// environment map. Scope is a human-readable owner such as an Agent Pack ID or
// a configuration section, and appears only in the error message.
func ValidateRuntimeEnvironment(scope string, env map[string]string) error {
	if len(env) > MaxRuntimeEnvironmentEntries {
		return fmt.Errorf("%s environment has %d entries, the limit is %d", scope, len(env), MaxRuntimeEnvironmentEntries)
	}
	for key, value := range env {
		if err := validateRuntimeEnvironmentEntry(key, value); err != nil {
			return fmt.Errorf("%s environment: %w", scope, err)
		}
	}
	return nil
}

func validateRuntimeEnvironmentEntry(key, value string) error {
	if !runtimeEnvironmentKeyPattern.MatchString(key) || len(key) > MaxRuntimeEnvironmentKeyBytes {
		return fmt.Errorf("key %q must match [A-Z][A-Z0-9_]* and be at most %d bytes", key, MaxRuntimeEnvironmentKeyBytes)
	}
	for _, prefix := range reservedRuntimeEnvironmentPrefixes {
		if strings.HasPrefix(key, prefix) {
			return fmt.Errorf("key %q uses the reserved prefix %q", key, prefix)
		}
	}
	if _, forbidden := forbiddenRuntimeEnvironmentKeys[key]; forbidden {
		return fmt.Errorf("key %q is reserved for the runtime process", key)
	}
	if value == "" || value != strings.Trim(value, " \t") {
		return fmt.Errorf("value for %q must be non-empty and must not be padded with whitespace", key)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("value for %q contains a control character", key)
	}
	if len(value) > MaxRuntimeEnvironmentValueBytes {
		return fmt.Errorf("value for %q exceeds %d bytes", key, MaxRuntimeEnvironmentValueBytes)
	}
	return nil
}
