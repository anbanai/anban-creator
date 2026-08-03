import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, readdir } from "node:fs/promises";
import { basename, extname, join, relative, resolve } from "node:path";
import { Readable } from "node:stream";

import OSS from "ali-oss";

import type { BootstrapResponse } from "./bootstrap.js";
import type { ArtifactManifestFile, ArtifactPrepareResponse, Reporter } from "./reporter.js";

const SNAPSHOT_ATTEMPTS = 3;

export interface WorkspaceArtifact {
  localPath: string;
  relativePath: string;
  filename: string;
}

export type ArtifactReporter = Pick<Reporter, "progress" | "prepareArtifactUpload" | "streamArtifactContent" | "reportArtifactManifest">;

interface ArtifactSnapshot {
  size: number;
  sha256: string;
  contentType: string;
  modifiedAt: number;
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
    if (entry.name.startsWith(".") || entry.name === "node_modules") continue;
    const path = join(directory, entry.name);
    const info = await lstat(path);
    signal?.throwIfAborted();
    if (info.isSymbolicLink()) continue;
    if (info.isDirectory()) { await scanDirectory(root, path, artifacts, signal); continue; }
    if (!info.isFile()) continue;
    artifacts.push({ localPath: path, relativePath: relative(root, path).replaceAll("\\", "/"), filename: basename(path) });
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
    let objectKey: string;
    if (mode === "stream") {
      let streamed: Awaited<ReturnType<ArtifactReporter["streamArtifactContent"]>>;
      try {
        streamed = await reporter.streamArtifactContent({ relative_path: artifact.relativePath, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, Readable.toWeb(createReadStream(artifact.localPath, { signal })) as ReadableStream<Uint8Array>, signal);
      } catch (error) {
        throw contextualError(`upload artifact ${artifact.relativePath}`, error);
      }
      if (streamed.size !== snapshot.size || streamed.sha256.toLowerCase() !== snapshot.sha256 || !streamed.object_key) throw new Error(`stream upload response does not match artifact ${artifact.relativePath}`);
      objectKey = streamed.object_key;
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
          await uploadDirectArtifact(prepared, artifact.localPath, snapshot.contentType, snapshot.sha256, signal);
        } catch (error) {
          throw contextualError(`upload artifact ${artifact.relativePath}`, error);
        }
      }
      objectKey = prepared.key;
    }
    if (await snapshotIsStable(artifact.localPath, snapshot, signal)) return { relative_path: artifact.relativePath, object_key: objectKey, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 };
  }
  throw new Error(`artifact ${artifact.relativePath} changed during final collection`);
}

async function snapshotArtifact(path: string, signal?: AbortSignal): Promise<ArtifactSnapshot> {
  signal?.throwIfAborted();
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink()) throw new Error(`artifact is not a regular file: ${path}`);
  const hash = createHash("sha256");
  await new Promise<void>((resolvePromise, reject) => createReadStream(path, { signal }).on("data", (chunk: string | Buffer) => { hash.update(chunk); }).on("error", reject).on("end", resolvePromise));
  signal?.throwIfAborted();
  const after = await lstat(path);
  if (!sameSnapshot(before, after)) throw new Error(`artifact changed while opening: ${path}`);
  return { size: before.size, sha256: hash.digest("hex"), contentType: contentTypeFor(path), modifiedAt: before.mtimeMs, inode: before.ino };
}

async function snapshotIsStable(path: string, snapshot: ArtifactSnapshot, signal?: AbortSignal): Promise<boolean> {
  signal?.throwIfAborted();
  try {
    const info = await lstat(path);
    signal?.throwIfAborted();
    return info.isFile() && !info.isSymbolicLink() && info.size === snapshot.size && info.mtimeMs === snapshot.modifiedAt && info.ino === snapshot.inode;
  } catch { return false; }
}

async function uploadDirectArtifact(prepared: ArtifactPrepareResponse, path: string, contentType: string, sha256: string, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  const headers = { ...(prepared.headers ?? {}) };
  if (headers["X-Oss-Meta-Sha256"]?.toLowerCase() !== sha256) throw new Error("direct artifact upload is missing SHA-256 metadata");
  if (prepared.upload_url) {
    const response = await fetch(prepared.upload_url, { method: prepared.method || "PUT", headers: { ...headers, "Content-Type": headers["Content-Type"] || contentType }, body: Readable.toWeb(createReadStream(path, { signal })) as ReadableStream<Uint8Array>, signal, duplex: "half" } as RequestInit);
    if (!response.ok) throw new Error(`direct artifact upload returned HTTP ${response.status}`);
    return;
  }
  if (!prepared.endpoint || !prepared.bucket || !prepared.sts_access_key_id || !prepared.sts_access_key_secret || !prepared.sts_security_token || !prepared.key) throw new Error("direct artifact upload is missing OSS STS credentials");
  const client = new OSS({ endpoint: prepared.endpoint, bucket: prepared.bucket, accessKeyId: prepared.sts_access_key_id, accessKeySecret: prepared.sts_access_key_secret, stsToken: prepared.sts_security_token });
  const stream = createReadStream(path, { signal });
  const abortUpload = () => stream.destroy(signal?.reason instanceof Error ? signal.reason : new Error("artifact upload aborted"));
  if (signal?.aborted) abortUpload();
  else signal?.addEventListener("abort", abortUpload, { once: true });
  try {
    await client.put(prepared.key, stream, {
      headers: { ...headers, "Content-Type": headers["Content-Type"] || contentType },
      timeout: 60_000,
    });
  } finally {
    signal?.removeEventListener("abort", abortUpload);
    stream.destroy();
  }
}

function contextualError(prefix: string, error: unknown): Error {
  return new Error(`${prefix}: ${error instanceof Error ? error.message : "unknown error"}`, { cause: error });
}

function sameSnapshot(left: Awaited<ReturnType<typeof lstat>>, right: Awaited<ReturnType<typeof lstat>>): boolean {
  return left.isFile() && right.isFile() && !left.isSymbolicLink() && !right.isSymbolicLink() && left.size === right.size && left.mtimeMs === right.mtimeMs && left.ino === right.ino;
}

function contentTypeFor(path: string): string {
  switch (extname(path).toLowerCase()) {
    case ".md": return "text/markdown";
    case ".html": return "text/html";
    case ".json": return "application/json";
    case ".png": return "image/png";
    case ".jpg": case ".jpeg": return "image/jpeg";
    case ".webp": return "image/webp";
    case ".mp4": return "video/mp4";
    default: return "application/octet-stream";
  }
}
