import { createHash } from "node:crypto";
import { type ReadStream } from "node:fs";
import { type FileHandle, lstat, open, readdir } from "node:fs/promises";
import { basename, extname, join, relative, resolve } from "node:path";
import { Readable } from "node:stream";

import type { BootstrapResponse } from "./bootstrap.js";
import type { ArtifactManifestFile, ArtifactPrepareResponse, Reporter } from "./reporter.js";
import { asTransferFailure, retryRequest, transportErrorFromResponse, TypedTransportError, type TransferFailure } from "./transport.js";

const SNAPSHOT_ATTEMPTS = 3;
const MINIMUM_SIGNATURE_LIFETIME_MS = 5_000;
const MAX_PUT_ERROR_RESPONSE_BYTES = 8 * 1024;
const MAX_ARTIFACT_MANIFEST_FILES = 256;
const SKIPPED_DIRECTORIES = new Set([
  ".anban-creator", ".anban-runtime-home", ".claude", ".git",
  "node_modules", "dist", "build", ".cache", ".vite",
]);
const SKIPPED_FILES = new Set([
  "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb",
  "tsconfig.json", "vite.config.ts", "vite.config.js", "eslint.config.js", "eslint.config.mjs",
]);

export interface WorkspaceArtifact {
  localPath: string;
  relativePath: string;
  filename: string;
}

export interface ArtifactUploadFailure extends TransferFailure { path: string }

export interface ArtifactUploadSummary {
  uploaded: number;
  failures: ArtifactUploadFailure[];
}

export type ArtifactReporter = Pick<Reporter, "progress" | "prepareArtifactUpload" | "streamArtifactContent" | "reportArtifactManifest">;

interface ArtifactSnapshot {
  handle: FileHandle;
  size: number;
  sha256: string;
  contentType: string;
  modifiedAt: number;
  device: number;
  inode: number;
}

export async function uploadWorkspaceArtifacts(workspace: string, bootstrap: BootstrapResponse, reporter: ArtifactReporter, signal?: AbortSignal, deadlineAt?: number): Promise<ArtifactUploadSummary> {
  let artifacts: WorkspaceArtifact[];
  try {
    artifacts = await scanWorkspaceArtifacts(workspace, signal);
  } catch (error) {
    throwIfTransferAborted(bootstrap.artifact_transport.mode === "stream" ? "stream" : "put", signal);
    throw contextualError("scan workspace artifacts", error);
  }
  const files: ArtifactManifestFile[] = [];
  const failures: ArtifactUploadFailure[] = [];
  for (const artifact of artifacts) {
    throwIfTransferAborted(bootstrap.artifact_transport.mode === "stream" ? "stream" : "put", signal);
    try {
      files.push(await uploadArtifact(artifact, bootstrap.artifact_transport.mode, reporter, signal, deadlineAt));
    } catch (error) {
      if (signal?.aborted && error instanceof TypedTransportError) throw error;
      throwIfTransferAborted(bootstrap.artifact_transport.mode === "stream" ? "stream" : "put", signal);
      const failure = asTransferFailure(bootstrap.artifact_transport.mode === "stream" ? "stream" : "put", error);
      failures.push({ path: artifact.relativePath, ...failure });
      await reporter.progress(`artifact upload failed: ${artifact.relativePath}: ${failure.code}`, signal).catch(() => {});
    }
  }
  try {
    await reporter.reportArtifactManifest(files, signal, deadlineAt);
  } catch (error) {
    if (error instanceof TypedTransportError) throw error;
    if (signal?.aborted) {
      const code = signal.reason instanceof Error && signal.reason.message.toLowerCase().includes("deadline") ? "deadline_exceeded" : "cancelled";
      throw new TypedTransportError({ operation: "manifest", code, attempts: 1, retryable: false });
    }
    throw contextualError("report artifact manifest", error);
  }
  try {
    if (files.length) await reporter.progress(`uploaded ${files.length} workspace artifact(s)`, signal);
  } catch {
    // The acknowledged manifest is authoritative; progress is best-effort.
  }
  return { uploaded: files.length, failures };
}

function throwIfTransferAborted(operation: "put" | "stream", signal?: AbortSignal): void {
  if (!signal?.aborted) return;
  const code = signal.reason instanceof Error && signal.reason.message.toLowerCase().includes("deadline") ? "deadline_exceeded" : "cancelled";
  throw new TypedTransportError({ operation, code, attempts: 1, retryable: false });
}

export async function scanWorkspaceArtifacts(workspace: string, signal?: AbortSignal): Promise<WorkspaceArtifact[]> {
  signal?.throwIfAborted();
  const root = resolve(workspace);
  const output = join(root, "output");
  let outputInfo: Awaited<ReturnType<typeof lstat>>;
  try {
    outputInfo = await lstat(output);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    signal?.throwIfAborted();
    return [];
  }
  signal?.throwIfAborted();
  if (!outputInfo.isDirectory() || outputInfo.isSymbolicLink()) throw new Error("job output must be a real directory");
  const artifacts: WorkspaceArtifact[] = [];
  await scanDirectory(root, output, artifacts, signal);
  return artifacts.sort((left, right) => (left.relativePath < right.relativePath ? -1 : left.relativePath > right.relativePath ? 1 : 0));
}

async function scanDirectory(root: string, directory: string, artifacts: WorkspaceArtifact[], signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    signal?.throwIfAborted();
    if (entry.isDirectory() && SKIPPED_DIRECTORIES.has(entry.name)) continue;
    if (!entry.isDirectory() && (entry.name.startsWith(".") || SKIPPED_FILES.has(entry.name))) continue;
    const path = join(directory, entry.name);
    const info = await lstat(path);
    signal?.throwIfAborted();
    if (info.isSymbolicLink()) continue;
    if (info.isDirectory()) { await scanDirectory(root, path, artifacts, signal); continue; }
    if (!info.isFile()) continue;
    artifacts.push({ localPath: path, relativePath: relative(root, path).replaceAll("\\", "/"), filename: basename(path) });
    if (artifacts.length > MAX_ARTIFACT_MANIFEST_FILES) throw new Error(`artifact manifest supports at most ${MAX_ARTIFACT_MANIFEST_FILES} files`);
  }
}

async function uploadArtifact(artifact: WorkspaceArtifact, mode: "direct" | "stream", reporter: ArtifactReporter, signal?: AbortSignal, deadlineAt?: number): Promise<ArtifactManifestFile> {
  for (let attempt = 0; attempt < SNAPSHOT_ATTEMPTS; attempt += 1) {
    signal?.throwIfAborted();
    let snapshot: ArtifactSnapshot;
    try {
      snapshot = await snapshotArtifact(artifact.localPath, signal);
    } catch (error) {
      throw contextualError(`hash artifact ${artifact.relativePath}`, error);
    }
    try {
      let objectKey: string;
      let contentType = snapshot.contentType;
      if (mode === "stream") {
        let streamed: Awaited<ReturnType<ArtifactReporter["streamArtifactContent"]>>;
        streamed = await reporter.streamArtifactContent(
          { relative_path: artifact.relativePath, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 },
          async (requestSignal) => Readable.toWeb(await snapshotReadStream(artifact.localPath, snapshot, "stream", requestSignal)) as ReadableStream<Uint8Array>, signal, deadlineAt,
        );
        if (streamed.size !== snapshot.size || streamed.sha256.toLowerCase() !== snapshot.sha256 || !streamed.object_key) throw new Error(`stream upload response does not match artifact ${artifact.relativePath}`);
        objectKey = streamed.object_key;
        contentType = streamed.content_type || snapshot.contentType;
      } else {
        const prepare = () => reporter.prepareArtifactUpload({ relative_path: artifact.relativePath, filename: artifact.filename, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, signal, deadlineAt);
        let prepared: ArtifactPrepareResponse = await prepare();
        if (!prepared.key || (prepared.upload_required && snapshot.size > prepared.max_size)) throw new Error(`prepare artifact upload rejected ${artifact.relativePath}`);
        let signatureRefreshed = false;
        if (prepared.upload_required && Date.parse(prepared.expires_at) - Date.now() < MINIMUM_SIGNATURE_LIFETIME_MS) {
          signal?.throwIfAborted();
          prepared = await prepare();
          signatureRefreshed = true;
        }
        if (prepared.upload_required) {
          try {
            await uploadDirectArtifact(prepared, artifact.localPath, snapshot, snapshot.sha256, signal, deadlineAt);
          } catch (error) {
            if (!(error instanceof TypedTransportError) || error.failure.code !== "signature_expired" || signatureRefreshed) throw error;
            signal?.throwIfAborted();
            prepared = await prepare();
            signatureRefreshed = true;
            if (prepared.upload_required) await uploadDirectArtifact(prepared, artifact.localPath, snapshot, snapshot.sha256, signal, deadlineAt);
          }
        }
        objectKey = prepared.key;
        contentType = prepared.upload_required ? headerValue(prepared.headers, "Content-Type") || snapshot.contentType : snapshot.contentType;
      }
      if (await snapshotIsStable(artifact.localPath, snapshot, signal)) return { relative_path: artifact.relativePath, object_key: objectKey, content_type: contentType, size: snapshot.size, sha256: snapshot.sha256 };
    } catch (error) {
      if (!(error instanceof TypedTransportError) || error.failure.code !== "integrity_mismatch" || attempt === SNAPSHOT_ATTEMPTS - 1) throw error;
    } finally {
      await snapshot.handle.close();
    }
  }
  throw new Error(`artifact ${artifact.relativePath} changed during final collection`);
}

async function snapshotArtifact(path: string, signal?: AbortSignal): Promise<ArtifactSnapshot> {
  signal?.throwIfAborted();
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink()) throw new Error(`artifact is not a regular file: ${path}`);
  const handle = await open(path, "r");
  try {
    const opened = await handle.stat();
    if (!sameSnapshot(before, opened)) throw new Error(`artifact changed while opening: ${path}`);
    const hash = createHash("sha256");
    let header = Buffer.alloc(0);
    const source = handle.createReadStream({ autoClose: false, start: 0, signal });
    await new Promise<void>((resolvePromise, reject) => source.on("data", (chunk: string | Buffer) => {
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      hash.update(bytes);
      if (header.length < 512) header = Buffer.concat([header, bytes.subarray(0, 512 - header.length)]);
    }).on("error", reject).on("end", resolvePromise));
    signal?.throwIfAborted();
    return { handle, size: opened.size, sha256: hash.digest("hex"), contentType: contentTypeFor(path, header), modifiedAt: opened.mtimeMs, device: opened.dev, inode: opened.ino };
  } catch (error) {
    await handle.close();
    throw error;
  }
}

async function snapshotIsStable(path: string, snapshot: ArtifactSnapshot, signal?: AbortSignal): Promise<boolean> {
  signal?.throwIfAborted();
  try {
    const [opened, visible] = await Promise.all([snapshot.handle.stat(), lstat(path)]);
    signal?.throwIfAborted();
    return opened.isFile() && visible.isFile() && !visible.isSymbolicLink()
      && opened.size === snapshot.size && opened.mtimeMs === snapshot.modifiedAt && opened.dev === snapshot.device && opened.ino === snapshot.inode
      && visible.size === snapshot.size && visible.mtimeMs === snapshot.modifiedAt && visible.dev === snapshot.device && visible.ino === snapshot.inode;
  } catch { return false; }
}

async function uploadDirectArtifact(prepared: ArtifactPrepareResponse, path: string, snapshot: ArtifactSnapshot, sha256: string, signal?: AbortSignal, deadlineAt?: number): Promise<void> {
  signal?.throwIfAborted();
  if (!prepared.upload_required) return;
  const headers = { ...prepared.headers };
  if (headerValue(headers, "X-Oss-Meta-Sha256")?.toLowerCase() !== sha256) throw new Error("direct artifact upload is missing SHA-256 metadata");
  await retryRequest("put", async (requestSignal) => {
    const stream = await snapshotReadStream(path, snapshot, "put", requestSignal);
    try {
      const response = await fetch(prepared.upload_url, { method: "PUT", headers, body: Readable.toWeb(stream) as ReadableStream<Uint8Array>, signal: requestSignal, duplex: "half", redirect: "error" } as RequestInit);
      if (!response.ok) {
        let code: string | undefined;
        if (response.status === 403) {
          const raw = await boundedResponseText(response, MAX_PUT_ERROR_RESPONSE_BYTES);
          if (response.headers.get("X-Anban-Error-Code") === "signature_expired" || /<Code>(RequestHasExpired|ExpiredToken)<\/Code>/.test(raw)) code = "signature_expired";
        } else {
          await response.body?.cancel().catch(() => {});
        }
        throw transportErrorFromResponse("put", response, code);
      }
    } finally {
      if (!stream.readableEnded) stream.destroy();
    }
  }, { signal: signal ?? new AbortController().signal, deadlineAt, maxAttempts: 4 });
}

async function boundedResponseText(response: Response, maximum: number): Promise<string> {
  if (!response.body) return "";
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (size <= maximum) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maximum) return "";
      chunks.push(value);
    }
  } finally {
    await reader.cancel().catch(() => {});
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function snapshotReadStream(path: string, snapshot: ArtifactSnapshot, operation: "put" | "stream", signal?: AbortSignal): Promise<ReadStream> {
  const handle = await open(path, "r");
  try {
    const opened = await handle.stat();
    if (opened.size !== snapshot.size || opened.mtimeMs !== snapshot.modifiedAt || opened.dev !== snapshot.device || opened.ino !== snapshot.inode) {
      throw new TypedTransportError({ operation, code: "integrity_mismatch", attempts: 1, retryable: false });
    }
    return handle.createReadStream({ autoClose: true, start: 0, signal });
  } catch (error) {
    await handle.close();
    throw error;
  }
}

function contextualError(prefix: string, error: unknown): Error {
  return new Error(`${prefix}: ${error instanceof Error ? error.message : "unknown error"}`, { cause: error });
}

function sameSnapshot(left: Awaited<ReturnType<typeof lstat>>, right: Awaited<ReturnType<typeof lstat>>): boolean {
  return left.isFile() && right.isFile() && !left.isSymbolicLink() && !right.isSymbolicLink() && left.size === right.size && left.mtimeMs === right.mtimeMs && left.dev === right.dev && left.ino === right.ino;
}

function contentTypeFor(path: string, header: Uint8Array): string {
  const detectedImage = detectImageContentType(header);
  if (detectedImage) return detectedImage;
  switch (extname(path).toLowerCase()) {
    case ".htm": case ".html": return "text/html";
    case ".css": return "text/css";
    case ".js": return "application/javascript";
    case ".md": return "text/markdown";
    case ".markdown": return "text/markdown";
    case ".json": return "application/json";
    case ".png": return "image/png";
    case ".jpg": case ".jpeg": return "image/jpeg";
    case ".gif": return "image/gif";
    case ".webp": return "image/webp";
    case ".svg": return "image/svg+xml";
    case ".pdf": return "application/pdf";
    case ".zip": return "application/zip";
    case ".mp4": return "video/mp4";
    default: return detectGenericContentType(header);
  }
}

function detectImageContentType(data: Uint8Array): string | undefined {
  const bytes = Buffer.from(data);
  if (bytes.length >= 8 && bytes.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return "image/png";
  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return "image/jpeg";
  if (bytes.length >= 6 && (bytes.subarray(0, 6).toString("ascii") === "GIF87a" || bytes.subarray(0, 6).toString("ascii") === "GIF89a")) return "image/gif";
  if (bytes.length >= 12 && bytes.subarray(0, 4).toString("ascii") === "RIFF" && bytes.subarray(8, 12).toString("ascii") === "WEBP") return "image/webp";
  if (bytes.length >= 2 && bytes[0] === 0x42 && bytes[1] === 0x4d) return "image/bmp";
  if (bytes.length >= 4 && bytes[0] === 0 && bytes[1] === 0 && bytes[2] === 1 && bytes[3] === 0) return "image/x-icon";
  return undefined;
}

function detectGenericContentType(data: Uint8Array): string {
  const bytes = Buffer.from(data);
  if (bytes.subarray(0, 5).toString("ascii") === "%PDF-") return "application/pdf";
  if (bytes.length >= 4 && bytes[0] === 0x50 && bytes[1] === 0x4b && [0x03, 0x05, 0x07].includes(bytes[2]!) && [0x04, 0x06, 0x08].includes(bytes[3]!)) return "application/zip";
  try {
    if (!bytes.includes(0)) {
      new TextDecoder("utf-8", { fatal: true }).decode(bytes);
      return "text/plain; charset=utf-8";
    }
  } catch {
    // Invalid UTF-8 is treated as binary.
  }
  return "application/octet-stream";
}

function headerValue(headers: Record<string, string> | undefined, name: string): string | undefined {
  const match = Object.entries(headers ?? {}).find(([key]) => key.toLowerCase() === name.toLowerCase());
  return match?.[1];
}
