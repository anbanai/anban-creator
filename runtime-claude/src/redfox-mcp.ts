import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const DEFAULT_BASE_URL = "https://redfox.hk";
const DEFAULT_TIMEOUT_MS = 30_000;
const MAX_LIMIT = 50;

const modeSchema = z.enum([
  "cross_platform",
  "wechat_search",
  "xiaohongshu_search",
  "douyin_search",
  "douyin_daily",
]);

type ResearchMode = z.infer<typeof modeSchema>;
type JsonObject = Record<string, unknown>;

export interface RedFoxResearchInput {
  mode: ResearchMode;
  query?: string;
  date?: string;
  start_date?: string;
  end_date?: string;
  category?: string;
  page?: number;
  limit?: number;
}

export interface RedFoxResearchResult {
  schema_version: "1.0";
  data_source: "redfox";
  mode: ResearchMode;
  query: JsonObject;
  fetched_at: string;
  freshness: "fresh" | "stale" | "unknown";
  source_url: string;
  missing_fields: string[];
  fallback_reason: string;
  items: JsonObject[];
  total?: number;
}

function cleanString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function asObject(value: unknown): JsonObject {
  return value && typeof value === "object" && !Array.isArray(value) ? value as JsonObject : {};
}

function asArray(value: unknown): JsonObject[] {
  return Array.isArray(value) ? value.filter((item): item is JsonObject => Boolean(item && typeof item === "object" && !Array.isArray(item))).map(asObject) : [];
}

function readNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() && Number.isFinite(Number(value))) return Number(value);
  return undefined;
}

function normalizeLimit(value: number | undefined): number {
  if (!Number.isFinite(value)) return 20;
  return Math.max(1, Math.min(MAX_LIMIT, Math.trunc(value ?? 20)));
}

function sourceURL(baseURL: string, path: string): string {
  return `${baseURL.replace(/\/$/, "")}${path}`;
}

function missingFields(items: JsonObject[], fields: string[]): string[] {
  return fields.filter((field) => items.length === 0 || items.some((item) => cleanString(item[field]) === "" && readNumber(item[field]) === undefined));
}

function result(input: RedFoxResearchInput, fetchedAt: string, source: string, items: JsonObject[], missing: string[], total?: number): RedFoxResearchResult {
  return {
    schema_version: "1.0",
    data_source: "redfox",
    mode: input.mode,
    query: {
      mode: input.mode,
      ...(input.query ? { query: input.query } : {}),
      ...(input.date ? { date: input.date } : {}),
      ...(input.start_date ? { start_date: input.start_date } : {}),
      ...(input.end_date ? { end_date: input.end_date } : {}),
      ...(input.category ? { category: input.category } : {}),
      ...(input.page ? { page: input.page } : {}),
      limit: normalizeLimit(input.limit),
    },
    fetched_at: fetchedAt,
    freshness: items.length > 0 ? "fresh" : "unknown",
    source_url: source || "unknown",
    missing_fields: missing.length > 0 ? missing : ["none"],
    fallback_reason: "none",
    items,
    ...(total === undefined ? {} : { total }),
  };
}

function failure(input: RedFoxResearchInput, reason: string, source = "unknown"): RedFoxResearchResult {
  return {
    ...result(input, new Date().toISOString(), source, [], ["items"], 0),
    freshness: "unknown",
    fallback_reason: reason,
  };
}

async function postJSON(baseURL: string, path: string, body: JsonObject, signal?: AbortSignal): Promise<{ status: number; payload: JsonObject }> {
  const apiKey = cleanString(process.env.REDFOX_API_KEY);
  if (!apiKey) throw new Error("missing_redfox_api_key");
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS);
  if (signal) signal.addEventListener("abort", () => controller.abort(), { once: true });
  try {
    const response = await fetch(sourceURL(baseURL, path), {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-API-KEY": apiKey, Accept: "application/json" },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
    let payload: JsonObject = {};
    try {
      const decoded: unknown = await response.json();
      payload = asObject(decoded);
    } catch {
      throw new Error("malformed_redfox_response");
    }
    return { status: response.status, payload };
  } finally {
    clearTimeout(timer);
  }
}

async function request(baseURL: string, path: string, body: JsonObject, signal?: AbortSignal): Promise<JsonObject> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const response = await postJSON(baseURL, path, body, signal);
      if (response.status < 200 || response.status >= 300) {
        if (response.status >= 500 && attempt === 0) continue;
        throw new Error(`redfox_http_${response.status}`);
      }
      const code = readNumber(response.payload.code);
      if (code !== 2000) throw new Error(`redfox_api_error_${code ?? "unknown"}`);
      return response.payload;
    } catch (error) {
      lastError = error;
      const message = error instanceof Error ? error.message : String(error);
      if (attempt === 0 && (message.includes("fetch") || message.includes("abort") || message.includes("network"))) continue;
    }
  }
  throw lastError instanceof Error ? lastError : new Error("redfox_request_failed");
}

function hotspotItems(payload: JsonObject, limit: number): JsonObject[] {
  const groups = asArray(payload.data);
  const items: JsonObject[] = [];
  for (const group of groups) {
    for (const spot of asArray(group.hotSpotList)) {
      items.push({
        title: cleanString(spot.title).replaceAll(" ", ""),
        platform: cleanString(spot.platName),
        hot: spot.maxHotScore ?? null,
        url: cleanString(spot.url).replaceAll(" ", "%20"),
        rank: items.length + 1,
      });
      if (items.length >= limit) return items;
    }
  }
  return items;
}

function articleItems(payload: JsonObject, limit: number): JsonObject[] {
  const data = asObject(payload.data);
  return [...asArray(data.articles), ...asArray(data.latestHotArticles)].slice(0, limit).map((item) => ({
    title: cleanString(item.title),
    author: cleanString(item.author ?? item.authorName),
    url: cleanString(item.url ?? item.link),
    published_at: cleanString(item.publishTime ?? item.publishDate),
    read_count: item.readCount ?? item.readNum ?? null,
    like_count: item.likeCount ?? item.praiseNum ?? null,
    comment_count: item.commentCount ?? null,
  }));
}

function xhsItems(payload: JsonObject, limit: number): JsonObject[] {
  const data = asObject(payload.data);
  return asArray(data.articles).slice(0, limit).map((item) => ({
    title: cleanString(item.title ?? item.desc),
    author: cleanString(item.author ?? item.authorName),
    url: cleanString(item.shareInfoLink ?? item.url),
    published_at: cleanString(item.publishTime ?? item.publishDate),
    like_count: item.likeCount ?? item.likes ?? null,
    collect_count: item.collectCount ?? item.favorites ?? null,
    comment_count: item.commentCount ?? null,
    share_count: item.shareCount ?? null,
  }));
}

function douyinItems(payload: JsonObject, limit: number): JsonObject[] {
  const data = asObject(payload.data);
  const source = Array.isArray(data.list) ? data.list : Array.isArray(payload.data) ? payload.data : [];
  return asArray(source).slice(0, limit).map((item) => ({
    title: cleanString(item.content ?? item.title),
    author: cleanString(item.authorName ?? item.author),
    url: cleanString(item.opusUrl ?? item.url),
    published_at: cleanString(item.publishTime ?? item.publishDate),
    like_count: item.likeCount ?? null,
    comment_count: item.commentCount ?? null,
    share_count: item.shareCount ?? null,
  }));
}

export async function runRedFoxResearch(input: RedFoxResearchInput, signal?: AbortSignal): Promise<RedFoxResearchResult> {
  const baseURL = cleanString(process.env.REDFOX_API_BASE_URL) || DEFAULT_BASE_URL;
  const limit = normalizeLimit(input.limit);
  const fetchedAt = new Date().toISOString();
  try {
    let path: string;
    let body: JsonObject;
    let items: JsonObject[];
    switch (input.mode) {
      case "cross_platform":
        path = "/story/api/hotKeyword/list";
        body = { source: "anban-redfox-research", ...(input.start_date ? { startDate: input.start_date } : {}), ...(input.end_date ? { endDate: input.end_date } : {}) };
        items = hotspotItems(await request(baseURL, path, body, signal), limit);
        break;
      case "wechat_search":
        path = "/story/api/gzh/search/hotArticleNew";
        body = { keyword: cleanString(input.query), startDate: cleanString(input.start_date), endDate: cleanString(input.end_date), source: "anban-redfox-research" };
        items = articleItems(await request(baseURL, path, body, signal), limit);
        break;
      case "xiaohongshu_search":
        path = "/story/api/xhs/search/search";
        body = { keyword: cleanString(input.query), pageNum: input.page ?? 1, pageSize: limit, startDate: cleanString(input.start_date), endDate: cleanString(input.end_date), source: "anban-redfox-research" };
        items = xhsItems(await request(baseURL, path, body, signal), limit);
        break;
      case "douyin_search":
        path = "/story/api/dy/data/searchWork";
        body = { keyword: cleanString(input.query), pageNum: input.page ?? 1, pageSize: limit, source: "anban-redfox-research", ...(input.start_date ? { startDate: input.start_date } : {}), ...(input.end_date ? { endDate: input.end_date } : {}) };
        items = douyinItems(await request(baseURL, path, body, signal), limit);
        break;
      case "douyin_daily":
        path = "/story/api/dy/search/likesRank";
        body = { source: "anban-redfox-research", ...(input.category ? { type: input.category } : {}), ...(input.date ? { startTime: input.date, endTime: input.date } : {}) };
        items = douyinItems(await request(baseURL, path, body, signal), limit);
        break;
    }
    const source = sourceURL(baseURL, path);
    if (items.length === 0) return failure(input, "empty_result", source);
    return result(input, fetchedAt, source, items, missingFields(items, ["title", "url"]), items.length);
  } catch (error) {
    const reason = error instanceof Error ? error.message : "redfox_request_failed";
    return failure(input, reason);
  }
}

export async function startRedFoxMCP(): Promise<void> {
  const server = new McpServer({ name: "anban-redfox", version: "1.0.0" });
  server.registerTool("redfox_research", {
    title: "RedFox research",
    description: "Read-only RedFox research for trends and public platform search. Never publishes, subscribes, downloads, pays, or changes accounts.",
    inputSchema: {
      mode: modeSchema,
      query: z.string().trim().max(200).optional(),
      date: z.string().trim().max(32).optional(),
      start_date: z.string().trim().max(32).optional(),
      end_date: z.string().trim().max(32).optional(),
      category: z.string().trim().max(64).optional(),
      page: z.number().int().min(1).max(100).optional(),
      limit: z.number().int().min(1).max(MAX_LIMIT).optional(),
    },
    annotations: { readOnlyHint: true, destructiveHint: false, openWorldHint: true },
  }, async (input) => {
    const output = await runRedFoxResearch(input);
    return { content: [{ type: "text", text: JSON.stringify(output) }], structuredContent: { ...output } };
  });
  await server.connect(new StdioServerTransport());
}

if (process.argv[1]?.endsWith("redfox-mcp.js")) await startRedFoxMCP();
