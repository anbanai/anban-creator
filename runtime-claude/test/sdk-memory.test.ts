import { expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

test("real SDK writes and recalls project memory, and omits it during policy recovery", async () => {
  const root = await mkdtemp(join(tmpdir(), "anban-sdk-memory-"));
  // A separate process prevents runner.test.ts's SDK mock from replacing query().
  // Do not inherit developer credentials, proxy configuration, or Claude settings.
  const child = spawn(process.execPath, [fileURLToPath(new URL("./sdk-memory.fixture.ts", import.meta.url)), root], {
    detached: true,
    env: {
      PATH: process.env.PATH,
      HOME: join(root, "home"),
      TMPDIR: tmpdir(),
      LANG: "en_US.UTF-8",
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
      CLAUDE_CODE_DISABLE_AUTO_MEMORY: "0",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout.on("data", (chunk) => { output += chunk; });
  child.stderr.on("data", (chunk) => { output += chunk; });
  const stop = () => {
    if (child.pid) {
      try { process.kill(-child.pid, "SIGKILL"); } catch { /* Already exited. */ }
    }
  };
  const deadline = setTimeout(stop, 55_000);
  try {
    const exitCode = await new Promise<number | null>((resolve, reject) => {
      child.on("error", reject);
      child.on("close", resolve);
    });
    expect(exitCode, output).toBe(0);
    expect(output).toContain("SDK project memory write and fresh-session recall passed\n");
  } finally {
    clearTimeout(deadline);
    stop();
    await rm(root, { recursive: true, force: true });
  }
}, 60_000);
