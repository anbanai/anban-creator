import { HarnessClient, type HarnessNotification } from "@deepseek-ai/dsh-sdk-client";
import { existsSync, mkdirSync, statSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline";
import { basename, resolve } from "node:path";

type JsonRecord = Record<string, unknown>;
const DSH_BRIDGE_PROVIDER_API_KEY_ENV = "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY";
const PROVIDER_REGISTRATION_RETRIES = 20;
const PROVIDER_REGISTRATION_RETRY_MS = 50;
const RUNTIME_HEARTBEAT_INTERVAL_MS = 30_000;
const DSH_RETRY_POLICY = [
  "  retryPolicy:",
  "    mode: normal",
  "    maxRetries: 2",
  "    retryableCodes:",
  "      - EMPTY_RESPONSE",
  "      - RATE_LIMIT",
  "      - SERVER",
  "      - TIMEOUT",
  "      - TRANSPORT",
  "      - MALFORMED_RESPONSE",
  "    backoff:",
  "      initialDelayMs: 500",
  "      maxDelayMs: 5000",
  "      jitterRatio: 0.1",
].join("\n");

export interface InteractiveCommand {
  id?: string | number;
  method: "session/start" | "session/prompt" | "session/answer" | "shutdown";
  params: JsonRecord;
}

export interface StableBridgeEvent {
  event: string;
  data?: unknown;
}

interface InteractiveRuntimeCapabilities {
  sessionStart: true;
  sessionAnswer: true;
}

export const INTERACTIVE_PRESET_ID = "project-creator";
export const INTERACTIVE_PRESET_IDS = ["project-creator", "creation-planner", "postmortem-review"] as const;
export type InteractivePresetId = typeof INTERACTIVE_PRESET_IDS[number];
const DSH_BRIDGE_PRESET_ID_ENV = "ANBAN_DSH_BRIDGE_PRESET_ID";

export function validateInteractivePresetId(value: string): InteractivePresetId {
  const presetId = value.trim();
  if (!INTERACTIVE_PRESET_IDS.includes(presetId as InteractivePresetId)) {
    throw new Error(`unsupported interactive DSH preset: ${presetId || "(empty)"}`);
  }
  return presetId as InteractivePresetId;
}

export interface InteractiveRuntimeClient {
  start(): void;
  initialize(params: {
    cwd: string;
    provider: string;
    model: string;
  }): Promise<unknown>;
  request(method: string, params?: object): Promise<unknown>;
  subscribeSessionTree(sessionId: string): { next(): Promise<HarnessNotification>; close(): void };
  close(): Promise<void>;
}

export interface InteractiveRouteConfig {
  provider: string;
  model: string;
  api: string;
  baseURL: string;
}

export function parseInteractiveRouteArgs(argv = process.argv.slice(2)): InteractiveRouteConfig {
  const supported = new Set(["--provider", "--model", "--route-api", "--base-url"]);
  const values = new Map<string, string>();
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (!key.startsWith("--")) throw new Error(`unsupported DSH bridge argument: ${key}`);
    if (!supported.has(key)) throw new Error(`unsupported DSH bridge argument: ${key}`);
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--") || value.trim() === "") throw new Error(`${key} requires a value`);
    values.set(key, value.trim());
    index += 1;
  }
  const read = (key: string): string => {
    const value = values.get(key)?.trim() ?? "";
    if (!value) throw new Error(`${key} is required`);
    return value;
  };
  const route = {
    provider: read("--provider"), model: read("--model"), api: read("--route-api"),
    baseURL: read("--base-url"),
  };
  if (route.provider !== "deepseek-official" && route.provider !== "anthropic") {
    throw new Error(`unsupported DSH provider: ${route.provider}`);
  }
  return route;
}

function yamlQuote(value: string): string {
  return JSON.stringify(value);
}

export function writeDshRouteSettings(dshHome: string, route: InteractiveRouteConfig): string {
  if (route.provider !== "deepseek-official" && route.provider !== "anthropic") {
    throw new Error(`unsupported DSH provider: ${route.provider}`);
  }
  mkdirSync(dshHome, { recursive: true, mode: 0o700 });
  const model = `        - id: ${yamlQuote(route.model)}\n          contextWindow: 262144\n          maxTokens: 32768`;
  const body = route.provider === "deepseek-official"
    ? ["llm-deepseek:", `  apiKeyEnv: ${yamlQuote(DSH_BRIDGE_PROVIDER_API_KEY_ENV)}`, `  baseURL: ${yamlQuote(route.baseURL)}`, DSH_RETRY_POLICY, "  models:", model].join("\n") + "\n"
    : ["llm-pi-ai:", "  providers:", `    ${route.provider}:`, `      apiKeyEnv: ${yamlQuote(DSH_BRIDGE_PROVIDER_API_KEY_ENV)}`, `      api: ${yamlQuote(route.api)}`, `      baseURL: ${yamlQuote(route.baseURL)}`, DSH_RETRY_POLICY.replace(/^/gm, "    "), "      models:", model].join("\n") + "\n";
  const path = resolve(dshHome, "settings.yaml");
  writeFileSync(path, body, { encoding: "utf8", mode: 0o600 });
  return path;
}

async function initializeRuntimeClient(
  client: InteractiveRuntimeClient,
  route: InteractiveRouteConfig,
): Promise<unknown> {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await client.initialize({ cwd: process.cwd(), provider: route.provider, model: route.model });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (!message.includes(`no adapter registered for provider "${route.provider}"`) || attempt >= PROVIDER_REGISTRATION_RETRIES) {
        throw error;
      }
      await new Promise<void>((resolveDelay) => setTimeout(resolveDelay, PROVIDER_REGISTRATION_RETRY_MS));
    }
  }
}

/** Fail closed when an older DSH runtime cannot drive interactive sessions. */
export function assertInteractiveRuntimeCapabilities(value: unknown): asserts value is {
  capabilities: InteractiveRuntimeCapabilities;
} {
  if (!isRecord(value) || !isRecord(value.capabilities)) {
    throw new Error("interactive runtime does not support session/start");
  }
  if (value.capabilities.sessionStart !== true) {
    throw new Error("interactive runtime does not support session/start");
  }
  if (value.capabilities.sessionAnswer !== true) {
    throw new Error("interactive runtime does not support session/answer");
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function redactInteractiveSecrets(
  message: string,
  token = process.env.ANBAN_DSH_BRIDGE_RUNTIME_TOKEN ?? "",
  additionalSecrets: readonly string[] = [],
): string {
  let redacted = message;
  for (const secret of [token, ...additionalSecrets]) {
    if (secret) redacted = redacted.split(secret).join("[redacted]");
  }
  return redacted;
}

export function resolveInteractivePatchPath(
  raw = process.env.ANBAN_DSH_BRIDGE_PATCH,
  cwd = process.cwd(),
  presetId: string = INTERACTIVE_PRESET_ID,
): string {
  const value = raw?.trim() ?? "";
  if (!value) throw new Error("ANBAN_DSH_BRIDGE_PATCH is required");
  const selectedPreset = validateInteractivePresetId(presetId);
  const path = resolve(cwd, value);
  if (!existsSync(path) || !statSync(path).isFile()) {
    throw new Error(`interactive DSH patch does not exist: ${path}`);
  }
  const expectedPatch = `${selectedPreset}.patch.yml`;
  if (basename(path) !== expectedPatch) {
    throw new Error(`interactive DSH patch ${basename(path)} does not match preset ${selectedPreset}`);
  }
  return path;
}

export function parseInteractiveCommand(line: string): InteractiveCommand {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    throw new Error("DSH bridge input is not valid JSON");
  }
  if (!isRecord(value) || typeof value.method !== "string") {
    throw new Error("DSH bridge command requires a method");
  }
  const methods = new Set(["session/start", "session/prompt", "session/answer", "shutdown"]);
  if (!methods.has(value.method)) {
    throw new Error(`unsupported DSH bridge method: ${value.method}`);
  }
  if (value.id !== undefined && typeof value.id !== "string" && typeof value.id !== "number") {
    throw new Error("DSH bridge request id is invalid");
  }
  const params = value.params === undefined ? {} : value.params;
  if (!isRecord(params)) throw new Error("DSH bridge params must be an object");
  return { id: value.id as string | number | undefined, method: value.method as InteractiveCommand["method"], params };
}

function textFromContent(content: unknown): string {
  if (!Array.isArray(content)) return "";
  return content
    .filter(isRecord)
    .filter((block) => block.type === "text" && typeof block.text === "string")
    .map((block) => block.text as string)
    .join("");
}

function normalizeQuestion(value: unknown): unknown {
  if (!isRecord(value)) return value;
  const normalized: JsonRecord = { ...value };
  if (normalized.multiSelect === undefined && typeof normalized.multi_select === "boolean") {
    normalized.multiSelect = normalized.multi_select;
    delete normalized.multi_select;
  }
  return normalized;
}

export function mapHarnessNotification(notification: HarnessNotification, rootSessionId?: string): StableBridgeEvent[] {
  const params = notification.params;
  if (notification.method === "session.ready") {
    if (rootSessionId !== undefined && params.sessionId !== rootSessionId) return [];
    return [{
      event: "session.ready",
      data: {
        session_id: params.sessionId,
        preset_id: params.presetId,
      },
    }];
  }
  if (notification.method === "session.status") {
    if (rootSessionId !== undefined && params.sessionId !== rootSessionId) return [];
    return [{ event: "session.status", data: { session_id: params.sessionId, status: params.status } }];
  }
  if (notification.method !== "session.event") return [];
  const sessionId = typeof params.sessionId === "string" ? params.sessionId : undefined;
  const event = isRecord(params.event) ? params.event : undefined;
  if (!event || typeof event.type !== "string") return [];
  if (rootSessionId !== undefined && sessionId !== rootSessionId && event.type !== "tool/call" && event.type !== "tool/result") return [];
  const data = isRecord(event.data) ? event.data : {};
  if (event.type === "turn/start") {
    const turn = typeof data.turn === "number" ? data.turn : undefined;
    return [{
      event: "turn.start",
      data: { session_id: sessionId, ...(turn === undefined ? {} : { turn }) },
    }];
  }
  if (event.type === "turn/end") {
    const reason = isRecord(data.reason) ? data.reason : {};
    const turn = typeof data.turn === "number" ? data.turn : undefined;
    if (reason.kind === "error") {
      const failure = isRecord(reason.error) ? reason.error : {};
      const code = typeof failure.code === "string" && failure.code.trim() !== "" ? failure.code : "UNKNOWN";
      const message = typeof failure.message === "string" && failure.message.trim() !== ""
        ? failure.message
        : "interactive agent turn failed";
      const status = typeof failure.status === "number" && Number.isInteger(failure.status)
        && failure.status >= 100 && failure.status <= 599 ? failure.status : undefined;
      const requestId = typeof failure.requestId === "string" && failure.requestId.trim() !== ""
        ? failure.requestId
        : undefined;
      return [{
        event: "session.error",
        data: {
          session_id: sessionId,
          status: "runtime_failed",
          code,
          message,
          ...(status === undefined ? {} : { provider_status: status }),
          ...(requestId === undefined ? {} : { provider_request_id: requestId }),
          ...(turn === undefined ? {} : { turn }),
        },
      }];
    }
    return [{
      event: "turn.end",
      data: {
        session_id: sessionId,
        ...(turn === undefined ? {} : { turn }),
        reason,
      },
    }];
  }
  if (event.type === "assistant/message") {
    const message = isRecord(data.message) ? data.message : {};
    const text = textFromContent(message.content);
    if (!text) return [];
    return [{ event: "assistant.message", data: { session_id: sessionId, text } }];
  }
  if (event.type === "tool/call") {
    const name = typeof data.name === "string" ? data.name.trim() : "";
    if (name.toLowerCase() !== "ask_user_question") {
      if (!name) return [];
      return [{
        event: "agent.activity",
        data: {
          session_id: sessionId,
          ...(typeof data.callId === "string" && data.callId.trim() !== "" ? { call_id: data.callId } : {}),
          name: name.replace(/^mcp__(?:anban|creator)__/, "").slice(0, 128),
        },
      }];
    }
    if (rootSessionId !== undefined && sessionId !== rootSessionId) {
      return [{
        event: "session.error",
        data: {
          session_id: sessionId,
          message: "AskUserQuestion from a child Agent is not allowed",
        },
      }];
    }
    let args: JsonRecord = {};
    if (typeof data.arguments === "string") {
      try {
        const parsed: unknown = JSON.parse(data.arguments);
        if (isRecord(parsed)) args = parsed;
      } catch {
        return [{ event: "session.error", data: { session_id: sessionId, message: "AskUserQuestion arguments are invalid" } }];
      }
    }
    const questions = Array.isArray(args.questions)
      ? args.questions.map(normalizeQuestion)
      : Array.isArray(args.items) ? args.items.map(normalizeQuestion) : [];
    const callId = typeof data.callId === "string" ? data.callId : data.call_id;
    if (typeof callId !== "string" || callId.trim() === "") {
      return [{ event: "session.error", data: { session_id: sessionId, message: "AskUserQuestion call id is missing" } }];
    }
    return [{
      event: "question.opened",
      data: {
        session_id: sessionId,
        call_id: callId,
        questions,
      },
    }];
  }
  if (event.type === "tool/result") {
    const message = isRecord(data.message) ? data.message : {};
    const callId = typeof message.toolCallId === "string" ? message.toolCallId.trim() : "";
    if (!callId) return [];
    const content = textFromContent(message.content);
    const error = isRecord(data.error) ? data.error : {};
    const code = typeof error.code === "string" && error.code.trim() !== "" ? error.code : undefined;
    const turn = typeof data.turn === "number" ? data.turn : undefined;
    return [{
      event: "tool.result",
      data: {
        session_id: sessionId,
        ...(turn === undefined ? {} : { turn }),
        call_id: callId,
        name: "",
        ok: message.isError !== true,
        message: content,
        ...(code === undefined ? {} : { code }),
      },
    }];
  }
  return [];
}

export class DshBridge {
  private client: InteractiveRuntimeClient | undefined;
  private subscription: { next(): Promise<HarnessNotification>; close(): void } | undefined;
  private runtimeReady: Promise<void> | undefined;
  private heartbeatTimer: ReturnType<typeof setInterval> | undefined;
  private closeTask: Promise<void> | undefined;
  private commandQueue: Promise<unknown> = Promise.resolve();
  private closed = false;
  private readonly route: InteractiveRouteConfig;
  private readonly presetId: string;

  constructor(
    private readonly write: (event: StableBridgeEvent) => void,
    private readonly clientFactory?: () => InteractiveRuntimeClient,
    private readonly rootSessionId = process.env.ANBAN_DSH_BRIDGE_SESSION_ID?.trim() ?? "",
    route?: InteractiveRouteConfig,
    presetId = process.env[DSH_BRIDGE_PRESET_ID_ENV]?.trim() ?? (clientFactory ? INTERACTIVE_PRESET_ID : ""),
  ) {
    this.presetId = presetId.trim();
    validateInteractivePresetId(this.presetId);
    this.route = route ?? (clientFactory ? {
      provider: "deepseek-official", model: "deepseek-flash", api: "deepseek-messages",
      baseURL: "https://api.deepseek.com/anthropic",
    } : parseInteractiveRouteArgs());
  }

  redact(message: string): string {
    const providerSecret = process.env[DSH_BRIDGE_PROVIDER_API_KEY_ENV]?.trim() ?? "";
    return redactInteractiveSecrets(message, process.env.ANBAN_DSH_BRIDGE_RUNTIME_TOKEN ?? "", [providerSecret]);
  }

  private redactEvent(event: StableBridgeEvent): StableBridgeEvent {
    if (!isRecord(event.data) || typeof event.data.message !== "string") return event;
    return { ...event, data: { ...event.data, message: this.redact(event.data.message) } };
  }

  private async ensureRuntime(): Promise<void> {
    if (this.closed) throw new Error("DSH bridge is closed");
    if (this.runtimeReady) return this.runtimeReady;
    this.runtimeReady = (async () => {
      const presetId = validateInteractivePresetId(this.presetId);
      const route = this.route;
      const client = this.clientFactory?.() ?? (() => {
        const dshHome = process.env.ANBAN_DSH_BRIDGE_HOME?.trim();
        if (!dshHome) throw new Error("ANBAN_DSH_BRIDGE_HOME is required");
        const patchPath = resolveInteractivePatchPath(process.env.ANBAN_DSH_BRIDGE_PATCH, process.cwd(), presetId);
        writeDshRouteSettings(dshHome, route);
        return new HarnessClient({
          dshHome,
          profile: "sdk",
          patches: [patchPath],
          processCwd: process.cwd(),
          env: process.env,
        });
      })();
      if (this.closed) {
        await client.close();
        throw new Error("DSH bridge is closed");
      }
      this.client = client;
      client.start();
      const initializeResult = await initializeRuntimeClient(client, route);
      assertInteractiveRuntimeCapabilities(initializeResult);
      if (!this.rootSessionId) throw new Error("ANBAN_DSH_BRIDGE_SESSION_ID is required");
      this.subscription = client.subscribeSessionTree(this.rootSessionId);
      void this.forwardNotifications();
      this.write({ event: "session.status", data: { status: "starting" } });
      this.write({ event: "session.heartbeat", data: {} });
      this.heartbeatTimer = setInterval(() => {
        if (!this.closed) this.write({ event: "session.heartbeat", data: {} });
      }, RUNTIME_HEARTBEAT_INTERVAL_MS);
    })().catch(async (error: unknown) => {
      this.write({ event: "session.error", data: { message: this.redact(error instanceof Error ? error.message : String(error)) } });
      await this.closeRuntime();
      throw error;
    });
    return this.runtimeReady;
  }

  private async forwardNotifications(): Promise<void> {
    const subscription = this.subscription;
    if (!subscription) return;
    try {
      for (;;) {
        const notification = await subscription.next();
        for (const event of mapHarnessNotification(notification, this.rootSessionId)) this.write(this.redactEvent(event));
      }
    } catch (error) {
      if (!this.closed) {
        this.write({ event: "session.error", data: { message: this.redact(error instanceof Error ? error.message : String(error)) } });
      }
    }
  }

  private validateCommand(command: InteractiveCommand): void {
    if (!this.rootSessionId) throw new Error("ANBAN_DSH_BRIDGE_SESSION_ID is required");
    if (command.method === "shutdown") return;
    const configuredPreset = validateInteractivePresetId(this.presetId);
    const sessionId = command.params.sessionId;
    if (sessionId !== this.rootSessionId) {
      throw new Error(`${command.method} must target the configured interactive session`);
    }
    if (command.method === "session/start") {
      if (typeof command.params.presetId !== "string") {
        throw new Error("session/start requires the configured interactive preset");
      }
      const requestedPreset = validateInteractivePresetId(command.params.presetId);
      if (requestedPreset !== configuredPreset) {
        throw new Error(`session/start preset ${requestedPreset} does not match configured preset ${configuredPreset}`);
      }
    }
  }

  private closeRuntime(): Promise<void> {
    this.closeTask ??= (async () => {
      this.closed = true;
      if (this.heartbeatTimer !== undefined) {
        clearInterval(this.heartbeatTimer);
        this.heartbeatTimer = undefined;
      }
      this.subscription?.close();
      this.subscription = undefined;
      await this.client?.close();
    })();
    return this.closeTask;
  }

  async dispose(): Promise<void> {
    await this.closeRuntime();
  }

  private async executeCommand(command: InteractiveCommand): Promise<unknown> {
    if (command.method === "shutdown") {
      await this.closeRuntime();
      this.write({ event: "session.closed", data: {} });
      return {};
    }
    this.validateCommand(command);
    await this.ensureRuntime();
    const client = this.client;
    if (!client) throw new Error("interactive runtime is unavailable");
    const result = await client.request(command.method, command.params);
    return result;
  }

  // The bridge may receive commands from multiple callers (for example, an
  // HTTP relay and a shutdown signal). Serialize them so an answer cannot
  // race a follow-up prompt or be delivered after the runtime has closed.
  command(command: InteractiveCommand): Promise<unknown> {
    const result = this.commandQueue.then(() => this.executeCommand(command));
    this.commandQueue = result.catch(() => undefined);
    return result;
  }
}

export async function runDshBridge(): Promise<void> {
  const bridge = new DshBridge((event) => {
    process.stdout.write(`${JSON.stringify(event)}\n`);
  });
  const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
  try {
    for await (const line of input) {
      if (!line.trim()) continue;
      let command: InteractiveCommand;
      try {
        command = parseInteractiveCommand(line);
      } catch (error) {
        process.stdout.write(`${JSON.stringify({ id: undefined, error: redactInteractiveSecrets(error instanceof Error ? error.message : String(error)) })}\n`);
        continue;
      }
      try {
        const result = await bridge.command(command);
        if (command.id !== undefined) process.stdout.write(`${JSON.stringify({ id: command.id, result })}\n`);
      } catch (error) {
        if (command.id !== undefined) process.stdout.write(`${JSON.stringify({ id: command.id, error: bridge.redact(error instanceof Error ? error.message : String(error)) })}\n`);
      }
      if (command.method === "shutdown") break;
    }
  } finally {
    input.close();
    await bridge.dispose();
  }
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  runDshBridge().catch((error: unknown) => {
    process.stderr.write(`${redactInteractiveSecrets(error instanceof Error ? error.message : String(error))}\n`);
    process.exitCode = 1;
  });
}
