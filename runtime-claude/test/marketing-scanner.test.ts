import { afterEach, describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const roots: string[] = [];
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

async function runScanner(input: string, fix = false) {
  const root = await mkdtemp(join(tmpdir(), "anban-marketing-scan-"));
  roots.push(root);
  await mkdir(join(root, "output"));
  const article = join(root, "output", "04-article-final.md");
  const report = join(root, "output", "marketing-scan.json");
  await writeFile(article, input);
  const script = resolve(import.meta.dir, "../../harness/skills/content-writing/scripts/scan-article-marketing.mjs");
  const process = Bun.spawn(["bun", script, article, report, ...(fix ? ["--fix"] : [])], { stdout: "pipe", stderr: "pipe" });
  const exitCode = await process.exited;
  if (exitCode !== 0) throw new Error(await new Response(process.stderr).text());
  return {
    article: await readFile(article, "utf8"),
    report: JSON.parse(await readFile(report, "utf8")) as Record<string, any>,
  };
}

describe("article marketing scanner", () => {
  test("reports block-publish findings with redacted evidence", async () => {
    const { report } = await runScanner("详情访问 https://example.com 或联系 13812345678。\n回复关键词领资料。\n");
    expect(report.status).toBe("block_publish");
    expect(report.file).toBe("output/04-article-final.md");
    expect(report.content_hash).toMatch(/^[a-f0-9]{64}$/);
    expect(report.findings).toEqual(expect.arrayContaining([
      expect.objectContaining({ rule_id: "external_url", severity: "block_publish", line: 1 }),
      expect.objectContaining({ rule_id: "contact_phone", severity: "block_publish", line: 1 }),
      expect.objectContaining({ rule_id: "keyword_reward", severity: "block_publish", line: 2 }),
    ]));
    expect(JSON.stringify(report.findings)).not.toContain("13812345678");
    expect(JSON.stringify(report.findings)).not.toContain("https://example.com");
    expect(report.findings.every((finding: { file: string }) => finding.file === "output/04-article-final.md")).toBe(true);
  });

  test("applies only a single low-ambiguity CTA revision pass", async () => {
    const { article, report } = await runScanner("欢迎私信我交流你的看法。\n", true);
    expect(article).toBe("欢迎在评论区交流你的看法。\n");
    expect(report.status).toBe("passed");
    expect(report.auto_revision).toEqual({ attempted: true, changed: true, passes: 1 });
  });

  test("detects marketing terms split by inline Markdown without changing the source", async () => {
    const source = "添加我**微信**即可领取资料。\n138**0013**8000\n";
    const { article, report } = await runScanner(source);

    expect(article).toBe(source);
    expect(report.status).toBe("block_publish");
    expect(report.content_hash).toBe(createHash("sha256").update(source).digest("hex"));
    expect(report.findings).toEqual(expect.arrayContaining([
      expect.objectContaining({ rule_id: "private_contact", line: 1 }),
      expect.objectContaining({ rule_id: "contact_phone", line: 2 }),
    ]));
  });

  test("scans ordinary Markdown link destinations while ignoring image destinations", async () => {
    const { report } = await runScanner("[查看详情](https://marketing.example/join)\n![配图](https://mmbiz.qpic.cn/article.png)\n");

    expect(report.status).toBe("block_publish");
    expect(report.findings.filter((finding: { rule_id: string }) => finding.rule_id === "external_url")).toHaveLength(1);
  });

  test("scans raw HTML anchor destinations", async () => {
    const { report } = await runScanner('<a class="cta" href="https://marketing.example/join">查看详情</a>\n');

    expect(report.status).toBe("block_publish");
    expect(report.findings).toEqual(expect.arrayContaining([
      expect.objectContaining({ rule_id: "external_url", line: 1 }),
    ]));
    expect(JSON.stringify(report.findings)).not.toContain("https://marketing.example/join");
  });

  test("scans every outbound Markdown link form rendered by the article pipeline", async () => {
    const { report } = await runScanner([
      "[协议相对](//marketing.example/join)",
      "[非层级 HTTP](http:marketing.example/join)",
      "<https://marketing.example/autolink>",
    ].join("\n"));

    expect(report.status).toBe("block_publish");
    expect(report.findings.filter((finding: { rule_id: string }) => finding.rule_id === "external_url")).toHaveLength(3);
  });

  test("detects marketing terms split by Unicode format characters", async () => {
    const source = "添加我微\u200B信即可领取资料。\n138\u200B00138000\n";
    const { article, report } = await runScanner(source);

    expect(article).toBe(source);
    expect(report.status).toBe("block_publish");
    expect(report.findings).toEqual(expect.arrayContaining([
      expect.objectContaining({ rule_id: "private_contact", line: 1 }),
      expect.objectContaining({ rule_id: "contact_phone", line: 2 }),
    ]));
    expect(JSON.stringify(report.findings)).not.toContain("138\u200B00138000");
  });

  test("detects marketing terms split by non-Cf default-ignorable characters", async () => {
    const source = "添加我微\uFE0F信即可领取资料。\n138\u034F00138000\n";
    const { report } = await runScanner(source);

    expect(report.status).toBe("block_publish");
    expect(report.findings).toEqual(expect.arrayContaining([
      expect.objectContaining({ rule_id: "private_contact", line: 1 }),
      expect.objectContaining({ rule_id: "contact_phone", line: 2 }),
    ]));
    expect(JSON.stringify(report.findings)).not.toContain("138\u034F00138000");
  });
});
