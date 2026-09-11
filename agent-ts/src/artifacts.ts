import { createHash } from "node:crypto";
import { type ReadStream } from "node:fs";
import { type FileHandle, lstat, open, readdir } from "node:fs/promises";
import { basename, extname, join, relative, resolve } from "node:path";
import { Readable } from "node:stream";

import OSS from "ali-oss";

import type { BootstrapResponse } from "./bootstrap.js";
import type { ArtifactManifestFile, ArtifactPrepareResponse, Reporter } from "./reporter.js";

const SNAPSHOT_ATTEMPTS = 3;
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

export async function uploadWorkspaceArtifacts(workspace: string, bootstrap: BootstrapResponse, reporter: ArtifactReporter, signal?: AbortSignal): Promise<number> {
  let artifacts: WorkspaceArtifact[];
  try {
    artifacts = await scanWorkspaceArtifacts(workspace, signal);
  } catch (error) {
    throw contextualError("scan workspace artifacts", error);
  }
  const files: ArtifactManifestFile[] = [];
  for (const artifact of artifacts) {
    signal?.throwIfAborted();
    files.push(await uploadArtifact(artifact, bootstrap.artifact_transport.mode, reporter, signal));
  }
  try {
    await reporter.reportArtifactManifest(files, signal);
  } catch (error) {
    throw contextualError("report artifact manifest", error);
  }
  try {
    if (files.length) await reporter.progress(`collected ${files.length} workspace artifact(s)`, signal);
  } catch {
    // The acknowledged manifest is authoritative; progress is best-effort.
  }
  return files.length;
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

async function uploadArtifact(artifact: WorkspaceArtifact, mode: "direct" | "stream", reporter: ArtifactReporter, signal?: AbortSignal): Promise<ArtifactManifestFile> {
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
        const source = snapshotReadStream(snapshot, signal);
        try {
          streamed = await reporter.streamArtifactContent({ relative_path: artifact.relativePath, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, Readable.toWeb(source) as ReadableStream<Uint8Array>, signal);
        } catch (error) {
          throw contextualError(`upload artifact ${artifact.relativePath}`, error);
        } finally {
          if (!source.readableEnded) source.destroy();
        }
        if (streamed.size !== snapshot.size || streamed.sha256.toLowerCase() !== snapshot.sha256 || !streamed.object_key) throw new Error(`stream upload response does not match artifact ${artifact.relativePath}`);
        objectKey = streamed.object_key;
        contentType = streamed.content_type || snapshot.contentType;
      } else {
        let prepared: ArtifactPrepareResponse;
        try {
          prepared = await reporter.prepareArtifactUpload({ relative_path: artifact.relativePath, filename: artifact.filename, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, signal);
        } catch (error) {
          throw contextualError(`prepare artifact upload ${artifact.relativePath}`, error);
        }
        if (!prepared.key || (prepared.max_size !== undefined && snapshot.size > prepared.max_size)) throw new Error(`prepare artifact upload rejected ${artifact.relativePath}`);
        if (prepared.upload_required) {
          try {
            await uploadDirectArtifact(prepared, snapshot, snapshot.contentType, snapshot.sha256, signal);
          } catch (error) {
            throw contextualError(`upload artifact ${artifact.relativePath}`, error);
          }
        }
        objectKey = prepared.key;
        contentType = headerValue(prepared.headers, "Content-Type") || snapshot.contentType;
      }
      if (await snapshotIsStable(artifact.localPath, snapshot, signal)) return { relative_path: artifact.relativePath, object_key: objectKey, content_type: contentType, size: snapshot.size, sha256: snapshot.sha256 };
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

async function uploadDirectArtifact(prepared: ArtifactPrepareResponse, snapshot: ArtifactSnapshot, contentType: string, sha256: string, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  const headers = { ...(prepared.headers ?? {}) };
  if (headerValue(headers, "X-Oss-Meta-Sha256")?.toLowerCase() !== sha256) throw new Error("direct artifact upload is missing SHA-256 metadata");
  const stream = snapshotReadStream(snapshot, signal);
  if (prepared.upload_url) {
    try {
      const response = await fetch(prepared.upload_url, { method: prepared.method || "PUT", headers: { ...headers, "Content-Type": headerValue(headers, "Content-Type") || contentType }, body: Readable.toWeb(stream) as ReadableStream<Uint8Array>, signal, duplex: "half" } as RequestInit);
      if (!response.ok) throw new Error(`direct artifact upload returned HTTP ${response.status}`);
      return;
    } finally {
      if (!stream.readableEnded) stream.destroy();
    }
  }
  if (!prepared.endpoint || !prepared.bucket || !prepared.sts_access_key_id || !prepared.sts_access_key_secret || !prepared.sts_security_token || !prepared.key) throw new Error("direct artifact upload is missing OSS STS credentials");
  const client = new OSS({ endpoint: prepared.endpoint, bucket: prepared.bucket, accessKeyId: prepared.sts_access_key_id, accessKeySecret: prepared.sts_access_key_secret, stsToken: prepared.sts_security_token });
  const abortUpload = () => stream.destroy(signal?.reason instanceof Error ? signal.reason : new Error("artifact upload aborted"));
  if (signal?.aborted) abortUpload();
  else signal?.addEventListener("abort", abortUpload, { once: true });
  try {
    await client.put(prepared.key, stream, {
      headers: { ...headers, "Content-Type": headerValue(headers, "Content-Type") || contentType },
      timeout: 60_000,
    });
  } finally {
    signal?.removeEventListener("abort", abortUpload);
    if (!stream.readableEnded) stream.destroy();
  }
}

function snapshotReadStream(snapshot: ArtifactSnapshot, signal?: AbortSignal): ReadStream {
  return snapshot.handle.createReadStream({ autoClose: false, start: 0, signal });
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
