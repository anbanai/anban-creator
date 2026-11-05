export type TransferOperation = "bootstrap" | "prepare" | "put" | "stream" | "manifest" | "complete" | "progress" | "job";
export type TransferErrorCode = "connection_reset" | "network_timeout" | "network_unavailable"
  | "rate_limited" | "service_unavailable" | "invalid_request" | "unauthorized"
  | "execution_conflict" | "signature_expired" | "integrity_mismatch"
  | "protocol_error" | "deadline_exceeded" | "cancelled";

export interface TransferFailure {
  code: TransferErrorCode;
  operation: TransferOperation;
  http_status?: number;
  attempts: number;
  retryable: boolean;
  request_id?: string;
}

export class TypedTransportError extends Error {
  readonly name = "TypedTransportError";
  constructor(
    public readonly failure: TransferFailure,
    readonly retryAfterMs?: number,
    public readonly serverCode?: string,
    public readonly networkCode?: string,
    /** The original failure, kept so a non-transport cause is not lost. */
    readonly cause?: unknown,
  ) {
    super(`${failure.operation} failed: ${serverCode ?? failure.code}`);
  }

  withAttempts(attempts: number): TypedTransportError {
    return new TypedTransportError({ ...this.failure, attempts }, this.retryAfterMs, this.serverCode, this.networkCode, this.cause);
  }
}

export interface RetryOptions {
  signal: AbortSignal;
  timeoutMs?: number;
  deadlineAt?: number;
  maxAttempts: 4;
}

export interface RetryDependencies {
  now(): number;
  random(): number;
  sleep(milliseconds: number, signal: AbortSignal): Promise<void>;
}

const defaultDependencies: RetryDependencies = {
  now: Date.now,
  random: Math.random,
  sleep: (milliseconds, signal) => new Promise((resolve, reject) => {
    const timer = setTimeout(done, milliseconds);
    function done() { signal.removeEventListener("abort", aborted); resolve(); }
    function aborted() { clearTimeout(timer); reject(signal.reason); }
    if (signal.aborted) aborted();
    else signal.addEventListener("abort", aborted, { once: true });
  }),
};

export async function retryRequest<T>(
  operation: TransferOperation,
  attempt: (signal: AbortSignal, attemptNumber: number) => Promise<T>,
  options: RetryOptions,
  dependencies: RetryDependencies = defaultDependencies,
): Promise<T> {
  for (let attemptNumber = 1; attemptNumber <= options.maxAttempts; attemptNumber += 1) {
    if (options.signal.aborted) throw failure(operation, abortCode(options.signal), attemptNumber - 1, false);
    if (options.deadlineAt !== undefined && dependencies.now() >= options.deadlineAt) throw failure(operation, "deadline_exceeded", attemptNumber - 1, false);
    const timeout = childTimeout(options, dependencies.now());
    try {
      return await attempt(timeout.signal, attemptNumber);
    } catch (caught) {
      const error = (options.signal.aborted
        ? failure(operation, abortCode(options.signal), attemptNumber, false)
        : normalizeError(operation, caught, timeout.timedOut(), false).withAttempts(attemptNumber));
      if (!error.failure.retryable || attemptNumber === options.maxAttempts) throw error;
      const cap = Math.min(2_000, 250 * 2 ** (attemptNumber - 1));
      const delay = Math.max(Math.floor(dependencies.random() * cap), error.retryAfterMs ?? 0);
      if (options.deadlineAt !== undefined && dependencies.now() + delay >= options.deadlineAt) {
        throw failure(operation, "deadline_exceeded", attemptNumber, false, error.failure.http_status, error.failure.request_id);
      }
      await dependencies.sleep(delay, options.signal).catch(() => { throw failure(operation, abortCode(options.signal), attemptNumber, false); });
    } finally {
      timeout.dispose();
    }
  }
  throw failure(operation, "protocol_error", options.maxAttempts, false);
}

export function transportErrorFromResponse(operation: TransferOperation, response: Response, code?: string, serverCode?: string): TypedTransportError {
  const status = response.status;
  const expired = status === 403 && code === "signature_expired";
  const errorCode: TransferErrorCode = expired ? "signature_expired"
    : status === 408 ? "network_timeout"
    : status === 429 ? "rate_limited"
    : [500, 502, 503, 504].includes(status) ? "service_unavailable"
    : status === 401 || status === 403 ? "unauthorized"
    : status === 409 ? "execution_conflict"
    : status >= 400 && status < 500 ? "invalid_request" : "protocol_error";
  const retryable = [408, 429, 500, 502, 503, 504].includes(status);
  return new TypedTransportError({
    operation, code: errorCode, http_status: status, attempts: 1, retryable,
    ...(safeRequestID(response.headers) ? { request_id: safeRequestID(response.headers) } : {}),
  }, parseRetryAfter(response.headers.get("Retry-After")), serverCode);
}

export function asTransferFailure(operation: TransferOperation, error: unknown): TransferFailure {
  return normalizeError(operation, error, false, false).failure;
}

/**
 * Renders a failure as a single log line for the container's stderr.
 *
 * undici reports every transport failure as the same "fetch failed" TypeError
 * and keeps the discriminating code (ENOTFOUND, ECONNREFUSED, ...) in the
 * cause chain. Kubernetes stores only the stderr tail as the termination
 * message, so the code has to be flattened into the text we print.
 *
 * Only allowlisted values are rendered: the mapped failure category, the
 * network code, an HTTP status, the operation, and the target host and port.
 * A free-form message is included only when no network code exists, so a
 * transient network failure never leaks request details.
 */
export function describeTransportError(operation: TransferOperation, error: unknown, endpoint?: string): string {
  const normalized = normalizeError(operation, error, false, false);
  // A typed error already knows which phase it belongs to; the caller's label
  // is only a fallback for errors that never entered a retried request.
  const label = error instanceof TypedTransportError ? error.failure.operation : operation;
  const fields = [`failure=${normalized.failure.code}`];
  if (normalized.networkCode) fields.push(`network_code=${normalized.networkCode}`);
  if (normalized.failure.http_status) fields.push(`http_status=${normalized.failure.http_status}`);
  if (normalized.serverCode) fields.push(`server_code=${normalized.serverCode}`);
  const target = describeEndpoint(endpoint);
  if (target) fields.push(target);
  if (!normalized.networkCode) {
    // A retried request rewraps its cause as protocol_error; the readable text
    // is the one the attempt actually failed with.
    const source = error instanceof TypedTransportError && error.cause !== undefined ? error.cause : error;
    const message = source instanceof Error ? source.message : "";
    if (message) fields.push(`message=${redactLogText(message)}`);
  }
  return `${label} failed: ${fields.join(" ")}`;
}

const MAX_LOG_MESSAGE = 300;

/**
 * True when an error carries a discriminating transport cause, i.e. flattening
 * it adds information. Domain errors keep their own descriptive message.
 */
export function isTransportFailure(error: unknown): boolean {
  if (error instanceof TypedTransportError) return true;
  return error instanceof TypeError && networkCode(error) !== undefined;
}

/** Renders an error for the log, flattening only transport failures. */
export function describeErrorForLog(operation: TransferOperation, error: unknown, endpoint?: string): string {
  if (!isTransportFailure(error)) {
    return error instanceof Error ? error.message : "agent job failed";
  }
  return describeTransportError(operation, error, endpoint);
}

/** Strips credentials and URL query strings before text enters the log. */
function redactLogText(value: string): string {
  return value
    .replace(/\bBearer\s+[^\s"']+/gi, "Bearer [redacted]")
    .replace(/\b(?:token|access_token|api_key|apikey|secret|signature|password)=[^\s&"']*/gi, "[redacted]")
    .replace(/(https?:\/\/[^\s?#"']+)\?[^\s"']*/gi, "$1?[redacted]")
    .slice(0, MAX_LOG_MESSAGE);
}

/** The runtime command line already carries the endpoint, so it is not a secret. */
function describeEndpoint(endpoint?: string): string | undefined {
  if (!endpoint) return undefined;
  try {
    const url = new URL(endpoint);
    return `host=${url.hostname} port=${url.port || (url.protocol === "https:" ? "443" : "80")}`;
  } catch {
    return undefined;
  }
}

function normalizeError(operation: TransferOperation, error: unknown, timedOut: boolean, cancelled: boolean): TypedTransportError {
  if (error instanceof TypedTransportError) return error;
  if (cancelled) return failure(operation, "cancelled", 1, false);
  if (timedOut) return failure(operation, "network_timeout", 1, true);
  const code = networkCode(error);
  if (code === "ECONNRESET" || code === "EPIPE") return failure(operation, "connection_reset", 1, true, undefined, undefined, code);
  if (["ETIMEDOUT", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT"].includes(code ?? "")) return failure(operation, "network_timeout", 1, true, undefined, undefined, code);
  if (["EAI_AGAIN", "ENOTFOUND", "ENETUNREACH", "EHOSTUNREACH", "ECONNREFUSED"].includes(code ?? "") || error instanceof TypeError) return failure(operation, "network_unavailable", 1, true, undefined, undefined, code);
  // Preserve the original error so a domain failure such as a rejected
  // bootstrap response is not replaced by a bare protocol_error.
  return failure(operation, "protocol_error", 1, false, undefined, undefined, undefined, error);
}

function networkCode(error: unknown): string | undefined {
  let current: unknown = error;
  const seen = new Set<object>();
  for (let depth = 0; depth < 8 && current && typeof current === "object" && !seen.has(current); depth += 1) {
    seen.add(current);
    const value = current as { code?: unknown; cause?: unknown };
    if (typeof value.code === "string" && ALLOWED_NETWORK_CODES.has(value.code)) return value.code;
    current = value.cause;
  }
  return undefined;
}

function failure(operation: TransferOperation, code: TransferErrorCode, attempts: number, retryable: boolean, httpStatus?: number, requestID?: string, networkCode?: string, cause?: unknown): TypedTransportError {
  return new TypedTransportError({ operation, code, attempts, retryable, ...(httpStatus ? { http_status: httpStatus } : {}), ...(requestID ? { request_id: requestID } : {}) }, undefined, undefined, networkCode, cause);
}

const ALLOWED_NETWORK_CODES = new Set([
  "ECONNRESET", "EPIPE", "ETIMEDOUT", "EAI_AGAIN", "ENOTFOUND", "ENETUNREACH", "EHOSTUNREACH", "ECONNREFUSED",
  "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT",
]);

function abortCode(signal: AbortSignal): "deadline_exceeded" | "cancelled" {
  return signal.reason instanceof Error && signal.reason.message.toLowerCase().includes("deadline") ? "deadline_exceeded" : "cancelled";
}

function childTimeout(options: RetryOptions, now: number): { signal: AbortSignal; timedOut(): boolean; dispose(): void } {
  const controller = new AbortController();
  let didTimeout = false;
  const remaining = options.deadlineAt === undefined ? Number.POSITIVE_INFINITY : Math.max(0, options.deadlineAt - now);
  const duration = Math.min(options.timeoutMs ?? Number.POSITIVE_INFINITY, remaining);
  const abortParent = () => controller.abort(options.signal.reason);
  options.signal.addEventListener("abort", abortParent, { once: true });
  const timer = Number.isFinite(duration) ? setTimeout(() => { didTimeout = true; controller.abort(new Error("request timeout")); }, duration) : undefined;
  return { signal: controller.signal, timedOut: () => didTimeout, dispose: () => { if (timer) clearTimeout(timer); options.signal.removeEventListener("abort", abortParent); } };
}

function safeRequestID(headers: Headers): string | undefined {
  const value = headers.get("X-Request-ID") ?? headers.get("X-Oss-Request-Id");
  return value && /^[A-Za-z0-9._:-]{1,128}$/.test(value) ? value : undefined;
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value) return undefined;
  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1_000;
  const date = Date.parse(value);
  return Number.isFinite(date) ? Math.max(0, date - Date.now()) : undefined;
}
