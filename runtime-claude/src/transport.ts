export type TransferOperation = "prepare" | "put" | "stream" | "manifest" | "complete" | "progress";
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
  ) {
    super(`${failure.operation} failed: ${serverCode ?? failure.code}`);
  }

  withAttempts(attempts: number): TypedTransportError {
    return new TypedTransportError({ ...this.failure, attempts }, this.retryAfterMs, this.serverCode);
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

function normalizeError(operation: TransferOperation, error: unknown, timedOut: boolean, cancelled: boolean): TypedTransportError {
  if (error instanceof TypedTransportError) return error;
  if (cancelled) return failure(operation, "cancelled", 1, false);
  if (timedOut) return failure(operation, "network_timeout", 1, true);
  const code = networkCode(error);
  if (code === "ECONNRESET" || code === "EPIPE") return failure(operation, "connection_reset", 1, true);
  if (["ETIMEDOUT", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT"].includes(code ?? "")) return failure(operation, "network_timeout", 1, true);
  if (["EAI_AGAIN", "ENETUNREACH", "EHOSTUNREACH", "ECONNREFUSED"].includes(code ?? "") || error instanceof TypeError) return failure(operation, "network_unavailable", 1, true);
  return failure(operation, "protocol_error", 1, false);
}

function networkCode(error: unknown): string | undefined {
  if (!error || typeof error !== "object") return undefined;
  const value = error as { code?: unknown; cause?: { code?: unknown } };
  const code = value.code ?? value.cause?.code;
  return typeof code === "string" ? code : undefined;
}

function failure(operation: TransferOperation, code: TransferErrorCode, attempts: number, retryable: boolean, httpStatus?: number, requestID?: string): TypedTransportError {
  return new TypedTransportError({ operation, code, attempts, retryable, ...(httpStatus ? { http_status: httpStatus } : {}), ...(requestID ? { request_id: requestID } : {}) });
}

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
