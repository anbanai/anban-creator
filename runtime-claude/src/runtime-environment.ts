// Managed runtime environment contract.
//
// The Server is the only authority on which values an Agent receives: it
// resolves them per Agent Pack from operator configuration. The runtime never
// gates a value on business identity (task type or Agent Pack); it only
// enforces these transport invariants before a value can reach the Agent SDK,
// an MCP server, or a child process.
//
// Keep these rules in sync with server/model/runtime_environment.go. Both sides
// validate independently so a Server defect cannot widen the Agent's
// privileges and so adding an Agent Pack never requires changing runtime code.

export const RUNTIME_ENVIRONMENT_LIMITS = {
  entries: 128,
  keyBytes: 255,
  valueBytes: 16 << 10,
} as const;

const KEY_PATTERN = /^[A-Z][A-Z0-9_]*$/;

/** Prefixes owned by the control plane, the model provider, or the host toolchain. */
const RESERVED_PREFIXES = [
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
] as const;

/** Names that would let a configured value hijack process or shell startup. */
const FORBIDDEN_KEYS = new Set([
  "PATH",
  "HOME",
  "SHELL",
  "PWD",
  "OLDPWD",
  "TMPDIR",
  "TMP",
  "TEMP",
  "ENV",
  "BASH_ENV",
  "ZDOTDIR",
  "IFS",
  "GOOGLE_APPLICATION_CREDENTIALS",
]);

/**
 * Validates a bootstrap environment map and returns a plain copy.
 * Throws on anything the Server must never send.
 */
export function validateRuntimeEnvironment(input: unknown): Record<string, string> {
  if (input === undefined || input === null) return {};
  if (typeof input !== "object" || Array.isArray(input)) throw new Error("bootstrap environment is invalid");
  const entries = Object.entries(input as Record<string, unknown>);
  if (entries.length > RUNTIME_ENVIRONMENT_LIMITS.entries) {
    throw new Error(`bootstrap environment has ${entries.length} entries, the limit is ${RUNTIME_ENVIRONMENT_LIMITS.entries}`);
  }
  const environment: Record<string, string> = {};
  for (const [key, value] of entries) {
    if (!KEY_PATTERN.test(key) || Buffer.byteLength(key) > RUNTIME_ENVIRONMENT_LIMITS.keyBytes) {
      throw new Error(`bootstrap environment key ${JSON.stringify(key)} must match [A-Z][A-Z0-9_]*`);
    }
    const reserved = RESERVED_PREFIXES.find((prefix) => key.startsWith(prefix));
    if (reserved) throw new Error(`bootstrap environment key ${key} uses the reserved prefix ${reserved}`);
    if (FORBIDDEN_KEYS.has(key)) throw new Error(`bootstrap environment key ${key} is reserved for the runtime process`);
    // ASCII space and tab only, matching the Go validator byte for byte.
    if (typeof value !== "string" || value.length === 0 || value !== value.replace(/^[ \t]+|[ \t]+$/, "")) {
      throw new Error(`bootstrap environment value for ${key} must be non-empty and must not be padded with whitespace`);
    }
    if (/[\x00\r\n]/.test(value)) throw new Error(`bootstrap environment value for ${key} contains a control character`);
    if (Buffer.byteLength(value) > RUNTIME_ENVIRONMENT_LIMITS.valueBytes) {
      throw new Error(`bootstrap environment value for ${key} exceeds ${RUNTIME_ENVIRONMENT_LIMITS.valueBytes} bytes`);
    }
    environment[key] = value;
  }
  return environment;
}

/** A RedFox key is the Server's opt-in for the managed RedFox research MCP. */
export function hasRedFoxCredentials(environment: Record<string, string> | undefined): boolean {
  const key = environment?.REDFOX_API_KEY;
  return typeof key === "string" && key.trim().length > 0;
}
