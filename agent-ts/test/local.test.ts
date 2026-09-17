import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { buildLocalPrompt, createLocalReporter, parseLocalConfig } from "../src/local.js";

async function localFixture() {
  const root = await mkdtemp(join(tmpdir(), "anban-local-"));
  const plugin = join(root, "plugin");
  await mkdir(plugin);
  await writeFile(join(plugin, "agent-pack-catalog.json"), JSON.stringify({
    packs: [
      {
        id: "article", version: "1.2.3", digest: "a".repeat(64),
        agent: { name: "article" }, bindings: { task_types: ["article"] },
        runtime: { profile: "article", adapter: "standard", max_turns: 60 },
        artifacts: [{ role: "final", path: "output/article.md", required: true }],
      },
      {
        id: "seednote", version: "1.0.0", digest: "b".repeat(64),
        agent: { name: "seednote" }, bindings: { task_types: ["seednote", "viral_analysis"] },
        runtime: { profile: "seednote", adapter: "standard", max_turns: 20 },
        artifacts: [
          { role: "content", path: "output/content.md", required: true },
          { role: "image_plan", path: "output/image-plan.md", required: true },
        ],
        artifacts_by_task_type: {
          viral_analysis: [
            { role: "analysis", path: "output/source-analysis.md", required: true },
            { role: "template", path: "output/viral-template.json", required: true },
          ],
        },
      },
    ],
  }));
  return { root, plugin };
}

describe("parseLocalConfig", () => {
  test("preserves the desktop run contract and resolves the frozen pack", async () => {
    const { root, plugin } = await localFixture();
    const config = await parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "1.2.3",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article",
      "--runtime-adapter", "standard", "--article-with-cover=false",
    ], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" });

    expect(config).toMatchObject({
      taskID: "task-1", executionID: "execution-local-1", taskType: "article", maxTurns: 60,
      agentFlag: "anban:article", executionToken: "execution-token", articleWithCover: false,
      articleWithContentImages: true,
      agentPack: {
        id: "article",
        artifacts: [{ role: "final", path: "output/article.md", required: true }],
      },
    });
  });

  test("rejects execution credentials in process arguments", async () => {
    const { root, plugin } = await localFixture();
    await expect(parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--execution-token", "exposed-token", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
    ], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" })).rejects.toThrow("unknown argument --execution-token");
  });

  test("rejects a desktop pack identity that differs from the runtime catalog", async () => {
    const { root, plugin } = await localFixture();
    expect(parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "9.9.9",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article", "--runtime-adapter", "standard",
    ], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" })).rejects.toThrow("frozen Agent Pack identity");
  });

  test("resolves task-specific Seednote artifact contracts for local runs", async () => {
    const { root, plugin } = await localFixture();
    const baseArgs = [
      "run", "--server-url", "https://creator.example.com", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--workspace", root,
    ];

    const viral = await parseLocalConfig([...baseArgs, "--task-type", "viral_analysis"], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" });
    expect(viral.agentPack.artifacts.map((artifact) => artifact.path)).toEqual(["output/source-analysis.md", "output/viral-template.json"]);

    const normal = await parseLocalConfig([...baseArgs, "--task-type", "seednote"], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" });
    expect(normal.agentPack.artifacts.map((artifact) => artifact.path)).toEqual(["output/content.md", "output/image-plan.md"]);
  });

  test("requires a non-empty execution identity", async () => {
    const { root, plugin } = await localFixture();
    const base = [
      "run", "--server-url", "https://creator.example.com", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--task-type", "article", "--workspace", root,
    ];
    await expect(parseLocalConfig(base, { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" })).rejects.toThrow("--execution-id is required");
    await expect(parseLocalConfig([...base, "--execution-id", "   "], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" })).rejects.toThrow("--execution-id is required");
  });

  test("constructs the local reporter with the claimed execution identity", async () => {
    const { root, plugin } = await localFixture();
    const config = await parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--artifact-upload-mode", "stream",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
    ], { CLAUDE_PLUGIN_ROOT: plugin, ANBAN_EXECUTION_TOKEN: "execution-token" });
    const requests: unknown[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (_input, init) => {
      requests.push(JSON.parse(String(init?.body)));
      return new Response("{}", { status: 200 });
    };
    try {
      await createLocalReporter(config).stageProgress({ stage: "research", state: "active" });
      expect(requests).toEqual([{ task_id: "task-1", execution_id: "execution-local-1", stage: "research", state: "active", description: "" }]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("buildLocalPrompt", () => {
  test("keeps article controls, task identity, project identity, and resume instructions", async () => {
    const { root } = await localFixture();
    await mkdir(join(root, ".anban-creator", "resume"), { recursive: true });
    await writeFile(join(root, ".anban-creator", "resume", "latest.md"), "continue");
    const prompt = buildLocalPrompt({
      taskType: "article", topic: "migration", taskID: "task-1", workspace: root,
      hasContentImage: true, hasTailImage: false,
      articleWithCover: false, articleWithContentImages: true,
    }, "project-1");
    expect(prompt).toContain("article_image_mode=content_only");
    expect(prompt).toContain("task_id=task-1, project_id=project-1");
    expect(prompt).toContain(".anban-creator/resume/latest.md");
  });
});

afterEach(() => {
  delete process.env.CLAUDE_PLUGIN_ROOT;
});
