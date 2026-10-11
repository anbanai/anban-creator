import { describe, expect, test } from "bun:test";
import { readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const runtimeDir = join(dirname(fileURLToPath(import.meta.url)), "..");
const packageJSON = JSON.parse(readFileSync(join(runtimeDir, "package.json"), "utf8")) as {
  dependencies: Record<string, string>;
};
const lockJSON = JSON.parse(readFileSync(join(runtimeDir, "package-lock.json"), "utf8")) as {
  packages: Record<string, { resolved?: string; link?: boolean }>;
};

const sdkPackages = [
  "@deepseek-ai/dsh-sdk-client",
  "@deepseek-ai/dsh-sdk-protocol",
  "@deepseek-ai/dsh-sdk-jsonrpc-server",
] as const;

describe("vendored DSH interactive SDK contract", () => {
  test("keeps all interactive SDK packages local and versioned together", () => {
    const versions = new Set<string>();
    for (const name of sdkPackages) {
      const packageDir = join(runtimeDir, "vendor", ...name.split("/"));
      const packagePath = join(packageDir, "package.json");
      expect(existsSync(packagePath)).toBe(true);
      const metadata = JSON.parse(readFileSync(packagePath, "utf8")) as { name: string; version: string };
      expect(metadata.name).toBe(name);
      versions.add(metadata.version);
      expect(packageJSON.dependencies[name]).toBe(`file:vendor/${name}`);
      const lockEntry = lockJSON.packages[`node_modules/${name}`];
      expect(lockEntry).toMatchObject({ resolved: `vendor/${name}`, link: true });
      expect(existsSync(join(packageDir, "lib", "index.js"))).toBe(true);
    }
    expect(versions.size).toBe(1);
  });

  test("contains the message-free start and structured answer handlers", () => {
    const server = readFileSync(join(runtimeDir, "vendor/@deepseek-ai/dsh-sdk-jsonrpc-server/lib/index.js"), "utf8");
    const protocol = readFileSync(join(runtimeDir, "vendor/@deepseek-ai/dsh-sdk-protocol/lib/index.js"), "utf8");
    expect(server).toContain("session/start");
    expect(server).toContain("session/answer");
    expect(protocol).toContain("JsonRpcLineTransport");
  });
});
