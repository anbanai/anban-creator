import { createHash } from "node:crypto";
import { chmod, lstat, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname, extname, join, resolve } from "node:path";

import { cleanBootstrapPath, workspacePath } from "./bootstrap.js";

const MAX_IMAGE_BYTES = 25 << 20;

export interface GeneratedImageDescriptor {
  task_file_id: string;
  file_path: string;
  download_url: string;
  mime_type: string;
  file_size: number;
  content_hash: string;
}

export function validateImageArtifactDescriptor(payload: GeneratedImageDescriptor): void {
  if (!payload.task_file_id.trim() || !payload.download_url.trim()) throw new Error("artifact task_file_id and download_url are required");
  if (!Number.isInteger(payload.file_size) || payload.file_size < 1 || payload.file_size > MAX_IMAGE_BYTES) throw new Error(`artifact file_size ${payload.file_size} is outside the allowed range`);
  if (!/^[a-f0-9]{64}$/.test(payload.content_hash)) throw new Error("artifact content_hash must be a lowercase SHA-256 digest");
  const ext = extname(payload.file_path).toLowerCase();
  if (payload.mime_type === "image/png" && ext !== ".png") throw new Error("image/png artifact must use a .png path");
  if (payload.mime_type === "image/jpeg" && ext !== ".jpg" && ext !== ".jpeg") throw new Error("image/jpeg artifact must use a .jpg or .jpeg path");
  if (payload.mime_type === "image/webp" && ext !== ".webp") throw new Error("image/webp artifact must use a .webp path");
  if (!new Set(["image/png", "image/jpeg", "image/webp"]).has(payload.mime_type)) throw new Error(`unsupported artifact MIME ${payload.mime_type}`);
}

export async function materializeGeneratedImage(runtimeDirectory: string, serverURL: string, token: string, requestedOutputPath: string, payload: GeneratedImageDescriptor, signal?: AbortSignal): Promise<void> {
  if (!requestedOutputPath || requestedOutputPath.trim() !== requestedOutputPath) throw new Error("generate_image output_path is required and must not contain surrounding whitespace");
  const cleanPath = cleanBootstrapPath(requestedOutputPath);
  if ((cleanPath !== "output" && !cleanPath.startsWith("output/")) || payload.file_path !== requestedOutputPath) throw new Error("generated image must stay inside requested output path");
  validateImageArtifactDescriptor(payload);
  const target = workspacePath(runtimeDirectory, cleanPath);
  if (await existingArtifactMatches(target, payload)) return;
  await ensureRealParents(runtimeDirectory, dirname(target));
  const body = await downloadArtifact(payload, serverURL, token, signal);
  if (body.byteLength !== payload.file_size) throw new Error(`artifact size mismatch: downloaded ${body.byteLength}, expected ${payload.file_size}`);
  const hash = createHash("sha256").update(body).digest("hex");
  if (hash !== payload.content_hash) throw new Error("artifact SHA-256 mismatch");
  await writeAtomic(target, body);
}

export function collectGeneratedImageDescriptors(value: unknown): GeneratedImageDescriptor[] {
  const result: GeneratedImageDescriptor[] = [];
  const visit = (entry: unknown): void => {
    if (Array.isArray(entry)) { entry.forEach(visit); return; }
    if (typeof entry === "string") {
      const text = entry.trim();
      if (text.startsWith("{") || text.startsWith("[")) { try { visit(JSON.parse(text)); } catch { /* no descriptor */ } }
      return;
    }
    if (!entry || typeof entry !== "object") return;
    const record = entry as Record<string, unknown>;
    if (typeof record.task_file_id === "string") {
      result.push(record as unknown as GeneratedImageDescriptor);
      return;
    }
    Object.values(record).forEach(visit);
  };
  visit(value);
  return result;
}

async function existingArtifactMatches(path: string, payload: GeneratedImageDescriptor): Promise<boolean> {
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size !== payload.file_size) return false;
    return createHash("sha256").update(await readFile(path)).digest("hex") === payload.content_hash;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw error;
  }
}

async function downloadArtifact(payload: GeneratedImageDescriptor, serverURL: string, token: string, signal?: AbortSignal): Promise<Uint8Array> {
  let url = new URL(payload.download_url, serverURL);
  const origin = new URL(serverURL).origin;
  for (let redirects = 0; redirects < 4; redirects += 1) {
    const response = await fetch(url, { headers: url.origin === origin ? { Authorization: `Bearer ${token}` } : {}, signal, redirect: "manual" });
    if (response.status >= 300 && response.status < 400) {
      const location = response.headers.get("location");
      if (!location) throw new Error("artifact download redirect is missing a location");
      url = new URL(location, url);
      continue;
    }
    if (!response.ok) throw new Error(`download artifact failed: HTTP ${response.status}`);
    if (response.headers.get("content-length") && Number(response.headers.get("content-length")) !== payload.file_size) throw new Error("artifact size mismatch from response header");
    const contentType = response.headers.get("content-type")?.split(";", 1)[0];
    if (contentType?.startsWith("image/") && contentType !== payload.mime_type) throw new Error(`artifact MIME mismatch: response is ${contentType}, descriptor is ${payload.mime_type}`);
    return readBoundedBytes(response, MAX_IMAGE_BYTES, "artifact download");
  }
  throw new Error("artifact download exceeded redirect limit");
}

async function readBoundedBytes(response: Response, limit: number, label: string): Promise<Uint8Array> {
  const reader = response.body?.getReader();
  if (!reader) return new Uint8Array();
  const chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > limit) throw new Error(`${label} exceeds size limit`);
    chunks.push(value);
  }
  const result = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength; }
  return result;
}

async function ensureRealParents(root: string, parent: string): Promise<void> {
  const resolvedRoot = resolve(root);
  if (!parent.startsWith(`${resolvedRoot}/`) && parent !== resolvedRoot) throw new Error("artifact parent escapes runtime workspace");
  let current = resolvedRoot;
  for (const component of parent.slice(resolvedRoot.length).split("/").filter(Boolean)) {
    current = join(current, component);
    try {
      const info = await lstat(current);
      if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`artifact directory ${current} must be a real directory`);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
      await mkdir(current, { mode: 0o755 });
    }
  }
}

async function writeAtomic(target: string, data: Uint8Array): Promise<void> {
  const staging = `${target}.anban-artifact-${process.pid}-${crypto.randomUUID()}`;
  try {
    await writeFile(staging, data, { mode: 0o640, flag: "wx" });
    await chmod(staging, 0o640);
    await rename(staging, target);
  } catch (error) {
    await rm(staging, { force: true });
    throw error;
  }
}
