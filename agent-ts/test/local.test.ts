import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { buildLocalPrompt, parseLocalConfig } from "../src/local.js";

async function localFixture() {
  const root = await mkdtemp(join(tmpdir(), "anban-local-"));
  const plugin = join(root, "plugin");
  await mkdir(plugin);
  await writeFile(join(plugin, "agent-pack-catalog.json"), JSON.stringify({
    packs: [{
      id: "article", version: "1.2.3", digest: "a".repeat(64),
      agent: { name: "article" }, bindings: { task_types: ["article"] },
      runtime: { profile: "article", adapter: "standard", max_turns: 60 },
      progress: [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100 }],
    }],
  }));
  return { root, plugin };
}

describe("parseLocalConfig", () => {
  test("preserves the desktop run contract and resolves the frozen pack", async () => {
    const { root, plugin } = await localFixture();
    const config = await parseLocalConfig([
      "run", "--server-url", "https://creator.example.com", "--api-key", "user-key",
      "--task-id", "task-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "1.2.3",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article",
      "--runtime-adapter", "standard", "--article-with-cover=false",
    ], { CLAUDE_PLUGIN_ROOT: plugin });

    expect(config).toMatchObject({
      taskID: "task-1", taskType: "article", maxTurns: 60,
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
      "--task-id", "task-1", "--task-type", "article", "--workspace", root,
      "--agent-pack-id", "article", "--agent-pack-version", "9.9.9",
      "--agent-pack-digest", "a".repeat(64), "--runtime-profile", "article", "--runtime-adapter", "standard",
    ], { CLAUDE_PLUGIN_ROOT: plugin })).rejects.toThrow("frozen Agent Pack identity");
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
