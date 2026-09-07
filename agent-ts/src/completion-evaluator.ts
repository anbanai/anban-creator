import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

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

export async function evaluateCompletionMetadata(workspace: string, profile: ExecutionProfile, signal?: AbortSignal): Promise<void> {
  const metadataPath = join(workspace, "output", "completion-metadata.json");
  const metadata = JSON.parse(await readFile(metadataPath, "utf8")) as CompletionMetadata;
  try {
    const env = profile.envs;
    const apiKey = env.ANTHROPIC_API_KEY || env.ANTHROPIC_AUTH_TOKEN;
    const model = env.ANTHROPIC_MODEL || env.ANTHROPIC_DEFAULT_SONNET_MODEL;
    if (!apiKey || !model) throw new Error("runtime evaluator model configuration is unavailable");

    const sources: Array<{ file: string; content: string }> = [];
    let remaining = 120_000;
    for (const artifact of metadata.artifacts ?? []) {
      const file = typeof artifact.file === "string" ? artifact.file : "";
      if (!file || !/\.(md|txt|json)$/i.test(file) || remaining <= 0) continue;
      const content = (await readFile(join(workspace, file), "utf8")).slice(0, remaining);
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

function stripJSONFence(raw: string): string {
  const match = raw.match(/^```(?:json)?\s*([\s\S]*?)\s*```$/i);
  return match?.[1] ?? raw;
}
