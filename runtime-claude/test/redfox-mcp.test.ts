import { afterEach, describe, expect, test } from "bun:test";
import { runRedFoxResearch } from "../src/redfox-mcp.js";

const originalFetch = globalThis.fetch;
const originalKey = process.env.REDFOX_API_KEY;
const originalBase = process.env.REDFOX_API_BASE_URL;

afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalKey === undefined) delete process.env.REDFOX_API_KEY;
  else process.env.REDFOX_API_KEY = originalKey;
  if (originalBase === undefined) delete process.env.REDFOX_API_BASE_URL;
  else process.env.REDFOX_API_BASE_URL = originalBase;
});

describe("RedFox research capability", () => {
  test("normalizes cross-platform data and never returns the API key", async () => {
    process.env.REDFOX_API_KEY = "secret-key";
    process.env.REDFOX_API_BASE_URL = "https://redfox.test";
    globalThis.fetch = (async (input, init) => {
      expect(String(input)).toBe("https://redfox.test/story/api/hotKeyword/list");
      expect(new Headers(init?.headers).get("X-API-KEY")).toBe("secret-key");
      return new Response(JSON.stringify({ code: 2000, data: [{ hotSpotList: [{ platName: "抖音", title: " 热点 ", maxHotScore: 123, url: "https://example.test/a" }] }] }), { status: 200 });
    }) as typeof fetch;

    const output = await runRedFoxResearch({ mode: "cross_platform", limit: 10 });
    expect(output.items).toEqual([{ title: "热点", platform: "抖音", hot: 123, url: "https://example.test/a", rank: 1 }]);
    expect(output.freshness).toBe("fresh");
    expect(JSON.stringify(output)).not.toContain("secret-key");
  });

  test("returns a structured fallback for missing credentials", async () => {
    delete process.env.REDFOX_API_KEY;
    const output = await runRedFoxResearch({ mode: "douyin_daily", date: "2026-11-03" });
    expect(output.items).toEqual([]);
    expect(output.freshness).toBe("unknown");
    expect(output.fallback_reason).toBe("missing_redfox_api_key");
  });

  test("supports WeChat, Xiaohongshu, and Douyin endpoint contracts", async () => {
    process.env.REDFOX_API_KEY = "secret-key";
    process.env.REDFOX_API_BASE_URL = "https://redfox.test";
    const paths: string[] = [];
    globalThis.fetch = (async (input) => {
      const path = new URL(String(input)).pathname;
      paths.push(path);
      const payload = path.includes("gzh")
        ? { code: 2000, data: { articles: [{ title: "公众号", url: "https://example.test/w" }] } }
        : path.includes("xhs")
          ? { code: 2000, data: { articles: [{ title: "小红书", shareInfoLink: "https://example.test/x" }] } }
          : { code: 2000, data: { list: [{ content: "抖音", opusUrl: "https://example.test/d" }] } };
      return new Response(JSON.stringify(payload), { status: 200 });
    }) as typeof fetch;
    await runRedFoxResearch({ mode: "wechat_search", query: "AI" });
    await runRedFoxResearch({ mode: "xiaohongshu_search", query: "AI" });
    await runRedFoxResearch({ mode: "douyin_search", query: "AI" });
    expect(paths).toEqual([
      "/story/api/gzh/search/hotArticleNew",
      "/story/api/xhs/search/search",
      "/story/api/dy/data/searchWork",
    ]);
  });
});
