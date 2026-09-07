import { lstat, readFile, realpath, stat, writeFile } from "node:fs/promises";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import Anthropic from "@anthropic-ai/sdk";
import { z } from "zod";

import type { ExecutionProfile } from "./bootstrap.js";

const dimensions = [
  "industry", "topic", "audience", "intent", "funnel_stage", "format", "narrative_hook",
  "tone", "value_proposition", "media_shape", "source_relation", "evidence_level", "visual_style", "risk",
] as const;

const evaluatorResultSchema = z.object({
  tags: z.array(z.object({
    dimension: z.enum(dimensions),
    value: z.string().trim().min(1).max(160),
    canonical_value: z.string().trim().min(1).max(160),
    display_name: z.string().trim().min(1).max(160),
    label_status: z.enum(["canonical", "candidate"]),
    confidence: z.number().min(0).max(1),
    primary: z.boolean(),
    evidence: z.object({
      source_file: z.string().trim().min(1),
      location: z.object({ line: z.number().int().positive() }).strict(),
      evidence_hash: z.string().regex(/^[a-f0-9]{64}$/),
    }).strict(),
  }).strict()).max(80),
  feedback: z.object({
    scores: z.object({ quality: z.number().min(0).max(10), completeness: z.number().min(0).max(10), efficiency: z.number().min(0).max(10) }).strict(),
    errors: z.string().max(4000),
    optimizations: z.string().max(4000),
    summary: z.string().max(500),
  }).strict(),
}).strict();

type CompletionMetadata = Record<string, unknown> & { artifacts?: Array<{ file?: string }>; tags?: unknown; feedback?: unknown };

const MAX_METADATA_BYTES = 512 * 1024;
const MAX_ARTIFACTS = 100;
const MAX_SOURCE_BYTES = 120_000;

export async function evaluateCompletionMetadata(workspace: string, profile: ExecutionProfile, signal?: AbortSignal): Promise<void> {
  const metadataPath = join(workspace, "output", "completion-metadata.json");
  const metadataStat = await stat(metadataPath);
  if (metadataStat.size > MAX_METADATA_BYTES) throw new Error("completion metadata exceeds size limit");
  const metadata = JSON.parse(await readFile(metadataPath, "utf8")) as CompletionMetadata;
  try {
    const env = profile.envs;
    const apiKey = env.ANTHROPIC_API_KEY || env.ANTHROPIC_AUTH_TOKEN;
    const model = env.ANTHROPIC_MODEL || env.ANTHROPIC_DEFAULT_SONNET_MODEL;
    if (!apiKey || !model) throw new Error("runtime evaluator model configuration is unavailable");

    const sources: Array<{ file: string; content: string }> = [];
    let remaining = MAX_SOURCE_BYTES;
    const artifacts = metadata.artifacts ?? [];
    if (artifacts.length > MAX_ARTIFACTS) throw new Error("completion metadata contains too many artifacts");
    for (const artifact of artifacts) {
      const file = typeof artifact.file === "string" ? artifact.file : "";
      if (!file || !/\.(md|txt|json)$/i.test(file) || remaining <= 0) continue;
      const content = await readSafeArtifact(workspace, file, remaining);
      sources.push({ file, content });
      remaining -= content.length;
    }
    const client = new Anthropic({ apiKey, baseURL: env.ANTHROPIC_BASE_URL });
    const response = await client.messages.create({
      model,
      max_tokens: 5000,
      temperature: 0,
      system: "You are a content completion evaluator. Return JSON only. Preserve evidence source_file and evidence_hash from the supplied mechanical metadata. Never invent sources. Use only the 14 allowed dimensions and score quality, completeness, and efficiency from 0 to 10.",
      messages: [{ role: "user", content: JSON.stringify({ taxonomy_version: metadata.taxonomy_version, mechanical_metadata: metadata, sources }) }],
    }, { signal });
    const raw = response.content.filter((block) => block.type === "text").map((block) => block.text).join("").trim();
    const parsed = evaluatorResultSchema.parse(JSON.parse(stripJSONFence(raw)));
    metadata.tags = parsed.tags;
    metadata.feedback = parsed.feedback;
    metadata.evaluation_status = "evaluated";
    metadata.evaluator = `${profile.provider}/${model}`;
    delete metadata.evaluation_error;
  } catch (error) {
    metadata.evaluation_status = "mechanical_fallback";
    metadata.evaluation_error = error instanceof Error ? error.message.slice(0, 500) : "runtime evaluator failed";
  }
  await writeFile(metadataPath, `${JSON.stringify(metadata, null, 2)}\n`);
}

async function readSafeArtifact(workspace: string, artifactFile: string, maxBytes: number): Promise<string> {
  if (isAbsolute(artifactFile) || artifactFile.includes("\\") || artifactFile.split("/").includes("..")) throw new Error(`invalid completion artifact path: ${artifactFile}`);
  const workspaceRoot = resolve(workspace);
  const candidate = resolve(workspaceRoot, artifactFile);
  const rel = relative(workspaceRoot, candidate);
  if (!rel || rel.startsWith(`..${sep}`) || isAbsolute(rel) || !(rel === "output" || rel.startsWith(`output${sep}`))) throw new Error(`completion artifact is outside output: ${artifactFile}`);
  const realRoot = await realpath(workspaceRoot);
  const realCandidate = await realpath(candidate);
  const realRel = relative(realRoot, realCandidate);
  if (!realRel || realRel.startsWith(`..${sep}`) || isAbsolute(realRel) || !(realRel === "output" || realRel.startsWith(`output${sep}`))) throw new Error(`completion artifact resolves outside workspace: ${artifactFile}`);
  const entry = await lstat(candidate);
  if (entry.isSymbolicLink() || !entry.isFile()) throw new Error(`completion artifact is not a regular file: ${artifactFile}`);
  const bounded = Math.min(entry.size, maxBytes);
  return (await readFile(candidate, { encoding: "utf8" })).slice(0, bounded);
}

function stripJSONFence(raw: string): string {
  const match = raw.match(/^```(?:json)?\s*([\s\S]*?)\s*```$/i);
  return match?.[1] ?? raw;
}
