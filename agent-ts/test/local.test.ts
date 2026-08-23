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
        progress: [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100 }],
      },
      {
        id: "seednote", version: "1.0.0", digest: "b".repeat(64),
        agent: { name: "seednote" }, bindings: { task_types: ["seednote", "viral_analysis"] },
        runtime: { profile: "seednote", adapter: "standard", max_turns: 20 },
        progress: [
          { id: "research", title: "Research", active_percent: 5, complete_percent: 25 },
          { id: "writing", title: "Writing", active_percent: 35, complete_percent: 80 },
          { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100, required_artifacts: ["output/content.md", "output/image-plan.md"] },
        ],
        progress_by_task_type: {
          viral_analysis: [
            { id: "research", title: "Research", active_percent: 5, complete_percent: 80 },
            { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100, required_artifacts: ["output/source-analysis.md", "output/viral-template.json"] },
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
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "1.2.3",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article",
      "--runtime-adapter", "standard", "--article-with-cover=false",
    ], { CLAUDE_PLUGIN_ROOT: plugin });

    expect(config).toMatchObject({
      taskID: "task-1", executionID: "execution-local-1", taskType: "article", maxTurns: 60,
      agentFlag: "anban:article", articleWithCover: false,
      articleWithContentImages: true,
      agentPack: {
        id: "article",
        progress: [{ id: "research", active_percent: 10, complete_percent: 100 }],
      },
    });
  });

  test("rejects a desktop pack identity that differs from the runtime catalog", async () => {
    const { root, plugin } = await localFixture();
    expect(parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "9.9.9",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article", "--runtime-adapter", "standard",
    ], { CLAUDE_PLUGIN_ROOT: plugin })).rejects.toThrow("frozen Agent Pack identity");
  });

  test("resolves task-specific Seednote progress contracts for local runs", async () => {
    const { root, plugin } = await localFixture();
    const baseArgs = [
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--workspace", root,
    ];

    const viral = await parseLocalConfig([...baseArgs, "--task-type", "viral_analysis"], { CLAUDE_PLUGIN_ROOT: plugin });
    expect(viral.agentPack.progress.map((stage) => stage.id)).toEqual(["research", "delivery"]);
    expect(viral.agentPack.progress.at(-1)!.required_artifacts).toEqual(["output/source-analysis.md", "output/viral-template.json"]);

    const normal = await parseLocalConfig([...baseArgs, "--task-type", "seednote"], { CLAUDE_PLUGIN_ROOT: plugin });
    expect(normal.agentPack.progress.map((stage) => stage.id)).toEqual(["research", "writing", "delivery"]);
    expect(normal.agentPack.progress.at(-1)!.required_artifacts).toEqual(["output/content.md", "output/image-plan.md"]);
  });

  test("requires a non-empty execution identity", async () => {
    const { root, plugin } = await localFixture();
    const base = [
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--task-type", "article", "--workspace", root,
    ];
    await expect(parseLocalConfig(base, { CLAUDE_PLUGIN_ROOT: plugin })).rejects.toThrow("--execution-id is required");
    await expect(parseLocalConfig([...base, "--execution-id", "   "], { CLAUDE_PLUGIN_ROOT: plugin })).rejects.toThrow("--execution-id is required");
  });

  test("constructs the local reporter with the claimed execution identity", async () => {
    const { root, plugin } = await localFixture();
    const config = await parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--execution-id", "execution-local-1", "--task-type", "article", "--workspace", root,
    ], { CLAUDE_PLUGIN_ROOT: plugin });
    const requests: unknown[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (_input, init) => {
      requests.push(JSON.parse(String(init?.body)));
      return new Response("{}", { status: 200 });
    };
    try {
      await createLocalReporter(config).stageProgress({ stage: "research", state: "active", title: "Research", progress_percent: 10 });
      expect(requests).toEqual([{ task_id: "task-1", execution_id: "execution-local-1", stage: "research", state: "active", title: "Research", description: "", progress_percent: 10 }]);
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
