import { describe, expect, test } from "bun:test";
import { readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const runtimeDir = join(dirname(fileURLToPath(import.meta.url)), "..");
const packageJSON = JSON.parse(readFileSync(join(runtimeDir, "package.json"), "utf8")) as {
  dependencies: Record<string, string>;
  allowScripts?: Record<string, boolean>;
};
const lockJSON = JSON.parse(readFileSync(join(runtimeDir, "package-lock.json"), "utf8")) as {
  packages: Record<string, { resolved?: string; link?: boolean; version?: string }>;
};

const sdkPackages = [
  "@deepseek-ai/dsh-agent-loop",
  "@deepseek-ai/dsh-sdk-client",
  "@deepseek-ai/dsh-sdk-protocol",
  "@deepseek-ai/dsh-sdk-jsonrpc-server",
] as const;

describe("vendored DSH interactive SDK contract", () => {
  test("allows only pinned install scripts required by the Linux DSH runtime", () => {
    const approvals = packageJSON.allowScripts ?? {};
    expect(approvals["@deepseek-ai/dsh-subprocess-local@0.2.1-alpha.4"]).toBe(true);
    expect(approvals["node-pty@1.2.0-beta.15"]).toBe(true);
    expect(approvals["koffi@3.1.1"]).toBe(true);
    expect(approvals["@google/genai"]).toBe(false);
    expect(approvals.protobufjs).toBe(false);
  });

  test("keeps vulnerable ZIP parsing versions out of the DSH dependency tree", () => {
    const fflatePackages = Object.entries(lockJSON.packages)
      .filter(([path]) => path === "node_modules/fflate" || path.endsWith("/node_modules/fflate"));
    expect(fflatePackages.length).toBeGreaterThan(0);
    expect(fflatePackages.every(([, metadata]) => metadata.version === "0.8.3")).toBe(true);
  });

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
      const vendorEntry = lockJSON.packages[`vendor/${name}`];
      expect(vendorEntry).toMatchObject({ version: metadata.version });
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

  test("ships the pinned transport-cause dependency patch", () => {
    const patch = readFileSync(join(runtimeDir, "scripts/patch-dsh-dependencies.mjs"), "utf8");
    expect(patch).toContain("Anban transport-cause preservation patch");
    expect(patch).toContain("unsupported @deepseek-ai/dsh-llm alpha.4 layout");
  });

  test("keeps malformed provider tool input recoverable", () => {
    const adapter = readFileSync(
      join(runtimeDir, "node_modules/@deepseek-ai/dsh-llm-deepseek/lib/index.js"),
      "utf8",
    );
    expect(adapter).toContain("Anban malformed-tool-input recovery patch");
    expect(adapter).toContain("ToolRuntime validates malformed provider arguments");
    expect(adapter).not.toContain('return malformed("tool input is invalid JSON")');
  });
});
