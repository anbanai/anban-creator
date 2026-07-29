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

interface ArtifactSnapshot {
  size: number;
  sha256: string;
  contentType: string;
  modifiedAt: number;
  inode: number;
}

export async function uploadWorkspaceArtifacts(workspace: string, bootstrap: BootstrapResponse, reporter: Reporter, signal?: AbortSignal): Promise<void> {
  const artifacts = await scanWorkspaceArtifacts(workspace);
  const files: ArtifactManifestFile[] = [];
  for (const artifact of artifacts) files.push(await uploadArtifact(artifact, bootstrap.artifact_transport.mode, reporter, signal));
  await reporter.reportArtifactManifest(files, signal);
  if (files.length) await reporter.progress(`collected ${files.length} workspace artifact(s)`, signal);
}

export async function scanWorkspaceArtifacts(workspace: string): Promise<WorkspaceArtifact[]> {
  const root = resolve(workspace);
  const output = join(root, "output");
  let outputInfo: Awaited<ReturnType<typeof lstat>>;
  try { outputInfo = await lstat(output); } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return []; throw error; }
  if (!outputInfo.isDirectory() || outputInfo.isSymbolicLink()) throw new Error("job output must be a real directory");
  const artifacts: WorkspaceArtifact[] = [];
  await scanDirectory(root, output, artifacts);
  return artifacts.sort((left, right) => (left.relativePath < right.relativePath ? -1 : left.relativePath > right.relativePath ? 1 : 0));
}

async function scanDirectory(root: string, directory: string, artifacts: WorkspaceArtifact[]): Promise<void> {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || entry.name === "node_modules") continue;
    const path = join(directory, entry.name);
    const info = await lstat(path);
    if (info.isSymbolicLink()) continue;
    if (info.isDirectory()) { await scanDirectory(root, path, artifacts); continue; }
    if (!info.isFile()) continue;
    artifacts.push({ localPath: path, relativePath: relative(root, path).replaceAll("\\", "/"), filename: basename(path) });
  }
}

async function uploadArtifact(artifact: WorkspaceArtifact, mode: "direct" | "stream", reporter: Reporter, signal?: AbortSignal): Promise<ArtifactManifestFile> {
  for (let attempt = 0; attempt < SNAPSHOT_ATTEMPTS; attempt += 1) {
    const snapshot = await snapshotArtifact(artifact.localPath);
    let objectKey: string;
    if (mode === "stream") {
      const streamed = await reporter.streamArtifactContent({ relative_path: artifact.relativePath, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, Readable.toWeb(createReadStream(artifact.localPath)) as ReadableStream<Uint8Array>, signal);
      if (streamed.size !== snapshot.size || streamed.sha256.toLowerCase() !== snapshot.sha256 || !streamed.object_key) throw new Error(`stream upload response does not match artifact ${artifact.relativePath}`);
      objectKey = streamed.object_key;
    } else {
      const prepared = await reporter.prepareArtifactUpload({ relative_path: artifact.relativePath, filename: artifact.filename, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 }, signal);
      if (!prepared.key || (prepared.max_size !== undefined && snapshot.size > prepared.max_size)) throw new Error(`prepare artifact upload rejected ${artifact.relativePath}`);
      if (prepared.upload_required) await uploadDirectArtifact(prepared, artifact.localPath, snapshot.contentType, snapshot.sha256, signal);
      objectKey = prepared.key;
    }
    if (await snapshotIsStable(artifact.localPath, snapshot)) return { relative_path: artifact.relativePath, object_key: objectKey, content_type: snapshot.contentType, size: snapshot.size, sha256: snapshot.sha256 };
  }
  throw new Error(`artifact ${artifact.relativePath} changed during final collection`);
}

async function snapshotArtifact(path: string): Promise<ArtifactSnapshot> {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink()) throw new Error(`artifact is not a regular file: ${path}`);
  const hash = createHash("sha256");
  await new Promise<void>((resolvePromise, reject) => createReadStream(path).on("data", (chunk: string | Buffer) => { hash.update(chunk); }).on("error", reject).on("end", resolvePromise));
  const after = await lstat(path);
  if (!sameSnapshot(before, after)) throw new Error(`artifact changed while opening: ${path}`);
  return { size: before.size, sha256: hash.digest("hex"), contentType: contentTypeFor(path), modifiedAt: before.mtimeMs, inode: before.ino };
}

async function snapshotIsStable(path: string, snapshot: ArtifactSnapshot): Promise<boolean> {
  try {
    const info = await lstat(path);
    return info.isFile() && !info.isSymbolicLink() && info.size === snapshot.size && info.mtimeMs === snapshot.modifiedAt && info.ino === snapshot.inode;
  } catch { return false; }
}

async function uploadDirectArtifact(prepared: ArtifactPrepareResponse, path: string, contentType: string, sha256: string, _signal?: AbortSignal): Promise<void> {
  const headers = { ...(prepared.headers ?? {}) };
  if (headers["X-Oss-Meta-Sha256"]?.toLowerCase() !== sha256) throw new Error("direct artifact upload is missing SHA-256 metadata");
  if (prepared.upload_url) {
    const response = await fetch(prepared.upload_url, { method: prepared.method || "PUT", headers: { ...headers, "Content-Type": headers["Content-Type"] || contentType }, body: Readable.toWeb(createReadStream(path)) as ReadableStream<Uint8Array>, duplex: "half" } as RequestInit);
    if (!response.ok) throw new Error(`direct artifact upload returned HTTP ${response.status}`);
    return;
  }
  if (!prepared.endpoint || !prepared.bucket || !prepared.sts_access_key_id || !prepared.sts_access_key_secret || !prepared.sts_security_token || !prepared.key) throw new Error("direct artifact upload is missing OSS STS credentials");
  const client = new OSS({ endpoint: prepared.endpoint, bucket: prepared.bucket, accessKeyId: prepared.sts_access_key_id, accessKeySecret: prepared.sts_access_key_secret, stsToken: prepared.sts_security_token });
  await client.put(prepared.key, path, { headers: { ...headers, "Content-Type": headers["Content-Type"] || contentType } });
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
