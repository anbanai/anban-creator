import { test, expect } from "bun:test";
import {
  mkdtemp,
  mkdir,
  writeFile,
  readFile,
  rm,
  symlink,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  exportHypitProject,
  importHypitProject,
  validateHypitEnvironment,
} from "../src/hypit.js";
test("reserved environment cannot replace runner controls; provider secrets permitted", () => {
  expect(() => validateHypitEnvironment({ PATH: "/bad" })).toThrow();
  expect(() =>
    validateHypitEnvironment({ NODE_OPTIONS: "--import bad" }),
  ).toThrow();
  expect(() =>
    validateHypitEnvironment({ ANBAN_HYPIT_ROOT: "/bad" }),
  ).toThrow();
  expect(() =>
    validateHypitEnvironment({ PROVIDER_API_KEY: "trusted" }),
  ).not.toThrow();
});
test("bounded archive roundtrip preserves official results and excludes execution secrets", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-test-"));
  try {
    const project = join(root, "project");
    await mkdir(join(project, ".hypit/results/a"), { recursive: true });
    await mkdir(join(project, ".hypit/execution"), { recursive: true });
    await writeFile(join(project, "package.json"), "{}");
    await writeFile(join(project, ".hypit/results/a/result.json"), "{}");
    await writeFile(join(project, ".hypit/execution/token"), "secret");
    await writeFile(join(project, ".env"), "secret");
    await exportHypitProject(project, join(root, "project.zip"));
    await importHypitProject(join(root, "project.zip"), join(root, "copy"));
    expect(
      await readFile(join(root, "copy/.hypit/results/a/result.json"), "utf8"),
    ).toBe("{}");
    expect(readFile(join(root, "copy/.env"))).rejects.toThrow();
    await expect(
      importHypitProject(join(root, "project.zip"), join(root, "small"), {
        max_expanded_bytes: 1,
      }),
    ).rejects.toThrow();
    await symlink("/etc/passwd", join(project, "leak"));
    await expect(
      exportHypitProject(project, join(root, "bad.zip")),
    ).rejects.toThrow();
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

import { createWriteStream } from "node:fs";
import { pipeline } from "node:stream/promises";
import { spawnSync } from "node:child_process";
import yazl from "yazl";
import {
  finalizeHypit,
  prepareHypitWorkspace,
  runHypitCommand,
  cleanupHypit,
  validateHypitReferences,
} from "../src/hypit.js";
import { preflightBootstrapFiles } from "../src/bootstrap.js";
import { materializeBootstrapFiles } from "../src/workspace.js";
import { createHash } from "node:crypto";

test("ZIP rejects traversal, duplicates, private paths and leaves no partial project", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-zip-"));
  try {
    for (const [label, names] of [
      ["traversal", ["good"]],
      ["duplicate", ["A.json", "a.json"]],
      ["private", [".env"]],
    ] as const) {
      const zip = new yazl.ZipFile();
      for (const name of names) zip.addBuffer(Buffer.from("data"), name);
      zip.end();
      const archive = join(root, label + ".zip");
      await pipeline(zip.outputStream, createWriteStream(archive));
      if (label === "traversal") {
        const bytes = await readFile(archive);
        await writeFile(
          archive,
          Buffer.from(
            bytes.toString("latin1").replaceAll("good", "../x"),
            "latin1",
          ),
        );
      }
      await expect(
        importHypitProject(archive, join(root, label)),
      ).rejects.toThrow();
      await expect(readFile(join(root, label, "data"))).rejects.toThrow();
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
test("large bootstrap scope and streaming digest mismatch rollback", async () => {
  const asset = {
    path: "project/assets/a.mp4",
    mode: 0o644,
    download_url: "https://example.test/a",
    expected_size: 70000000,
    max_bytes: 268435456,
    content_sha256: "a".repeat(64),
  };
  expect(() => preflightBootstrapFiles([asset], "hypit")).not.toThrow();
  expect(() => preflightBootstrapFiles([asset], "article")).toThrow();
  expect(() =>
    preflightBootstrapFiles(
      [{ ...asset, path: "runtime-profile.json" }],
      "hypit",
    ),
  ).toThrow();
  const root = await mkdtemp(join(tmpdir(), "hypit-stream-"));
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(Buffer.from("first"));
            controller.enqueue(Buffer.from("second"));
            controller.close();
          },
        }),
      );
    await expect(
      materializeBootstrapFiles(
        root,
        [
          { path: "first.json", mode: 0o644, text: "{}" },
          { ...asset, expected_size: 11, max_bytes: 32 },
        ],
        undefined,
        "hypit",
      ),
    ).rejects.toThrow("SHA-256");
    await expect(readFile(join(root, "first.json"))).rejects.toThrow();
    await materializeBootstrapFiles(
      root,
      [
        {
          ...asset,
          expected_size: 11,
          max_bytes: 32,
          content_sha256: createHash("sha256")
            .update("firstsecond")
            .digest("hex"),
        },
      ],
      undefined,
      "hypit",
    );
    expect(await readFile(join(root, asset.path), "utf8")).toBe("firstsecond");
  } finally {
    globalThis.fetch = original;
    await rm(root, { recursive: true, force: true });
  }
});
test("fresh managed project has independent package and rejects runtime data outside project", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-init-"));
  try {
    await writeFile(join(root, "input.json"), "{}");
    await writeFile(
      join(root, "runtime-profile.json"),
      JSON.stringify({
        format: "hypit.runtime-local@1",
        dataRoot: join(root, "project/.hypit/execution"),
      }),
    );
    await prepareHypitWorkspace(root, process.env);
    expect(
      JSON.parse(await readFile(join(root, "project/package.json"), "utf8"))
        .private,
    ).toBe(true);
    await writeFile(
      join(root, "runtime-profile.json"),
      JSON.stringify({
        format: "hypit.runtime-local@1",
        dataRoot: "/tmp/shared",
      }),
    );
    await expect(prepareHypitWorkspace(root, process.env)).rejects.toThrow();
    await writeFile(
      join(root, "project/leak.json"),
      JSON.stringify({ path: "file:///etc/passwd" }),
    );
    await expect(
      validateHypitReferences(join(root, "project")),
    ).rejects.toThrow();
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
const hasMedia =
  spawnSync("ffmpeg", ["-version"]).status === 0 &&
  spawnSync("ffprobe", ["-version"]).status === 0;
test.skipIf(!hasMedia)(
  "real ffmpeg media decode and official CLI check/plan gate produce runner-owned delivery",
  async () => {
    const root = await mkdtemp(join(tmpdir(), "hypit-media-"));
    try {
      await mkdir(join(root, "output"));
      await mkdir(join(root, "project/productions/main/runs"), {
        recursive: true,
      });
      await writeFile(join(root, "project/package.json"), "{}");
      await writeFile(
        join(root, "project/productions/main/runs/main.svrun"),
        "fixture",
      );
      await writeFile(
        join(root, "input.json"),
        JSON.stringify({
          preferences: { duration_seconds: 0.4, aspect_ratio: "1:1" },
          runtime: {
            image: "registry.example/hypit@sha256:" + "a".repeat(64),
            agent_pack_id: "hypit",
            agent_pack_version: "1.2.3",
            agent_pack_digest: "b".repeat(64),
          },
        }),
      );
      await writeFile(
        join(root, "output/semantic-report.json"),
        '{"passed":true}',
      );
      await writeFile(join(root, "output/delivery-manifest.json"), "{}");
      await writeFile(
        join(root, "runtime-profile.json"),
        JSON.stringify({
          format: "hypit.runtime-local@1",
          endpoints: {
            native: { credential: { store: "env", key: "HYPIT_AUTH" } },
          },
        }),
      );
      await runHypitCommand(
        "ffmpeg",
        [
          "-v",
          "error",
          "-f",
          "lavfi",
          "-i",
          "color=c=blue:s=64x64:d=0.4",
          "-c:v",
          "libx264",
          "-pix_fmt",
          "yuv420p",
          join(root, "output/final.mp4"),
        ],
        root,
        process.env,
      );
      await runHypitCommand(
        "ffmpeg",
        [
          "-v",
          "error",
          "-i",
          join(root, "output/final.mp4"),
          "-frames:v",
          "1",
          join(root, "output/cover.png"),
        ],
        root,
        process.env,
      );
      const commands: string[][] = [];
      const dependencies = {
        command: async (_c: string, args: string[]) => {
          commands.push(args);
          return '{"ok":true}';
        },
        revision: async () => "5a568f4be485ab5e735fe95533cd5f77a85c66ee",
      };
      const receipt = await finalizeHypit(
        root,
        process.env,
        undefined,
        dependencies,
      );
      const snapshot = await prepareHypitUpload(root, [], receipt);
      expect(
        await readFile(join(snapshot.workspace, "output/final.mp4")),
      ).toEqual(await readFile(join(root, "output/final.mp4")));
      await snapshot.dispose();
      const report = JSON.parse(
        await readFile(join(root, "output/quality-report.json"), "utf8"),
      );
      expect(report.passed).toBe(true);
      expect(report.media.width).toBe(64);
      expect(commands.map((c) => c[0])).toEqual(["check", "plan"]);
      expect(commands[1]).toContain("--runtime");
      await writeFile(join(root, "project/notes.txt"), "native-auth-sensitive");
      await expect(
        finalizeHypit(
          root,
          { ...process.env, HYPIT_AUTH: "native-auth-sensitive" },
          undefined,
          dependencies,
        ),
      ).rejects.toThrow("credential");
      await rm(join(root, "project/notes.txt"));
      await importHypitProject(
        join(root, "output/project.zip"),
        join(root, "copy"),
      );
      expect(await readFile(join(root, "copy/package.json"), "utf8")).toBe(
        "{}",
      );
      const template = JSON.parse(
        await readFile(
          join(root, "copy/restore/profile.template.json"),
          "utf8",
        ),
      );
      expect(template.format).toBe("hypit.runtime-local@1");
      expect(template.endpoints.native.credential).toEqual({
        store: "env",
        key: "HYPIT_AUTH",
      });
      const instructions = await readFile(
        join(root, "copy/restore/README.md"),
        "utf8",
      );
      expect(instructions).toContain("/workspace/project");
      expect(instructions).toContain("--offline");
      expect(instructions).toContain("--bin-links=false");
      expect(instructions).toContain("HYPIT_AUTH");
      expect(instructions).not.toContain("native-auth-sensitive");
      const manifest = JSON.parse(
        await readFile(join(root, "output/project.json"), "utf8"),
      );
      expect(manifest.runtime.image).toBe(
        "registry.example/hypit@sha256:" + "a".repeat(64),
      );
      expect(manifest.runtime.agent_pack_digest).toBe("b".repeat(64));
      await writeFile(
        join(root, "input.json"),
        JSON.stringify({
          preferences: { duration_seconds: 10, aspect_ratio: "1:1" },
        }),
      );
      await expect(
        finalizeHypit(root, process.env, undefined, dependencies),
      ).rejects.toThrow("duration");
      await writeFile(
        join(root, "input.json"),
        JSON.stringify({
          preferences: { duration_seconds: 0.4, aspect_ratio: "16:9" },
        }),
      );
      await expect(
        finalizeHypit(root, process.env, undefined, dependencies),
      ).rejects.toThrow("aspect");
      await writeFile(
        join(root, "input.json"),
        JSON.stringify({
          preferences: { duration_seconds: 0.4, aspect_ratio: "1:1" },
        }),
      );
      await expect(
        finalizeHypit(root, process.env, undefined, dependencies, [], {
          profile: { format: "hypit.runtime-local@1" },
          input: { preferences: { duration_seconds: 10, aspect_ratio: "1:1" } },
        }),
      ).rejects.toThrow("duration");
      const lateWrite = {
        ...dependencies,
        command: async (_command: string, args: string[]) => {
          if (args[0] === "check")
            await writeFile(
              join(root, "project/debug.dump"),
              "late-provider-secret",
            );
          return '{"ok":true}';
        },
      };
      await expect(
        finalizeHypit(
          root,
          { ...process.env, OPAQUE: "late-provider-secret" },
          undefined,
          lateWrite,
          ["OPAQUE"],
        ),
      ).rejects.toThrow("credential");
      await rm(join(root, "project/debug.dump"));
      await writeFile(
        join(root, "output/project.zip"),
        "changed-after-verification",
      );
      await expect(prepareHypitUpload(root, [], receipt)).rejects.toThrow(
        "changed",
      );
      await writeFile(join(root, "output/final.mp4"), "corrupt");
      await expect(
        finalizeHypit(root, process.env, undefined, dependencies),
      ).rejects.toThrow();
      expect(
        JSON.parse(
          await readFile(join(root, "output/quality-report.json"), "utf8"),
        ).passed,
      ).toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  },
);
test("official cleanup cancels known build then stops worker and external programs", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-cleanup-"));
  try {
    await mkdir(join(root, "project"));
    const log = join(root, "calls");
    await writeFile(
      join(root, "hypit"),
      `#!/bin/sh\nprintf '%s\\n' "$*" >> '${log}'\nif [ "$1" = activity ]; then printf '{"builds":[{"id":"build-1"}]}'; fi\n`,
      { mode: 0o755 },
    );
    await cleanupHypit(root, {
      ...process.env,
      PATH: root + ":" + process.env.PATH,
    });
    const calls = await readFile(log, "utf8");
    expect(calls).toContain("cancel build-1");
    expect(calls).toContain("runtime down");
    expect(calls).toContain("programs down");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("native Result dependencies are included and immutable distribution dependency is allowed", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-refs-"));
  try {
    await mkdir(join(root, ".hypit/results/2026-09-21/build-a"), {
      recursive: true,
    });
    await mkdir(join(root, "assets"));
    await writeFile(join(root, "assets/ref.mp4"), "fixture");
    await writeFile(
      join(root, "package.json"),
      JSON.stringify({ dependencies: { "@hypit/hypit": "file:/opt/hypit" } }),
    );
    await writeFile(
      join(root, ".hypit/results/2026-09-21/build-a/result.json"),
      JSON.stringify({
        resource: {
          kind: "external-file",
          uri: "file:///workspace/project/assets/ref.mp4",
        },
      }),
    );
    await symlink("/opt/hypit/node_modules", join(root, "node_modules"));
    await validateHypitReferences(root);
    await exportHypitProject(
      root,
      join(root, "../" + root.split("/").pop() + ".zip"),
    );
    await writeFile(
      join(root, ".hypit/results/2026-09-21/build-a/result.json"),
      JSON.stringify({
        resource: { kind: "build-file", build: "missing", path: "media.mp4" },
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow("forwarded");
    await writeFile(
      join(root, ".hypit/results/2026-09-21/build-a/result.json"),
      "{}",
    );
    await writeFile(
      join(root, "notes.txt"),
      "do not archive super-secret-value",
    );
    await expect(
      validateHypitReferences(root, ["super-secret-value"]),
    ).rejects.toThrow("credential");
  } finally {
    await rm(root + ".zip", { force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("same-task resume preserves edited project and restores only offline dependencies", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-resume-"));
  try {
    await mkdir(join(root, "project"));
    await mkdir(join(root, "bin"));
    await writeFile(
      join(root, "project/package.json"),
      '{"name":"edited-project"}',
    );
    await writeFile(
      join(root, "input.json"),
      '{"project_archive_path":".anban-creator/project.zip"}',
    );
    await writeFile(
      join(root, "runtime-profile.json"),
      JSON.stringify({
        format: "hypit.runtime-local@1",
        dataRoot: join(root, "project/.hypit/execution"),
        endpoints: {
          provider: { credential: { store: "env", key: "HYPIT_AUTH" } },
        },
      }),
    );
    const calls = join(root, "calls");
    for (const command of ["npm", "hypit"])
      await writeFile(
        join(root, "bin", command),
        `#!/bin/sh\nprintf '%s:%s:%s\\n' '${command}' "$*" "$HYPIT_AUTH" >> '${calls}'\nprintf '{"ok":true}'\n`,
        { mode: 0o755 },
      );
    await prepareHypitWorkspace(
      root,
      {
        ...process.env,
        PATH: join(root, "bin") + ":" + process.env.PATH,
        HYPIT_AUTH: "trusted-key",
      },
      undefined,
      true,
    );
    expect(await readFile(join(root, "project/package.json"), "utf8")).toBe(
      '{"name":"edited-project"}',
    );
    const log = await readFile(calls, "utf8");
    expect(log).toContain("--offline");
    expect(log).toContain("--bin-links=false");
    expect(log.split("\n")[0]).not.toContain("trusted-key");
    expect(log).toContain("hypit:check");
    expect(log).toContain("hypit:plan");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test.each([true, false])(
  "archive overlap accepts only identical regular bootstrap bytes: %s",
  async (identical) => {
    const root = await mkdtemp(join(tmpdir(), "hypit-overlap-"));
    try {
      await mkdir(join(root, "original/assets"), { recursive: true });
      await writeFile(join(root, "original/package.json"), "{}");
      await writeFile(join(root, "original/assets/reference.mp4"), "original");
      await mkdir(join(root, ".anban-creator"));
      await exportHypitProject(
        join(root, "original"),
        join(root, ".anban-creator/project.zip"),
      );
      await mkdir(join(root, "project/assets"), { recursive: true });
      await writeFile(
        join(root, "project/assets/reference.mp4"),
        identical ? "original" : "changed",
      );
      await writeFile(
        join(root, "input.json"),
        JSON.stringify({ project_archive_path: ".anban-creator/project.zip" }),
      );
      await writeFile(
        join(root, "runtime-profile.json"),
        JSON.stringify({
          format: "hypit.runtime-local@1",
          dataRoot: join(root, "project/.hypit/execution"),
        }),
      );
      await mkdir(join(root, "bin"));
      for (const name of ["hypit", "npm"])
        await writeFile(
          join(root, "bin", name),
          "#!/bin/sh\nprintf '{\"ok\":true}'\n",
          { mode: 0o755 },
        );
      const action = prepareHypitWorkspace(root, {
        ...process.env,
        PATH: join(root, "bin") + ":" + process.env.PATH,
      });
      if (identical) await action;
      else await expect(action).rejects.toThrow();
      expect(
        await readFile(join(root, "project/assets/reference.mp4"), "utf8"),
      ).toBe(identical ? "original" : "changed");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  },
);

import {
  prepareHypitUpload,
  hypitExecutionBudget,
  prepareHypitToolHome,
} from "../src/hypit.js";
test("prepared tool home seeds only image caches, remains writable and rejects escaping links", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-home-"));
  try {
    const source = join(root, "image");
    await mkdir(join(source, ".cache/hyperframes"), { recursive: true });
    await writeFile(join(source, ".cache/hyperframes/browser"), "prepared");
    await writeFile(join(source, ".env"), "secret");
    await prepareHypitToolHome(
      root,
      { HOME: join(root, ".anban-runtime-home") },
      source,
    );
    expect(
      await readFile(
        join(root, ".anban-runtime-home/.cache/hyperframes/browser"),
        "utf8",
      ),
    ).toBe("prepared");
    await expect(
      readFile(join(root, ".anban-runtime-home/.env")),
    ).rejects.toThrow();
    await writeFile(
      join(root, ".anban-runtime-home/.cache/hyperframes/browser"),
      "working",
    );
    await prepareHypitToolHome(
      root,
      { HOME: join(root, ".anban-runtime-home") },
      source,
    );
    expect(
      await readFile(
        join(root, ".anban-runtime-home/.cache/hyperframes/browser"),
        "utf8",
      ),
    ).toBe("working");
    await mkdir(join(source, ".cache/node"), { recursive: true });
    await symlink("/etc/passwd", join(source, ".cache/node/bad"));
    await expect(
      prepareHypitToolHome(
        root,
        { HOME: join(root, ".anban-runtime-home") },
        source,
      ),
    ).rejects.toThrow("link");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("pnpm immutable image links fail before invoking a package manager", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-pnpm-"));
  try {
    await mkdir(join(root, "project"));
    await writeFile(
      join(root, "project/package.json"),
      JSON.stringify({ dependencies: { "@hypit/hypit": "file:/opt/hypit" } }),
    );
    await writeFile(join(root, "project/pnpm-lock.yaml"), "lockfileVersion: 9");
    await writeFile(join(root, "input.json"), "{}");
    await writeFile(
      join(root, "runtime-profile.json"),
      JSON.stringify({
        format: "hypit.runtime-local@1",
        dataRoot: join(root, "project/.hypit/execution"),
      }),
    );
    await expect(
      prepareHypitWorkspace(root, { PATH: "/nonexistent" }, undefined, true),
    ).rejects.toThrow("matching runtime");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("all archived byte formats are credential scanned across chunks", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-bytes-"));
  try {
    await mkdir(join(root, "project"));
    const secret = "opaque-provider-auth";
    await writeFile(
      join(root, "project/execution.jsonl"),
      Buffer.concat([Buffer.alloc(65530, 65), Buffer.from(secret)]),
    );
    await expect(
      validateHypitReferences(join(root, "project"), [secret]),
    ).rejects.toThrow("credential");
    await expect(
      exportHypitProject(
        join(root, "project"),
        join(root, "out.zip"),
        {},
        undefined,
        [secret],
      ),
    ).rejects.toThrow("credential");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
test("native value documents and forwarded output names/cycles must resolve", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-value-"));
  try {
    const result = join(root, ".hypit/results/2026-09-21/build-a");
    await mkdir(result, { recursive: true });
    await writeFile(
      join(result, "result.json"),
      JSON.stringify({
        outputs: {
          final: { value: { kind: "value", path: "missing-value.json" } },
        },
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow();
    await writeFile(
      join(result, "result.json"),
      JSON.stringify({
        outputs: {
          final: {
            value: {
              kind: "build-output",
              build: "build-a",
              output: "missing",
            },
          },
        },
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow();
    await writeFile(
      join(result, "result.json"),
      JSON.stringify({
        outputs: {
          final: {
            value: { kind: "build-output", build: "build-a", output: "final" },
          },
        },
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow("cycle");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("execution budget validates frozen minutes and caps by original deadline", () => {
  expect(hypitExecutionBudget({ limits: { timeout_minutes: 2 } }, 0)).toBe(
    120000,
  );
  expect(
    hypitExecutionBudget(
      {
        limits: { timeout_minutes: 2 },
        execution_deadline: "1970-01-01T00:01:00Z",
      },
      0,
    ),
  ).toBe(60000);
  expect(() =>
    hypitExecutionBudget({ limits: { timeout_minutes: 91 } }, 0),
  ).toThrow();
  expect(() =>
    hypitExecutionBudget({ execution_deadline: "1970-01-01T00:00:00Z" }, 1),
  ).toThrow();
});

test("native composite documents with non-json extension validate nested resource closure", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-composite-"));
  try {
    const dir = join(root, ".hypit/results/2026-09-21/build-a");
    await mkdir(dir, { recursive: true });
    await writeFile(
      join(dir, "result.json"),
      JSON.stringify({
        outputs: {
          final: { value: { kind: "value", path: "composite.data" } },
        },
      }),
    );
    await writeFile(
      join(dir, "composite.data"),
      JSON.stringify({
        format: "hypit.result-value@1",
        value: { media: null },
        resources: [
          { at: ["media"], file: { kind: "build-file", path: "missing.mp4" } },
        ],
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow(
      "forwarded file",
    );
    await writeFile(join(dir, "missing.mp4"), "media");
    await validateHypitReferences(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
test("upload receipt cannot be used to follow a swapped output symlink", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-swap-"));
  try {
    await mkdir(join(root, "output"));
    await writeFile(join(root, "outside"), "sensitive");
    await symlink(join(root, "outside"), join(root, "output/final.mp4"));
    const receipt = Object.fromEntries(
      [
        "final.mp4",
        "cover.png",
        "project.json",
        "project.zip",
        "delivery-manifest.json",
        "quality-report.json",
      ].map((name) => [name, "a".repeat(64)]),
    );
    await expect(prepareHypitUpload(root, [], receipt)).rejects.toThrow(
      "snapshot",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("composite domain objects are data while native resource bindings still validate", async () => {
  const root = await mkdtemp(join(tmpdir(), "hypit-domain-"));
  try {
    const dir = join(root, ".hypit/results/2026-09-21/build-a");
    await mkdir(dir, { recursive: true });
    await writeFile(
      join(dir, "result.json"),
      JSON.stringify({
        format: "hypit.build-result@1",
        outputs: { final: { value: { kind: "value", path: "data.json" } } },
      }),
    );
    const value = {
      kind: "value",
      path: "ordinary-domain-string",
      nested: {
        kind: "build-output",
        build: "nonexistent-domain-name",
        output: "domain",
      },
      example: { kind: "external-file", uri: "file:///not-a-real-reference" },
    };
    await writeFile(
      join(dir, "data.json"),
      JSON.stringify({ format: "hypit.result-value@1", value, resources: [] }),
    );
    await validateHypitReferences(root);
    await writeFile(
      join(dir, "data.json"),
      JSON.stringify({
        format: "hypit.result-value@1",
        value,
        resources: [
          { at: ["media"], file: { kind: "build-file", path: "missing.mp4" } },
        ],
      }),
    );
    await expect(validateHypitReferences(root)).rejects.toThrow(
      "forwarded file",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
