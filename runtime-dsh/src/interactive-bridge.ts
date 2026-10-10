import { HarnessClient, type HarnessNotification } from "@deepseek-ai/dsh-sdk-client";
import { existsSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline";
import { resolve } from "node:path";

type JsonRecord = Record<string, unknown>;

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

export function redactInteractiveSecrets(message: string, token = process.env.ANBAN_INTERACTIVE_RUNTIME_TOKEN ?? ""): string {
  if (!token) return message;
  return message.split(token).join("[redacted]");
}

export function resolveInteractivePatchPath(raw = process.env.ANBAN_INTERACTIVE_DSH_PATCH, cwd = process.cwd()): string {
  const value = raw?.trim() ?? "";
  if (!value) throw new Error("ANBAN_INTERACTIVE_DSH_PATCH is required");
  const path = resolve(cwd, value);
  if (!existsSync(path) || !statSync(path).isFile()) {
    throw new Error(`interactive DSH patch does not exist: ${path}`);
  }
  return path;
}

export function parseInteractiveCommand(line: string): InteractiveCommand {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    throw new Error("interactive bridge input is not valid JSON");
  }
  if (!isRecord(value) || typeof value.method !== "string") {
    throw new Error("interactive bridge command requires a method");
  }
  const methods = new Set(["session/start", "session/prompt", "session/answer", "shutdown"]);
  if (!methods.has(value.method)) {
    throw new Error(`unsupported interactive bridge method: ${value.method}`);
  }
  if (value.id !== undefined && typeof value.id !== "string" && typeof value.id !== "number") {
    throw new Error("interactive bridge request id is invalid");
  }
  const params = value.params === undefined ? {} : value.params;
  if (!isRecord(params)) throw new Error("interactive bridge params must be an object");
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
  const data = isRecord(event.data) ? event.data : {};
  if (event.type === "assistant/message") {
    const message = isRecord(data.message) ? data.message : {};
    const text = textFromContent(message.content);
    if (!text) return [];
    return [{ event: "assistant.message", data: { session_id: sessionId, text } }];
  }
  if (event.type === "tool/call") {
    const name = typeof data.name === "string" ? data.name : "";
    if (name.toLowerCase() !== "ask_user_question") return [];
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
  return [];
}

export class InteractiveBridge {
  private client: InteractiveRuntimeClient | undefined;
  private subscription: { next(): Promise<HarnessNotification>; close(): void } | undefined;
  private runtimeReady: Promise<void> | undefined;
  private closeTask: Promise<void> | undefined;
  private commandQueue: Promise<unknown> = Promise.resolve();
  private closed = false;

  constructor(
    private readonly write: (event: StableBridgeEvent) => void,
    private readonly clientFactory?: () => InteractiveRuntimeClient,
    private readonly rootSessionId = process.env.ANBAN_INTERACTIVE_SESSION_ID?.trim() ?? "",
  ) {}

  private async ensureRuntime(): Promise<void> {
    if (this.closed) throw new Error("interactive bridge is closed");
    if (this.runtimeReady) return this.runtimeReady;
    this.runtimeReady = (async () => {
      const client = this.clientFactory?.() ?? (() => {
        const dshHome = process.env.ANBAN_INTERACTIVE_DSH_HOME?.trim();
        if (!dshHome) throw new Error("ANBAN_INTERACTIVE_DSH_HOME is required");
        const patchPath = resolveInteractivePatchPath();
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
        throw new Error("interactive bridge is closed");
      }
      this.client = client;
      client.start();
      const initializeResult = await client.initialize({
        cwd: process.cwd(),
        provider: process.env.ANBAN_INTERACTIVE_PROVIDER?.trim() || "deepseek-official",
        model: process.env.ANBAN_INTERACTIVE_MODEL?.trim() || "deepseek-v4-flash",
      });
      assertInteractiveRuntimeCapabilities(initializeResult);
      if (!this.rootSessionId) throw new Error("ANBAN_INTERACTIVE_SESSION_ID is required");
      this.subscription = client.subscribeSessionTree(this.rootSessionId);
      void this.forwardNotifications();
      this.write({ event: "session.status", data: { status: "starting" } });
    })().catch(async (error: unknown) => {
      this.write({ event: "session.error", data: { message: redactInteractiveSecrets(error instanceof Error ? error.message : String(error)) } });
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
        for (const event of mapHarnessNotification(notification, this.rootSessionId)) this.write(event);
      }
    } catch (error) {
      if (!this.closed) {
        this.write({ event: "session.error", data: { message: redactInteractiveSecrets(error instanceof Error ? error.message : String(error)) } });
      }
    }
  }

  private validateCommand(command: InteractiveCommand): void {
    if (!this.rootSessionId) throw new Error("ANBAN_INTERACTIVE_SESSION_ID is required");
    if (command.method === "shutdown") return;
    const sessionId = command.params.sessionId;
    if (sessionId !== this.rootSessionId) {
      throw new Error(`${command.method} must target the configured interactive session`);
    }
    if (command.method === "session/start" && command.params.presetId !== INTERACTIVE_PRESET_ID) {
      throw new Error(`session/start must use the ${INTERACTIVE_PRESET_ID} preset`);
    }
  }

  private closeRuntime(): Promise<void> {
    this.closeTask ??= (async () => {
      this.closed = true;
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

export async function runInteractiveBridge(): Promise<void> {
  const bridge = new InteractiveBridge((event) => {
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
        if (command.id !== undefined) process.stdout.write(`${JSON.stringify({ id: command.id, error: redactInteractiveSecrets(error instanceof Error ? error.message : String(error)) })}\n`);
      }
      if (command.method === "shutdown") break;
    }
  } finally {
    input.close();
    await bridge.dispose();
  }
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  runInteractiveBridge().catch((error: unknown) => {
    process.stderr.write(`${redactInteractiveSecrets(error instanceof Error ? error.message : String(error))}\n`);
    process.exitCode = 1;
  });
}
