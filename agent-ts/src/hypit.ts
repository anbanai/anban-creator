import { createHash } from "node:crypto";
import { constants, createReadStream, createWriteStream } from "node:fs";
import {
  chmod,
  cp,
  realpath,
  lstat,
  open,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname, join, posix, resolve, relative, isAbsolute } from "node:path";
import { spawn } from "node:child_process";
import { pipeline } from "node:stream/promises";
import { Readable, Transform } from "node:stream";
import { tmpdir } from "node:os";
import yauzl from "yauzl";
import yazl from "yazl";
import { cleanBootstrapPath } from "./bootstrap.js";

async function readJSON(path: string): Promise<Record<string, any>> {
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.size > 2 * 1024 * 1024)
      throw new Error("invalid Hypit JSON artifact");
    const value = JSON.parse(await handle.readFile("utf8"));
    if (!value || typeof value !== "object" || Array.isArray(value))
      throw new Error("invalid Hypit JSON object");
    return value;
  } finally {
    await handle.close();
  }
}
async function atomicJSON(path: string, value: unknown): Promise<void> {
  const stage = await mkdtemp(join(dirname(path), ".hypit-report-"));
  try {
    const file = join(stage, "value.json");
    await writeFile(file, JSON.stringify(value, null, 2), {
      mode: 0o600,
      flag: "wx",
    });
    await rename(file, path);
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}

/** Freeze credential identity before authored project code can modify workspace files. */
export function hypitCredentialKeys(
  profile: unknown,
  trustedEnv: Record<string, string> = {},
): string[] {
  const keys = new Set(Object.keys(trustedEnv));
  const visit = (value: unknown): void => {
    if (!value || typeof value !== "object") return;
    if (Array.isArray(value)) {
      value.forEach(visit);
      return;
    }
    const record = value as Record<string, unknown>;
    if (
      record.store === "env" &&
      typeof record.key === "string" &&
      /^[A-Z][A-Z0-9_]*$/.test(record.key)
    )
      keys.add(record.key);
    Object.values(record).forEach(visit);
  };
  visit(profile);
  return [...keys];
}
async function identicalFiles(first: string, second: string): Promise<boolean> {
  const [a, b] = await Promise.all([lstat(first), lstat(second)]);
  if (
    !a.isFile() ||
    a.isSymbolicLink() ||
    !b.isFile() ||
    b.isSymbolicLink() ||
    a.size !== b.size
  )
    return false;
  const digest = async (path: string) => {
    const hash = createHash("sha256");
    for await (const chunk of createReadStream(path)) hash.update(chunk);
    return hash.digest("hex");
  };
  const hashes = await Promise.all([digest(first), digest(second)]);
  return hashes[0] === hashes[1];
}
async function mergeBootstrapAssets(
  project: string,
  imported: string,
): Promise<void> {
  for (const name of (await files(project, HYPIT_LIMITS)).filter((name) =>
    name.startsWith("assets/"),
  )) {
    const source = join(project, name),
      target = join(imported, name);
    try {
      await lstat(target);
      if (!(await identicalFiles(source, target)))
        throw new Error("bootstrap asset conflicts with archived project");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
      await mkdir(dirname(target), { recursive: true });
      await cp(source, target, { force: false, errorOnExist: true });
    }
  }
}

export const HYPIT_LIMITS = {
  max_project_bytes: 2147483648,
  max_expanded_bytes: 8589934592,
  max_project_files: 10000,
  max_video_bytes: 536870912,
  max_duration_seconds: 180,
  max_assets: 20,
  max_asset_bytes: 268435456,
  max_input_bytes: 536870912,
  timeout_minutes: 90,
};
type Limits = typeof HYPIT_LIMITS;
export function validateHypitEnvironment(env: Record<string, string>): void {
  if (Object.keys(env).length > 128)
    throw new Error("too many Hypit environment keys");
  for (const [key, value] of Object.entries(env)) {
    if (
      !/^[A-Z][A-Z0-9_]*$/.test(key) ||
      typeof value !== "string" ||
      !value ||
      value.length > 16384 ||
      /[\x00\r\n]/.test(value) ||
      /^(?:ANBAN_|ANTHROPIC_|CLAUDE_|CODEX_|NODE_|BUN_|LD_|DYLD_|PYTHON|npm_|NPM_|GIT_|SSH_|AWS_|GOOGLE_APPLICATION_CREDENTIALS$)/.test(
        key,
      ) ||
      [
        "PATH",
        "HOME",
        "SHELL",
        "PWD",
        "OLDPWD",
        "TMPDIR",
        "TMP",
        "TEMP",
        "ENV",
        "BASH_ENV",
        "ZDOTDIR",
        "IFS",
      ].includes(key)
    )
      throw new Error("unsafe Hypit environment key");
  }
}
function excluded(name: string): boolean {
  const parts = name.split("/");
  return (
    parts.some(
      (p) =>
        p === "node_modules" ||
        p === ".git" ||
        p === ".anban-creator" ||
        p === ".claude" ||
        [
          ".npmrc",
          ".netrc",
          ".ssh",
          ".aws",
          ".config",
          ".codex",
          ".credentials",
        ].includes(p) ||
        /\.(?:pem|key|p12|pfx)$/i.test(p) ||
        p === "execution" ||
        p === "cache" ||
        p === ".cache" ||
        p.startsWith(".env") ||
        /^(?:runtime-profile|hypit\.runtime|credentials|secrets)(?:\.|$)/i.test(
          p,
        ),
    ) ||
    (parts[0] === ".hypit" && parts.length > 1 && parts[1] !== "results")
  );
}
function quota(limits: Partial<Limits>): Limits {
  const out = { ...HYPIT_LIMITS };
  for (const key of Object.keys(out) as (keyof Limits)[]) {
    const v = limits[key];
    if (v !== undefined) {
      if (!Number.isSafeInteger(v) || v < 1 || v > out[key])
        throw new Error("invalid Hypit limit");
      out[key] = v;
    }
  }
  return out;
}
async function files(root: string, limits: Limits): Promise<string[]> {
  const rootInfo = await lstat(root);
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink())
    throw new Error("project root must be a real directory");
  const result: string[] = [];
  let bytes = 0;
  async function walk(relative: string) {
    for (const entry of await readdir(join(root, relative))) {
      const name = relative ? `${relative}/${entry}` : entry;
      cleanBootstrapPath(name);
      if (excluded(name)) continue;
      const stat = await lstat(join(root, name));
      if (stat.isSymbolicLink())
        throw new Error("project symbolic links forbidden");
      if (stat.isDirectory()) await walk(name);
      else if (stat.isFile()) {
        bytes += stat.size;
        if (
          bytes > limits.max_expanded_bytes ||
          result.length >= limits.max_project_files
        )
          throw new Error("project exceeds expanded quota");
        result.push(name);
      } else throw new Error("project special files forbidden");
    }
  }
  await walk("");
  return result;
}
function bounded(max: number): Transform {
  let bytes = 0;
  return new Transform({
    transform(chunk, _encoding, cb) {
      bytes += chunk.length;
      cb(bytes > max ? new Error("archive exceeds size quota") : null, chunk);
    },
  });
}
/** Read one stable regular file without following any workspace path links. */
async function guardedFile(
  root: string,
  name: string,
  maximum: number,
  secrets: readonly string[],
  signal?: AbortSignal,
) {
  cleanBootstrapPath(name);
  let parent = resolve(root);
  const rootInfo = await lstat(parent);
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink())
    throw new Error("unsafe snapshot root");
  for (const part of name.split("/").slice(0, -1)) {
    parent = join(parent, part);
    const info = await lstat(parent);
    if (!info.isDirectory() || info.isSymbolicLink())
      throw new Error("snapshot parent link forbidden");
  }
  const target = join(root, name);
  const before = await lstat(target);
  if (!before.isFile() || before.isSymbolicLink() || before.size > maximum)
    throw new Error("invalid snapshot file");
  const handle = await open(target, constants.O_RDONLY | constants.O_NOFOLLOW);
  const initial = await handle.stat();
  if (process.platform === "linux") {
    const openedPath = await realpath("/proc/self/fd/" + handle.fd);
    const rootPath = await realpath(root);
    if (!openedPath.startsWith(rootPath + "/")) {
      await handle.close();
      throw new Error("opened snapshot escapes root");
    }
  }
  if (
    initial.dev !== before.dev ||
    initial.ino !== before.ino ||
    initial.size !== before.size
  ) {
    await handle.close();
    throw new Error("snapshot changed during open");
  }
  const needles = secrets.filter(Boolean).map((value) => Buffer.from(value));
  const overlap = Math.max(0, ...needles.map((value) => value.length - 1));
  let tail = Buffer.alloc(0),
    size = 0;
  const hash = createHash("sha256");
  const reader = handle.createReadStream({
    autoClose: false,
    highWaterMark: 64 * 1024,
  });
  const scan = new Transform({
    transform(chunk: Buffer, _encoding, callback) {
      try {
        signal?.throwIfAborted();
        size += chunk.length;
        if (size > maximum) throw new Error("snapshot exceeds quota");
        const combined = Buffer.concat([tail, chunk]);
        if (needles.some((secret) => combined.includes(secret)))
          throw new Error("artifact includes a managed credential");
        tail = overlap
          ? combined.subarray(Math.max(0, combined.length - overlap))
          : Buffer.alloc(0);
        hash.update(chunk);
        callback(null, chunk);
      } catch (error) {
        callback(error as Error);
      }
    },
    flush(callback) {
      void (async () => {
        const [after, visible] = await Promise.all([
          handle.stat(),
          lstat(target),
        ]);
        if (
          !visible.isFile() ||
          visible.isSymbolicLink() ||
          after.size !== size ||
          after.size !== initial.size ||
          after.mtimeMs !== initial.mtimeMs ||
          after.ctimeMs !== initial.ctimeMs ||
          visible.dev !== initial.dev ||
          visible.ino !== initial.ino
        )
          throw new Error("snapshot changed while reading");
      })().then(
        () => callback(),
        (error) => callback(error, undefined as never),
      );
    },
  });
  scan.on("error", () => {});
  reader.on("error", (error) => scan.destroy(error));
  const close = () => {
    reader.destroy();
    void handle.close().catch(() => {});
  };
  scan.once("error", close);
  scan.once("end", close);
  scan.once("close", close);
  reader.pipe(scan);
  return { stream: scan, digest: () => hash.digest("hex") };
}
async function snapshotFile(
  root: string,
  name: string,
  destination: string | undefined,
  maximum: number,
  secrets: readonly string[],
  signal?: AbortSignal,
): Promise<string> {
  const file = await guardedFile(root, name, maximum, secrets, signal);
  if (destination) {
    await mkdir(dirname(destination), { recursive: true });
    await pipeline(
      file.stream,
      createWriteStream(destination, { flags: "wx", mode: 0o600 }),
      { signal },
    );
  } else
    for await (const _chunk of file.stream) {
      signal?.throwIfAborted();
    }
  return file.digest();
}

export async function exportHypitProject(
  project: string,
  destination: string,
  limits: Partial<Limits> = {},
  signal?: AbortSignal,
  secrets: readonly string[] = [],
): Promise<string> {
  const q = quota(limits);
  const names = await files(project, q);
  const zip = new yazl.ZipFile();
  zip.on("error", (error) => (zip.outputStream as Readable).destroy(error));
  const temp = destination + ".partial";
  let expandedBytes = 0;
  const archiveHash = createHash("sha256");
  const digestStream = new Transform({
    transform(chunk, _encoding, callback) {
      archiveHash.update(chunk);
      callback(null, chunk);
    },
  });
  try {
    for (const name of names) {
      zip.addReadStreamLazy(name, { mode: 0o100644 }, (callback) => {
        void guardedFile(
          project,
          name,
          q.max_expanded_bytes,
          secrets,
          signal,
        ).then(
          (file) => {
            file.stream.on("data", (chunk: Buffer) => {
              expandedBytes += chunk.length;
              if (expandedBytes > q.max_expanded_bytes)
                file.stream.destroy(
                  new Error("archive exceeds expanded quota"),
                );
            });
            file.stream.on("error", (error) =>
              (zip.outputStream as Readable).destroy(error),
            );
            callback(null, file.stream);
          },
          (error) => callback(error, undefined as never),
        );
      });
    }
    zip.end();
    await pipeline(
      zip.outputStream,
      bounded(q.max_project_bytes),
      digestStream,
      createWriteStream(temp, { flags: "wx", mode: 0o600 }),
      { signal },
    );
    await rename(temp, destination);
    return archiveHash.digest("hex");
  } finally {
    await rm(temp, { force: true });
  }
}
export async function importHypitProject(
  archive: string,
  destination: string,
  limits: Partial<Limits> = {},
  signal?: AbortSignal,
): Promise<void> {
  const q = quota(limits);
  const info = await lstat(archive);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    info.size > q.max_project_bytes
  )
    throw new Error("invalid project archive");
  const stage = await mkdtemp(join(dirname(destination), ".hypit-import-"));
  let zip: yauzl.ZipFile | undefined;
  try {
    zip = await new Promise<yauzl.ZipFile>((res, rej) =>
      yauzl.open(
        archive,
        { lazyEntries: true, strictFileNames: true, validateEntrySizes: true },
        (e, z) => (e ? rej(e) : res(z!)),
      ),
    );
    const seen = new Set<string>();
    let expanded = 0,
      count = 0;
    await new Promise<void>((res, rej) => {
      zip!.once("error", rej);
      zip!.once("end", res);
      zip!.on("entry", (entry: yauzl.Entry) => {
        void (async () => {
          signal?.throwIfAborted();
          const directory = entry.fileName.endsWith("/");
          const name = directory ? entry.fileName.slice(0, -1) : entry.fileName;
          cleanBootstrapPath(name);
          const mode = (entry.externalFileAttributes >>> 16) & 0xf000;
          if (mode !== 0 && mode !== 0x8000 && mode !== 0x4000)
            throw new Error("archive links/special entries forbidden");
          if (excluded(name))
            throw new Error("archive contains excluded private path");
          if (seen.has(name.toLowerCase()))
            throw new Error("duplicate archive path");
          seen.add(name.toLowerCase());
          expanded += entry.uncompressedSize;
          if (
            ++count > q.max_project_files ||
            expanded > q.max_expanded_bytes ||
            entry.generalPurposeBitFlag & 1
          )
            throw new Error("archive exceeds quota or is encrypted");
          const target = join(stage, name);
          await mkdir(directory ? target : dirname(target), {
            recursive: true,
          });
          if (!directory) {
            const stream = await new Promise<NodeJS.ReadableStream>((r, j) =>
              zip!.openReadStream(entry, (e, s) => (e ? j(e) : r(s!))),
            );
            await pipeline(
              stream,
              bounded(entry.uncompressedSize),
              createWriteStream(target, { flags: "wx", mode: 0o600 }),
              { signal },
            );
          }
          zip!.readEntry();
        })().catch(rej);
      });
      zip!.readEntry();
    });
    await validateHypitReferences(stage, [], signal);
    await rename(stage, destination);
  } finally {
    zip?.close();
    await rm(stage, { recursive: true, force: true });
  }
}
// Validate native Result dependency objects and authored source attributes, not prose examples.
export async function validateHypitReferences(
  project: string,
  secrets: string[] = [],
  signal?: AbortSignal,
): Promise<void> {
  const names = await files(project, HYPIT_LIMITS);
  for (const name of names)
    await snapshotFile(
      project,
      name,
      undefined,
      HYPIT_LIMITS.max_expanded_bytes,
      secrets,
      signal,
    );
  const included = new Set(names);
  const results = new Map<
    string,
    { path: string; value: Record<string, any> }
  >();
  for (const name of names.filter(
    (name) =>
      name.startsWith(".hypit/results/") && name.endsWith("/result.json"),
  ))
    results.set(posix.basename(posix.dirname(name)), {
      path: name,
      value: await readJSON(join(project, name)),
    });
  const valueDocuments = new Map<string, Record<string, any>>();
  for (const result of results.values()) {
    // Only Output.value is persistence metadata. A composite document's value
    // is arbitrary canonical domain data, even when it resembles a reference.
    for (const output of Object.values(result.value.outputs ?? {})) {
      const reference = (output as { value?: Record<string, unknown> })?.value;
      if (reference?.kind !== "value") continue;
      if (typeof reference.path !== "string")
        throw new Error("invalid Result value");
      cleanBootstrapPath(reference.path);
      const path = posix.join(posix.dirname(result.path), reference.path);
      if (!included.has(path))
        throw new Error("missing native Result value document");
      const doc = await readJSON(join(project, path));
      if (
        doc.format !== "hypit.result-value@1" ||
        !Array.isArray(doc.resources)
      )
        throw new Error("invalid native Result value document");
      valueDocuments.set(path, doc);
    }
  }
  const resolveOutput = (
    build: string,
    output: string,
    seen = new Set<string>(),
  ): void => {
    const key = build + "#" + output;
    if (seen.has(key)) throw new Error("Result forwarding cycle");
    seen.add(key);
    const result = results.get(build),
      entry = result?.value.outputs?.[output];
    if (!result || !entry) throw new Error("missing forwarded Result output");
    const value = entry.value;
    if (value?.kind === "build-output")
      resolveOutput(value.build, value.output, seen);
  };
  const requireLocal = (
    value: string,
    document: string,
    allowDirectory = false,
  ) => {
    if (value === "file:/opt/hypit" && document === "package.json") return;
    let local = value;
    if (local.startsWith("file:")) {
      const url = new URL(local);
      if (url.host && url.host !== "localhost")
        throw new Error("non-local file URI");
      local = decodeURIComponent(url.pathname);
    }
    if (local.startsWith("/")) {
      if (!local.startsWith("/workspace/project/"))
        throw new Error("project has unsafe external file reference");
      local = local.slice("/workspace/project/".length);
    } else local = posix.normalize(posix.join(posix.dirname(document), local));
    cleanBootstrapPath(local);
    if (
      excluded(local) ||
      (!included.has(local) &&
        !(allowDirectory && names.some((n) => n.startsWith(local + "/"))))
    )
      throw new Error("project reference is not an included regular file");
  };
  for (const name of names) {
    if (
      !valueDocuments.has(name) &&
      !/\.(?:json|svrun|svml|svs|md|txt|log|ts|tsx|js|mjs|html|css|yaml|yml)$/i.test(
        name,
      )
    )
      continue;
    const stat = await lstat(join(project, name));
    if (stat.size > 8 * 1024 * 1024)
      throw new Error("project reference document too large");
    const text = await readFile(join(project, name), "utf8");
    if (secrets.some((secret) => text.includes(secret)))
      throw new Error("project includes a managed credential");
    if (name === "restore/profile.template.json") continue; // Trusted native config may reference immutable image tools.
    if (!valueDocuments.has(name) && !/\.(?:json|svrun|svml|svs)$/i.test(name))
      continue;
    if (valueDocuments.has(name) || name.endsWith(".json")) {
      const value: unknown = valueDocuments.get(name) ?? JSON.parse(text);
      const visit = (item: unknown, key = ""): void => {
        if (!item || typeof item !== "object") {
          if (
            typeof item === "string" &&
            /^(?:file:|\/)/.test(item) &&
            /^(?:path|uri|url|src|source)$/.test(key)
          )
            requireLocal(item, name);
          return;
        }
        if (Array.isArray(item)) {
          for (const child of item) visit(child);
          return;
        }
        const record = item as Record<string, unknown>;
        if (record.format === "hypit.result-value@1") {
          if (!Array.isArray(record.resources))
            throw new Error("invalid native Result resource bindings");
          for (const binding of record.resources) {
            if (!binding || typeof binding !== "object" || !("file" in binding))
              throw new Error("invalid native Result resource binding");
            visit(binding.file);
          }
          return;
        }
        if (record.kind === "external-file") {
          if (typeof record.uri !== "string" || !record.uri.startsWith("file:"))
            throw new Error("Result external file must be a local URI");
          requireLocal(record.uri, name);
        }
        if (record.kind === "value") {
          if (typeof record.path !== "string")
            throw new Error("invalid Result value path");
          cleanBootstrapPath(record.path);
          const root = name.match(/^(\.hypit\/results\/[^/]+\/[^/]+\/)/)?.[1];
          if (!root || !included.has(root + record.path))
            throw new Error("missing native Result value document");
        }
        if (record.kind === "build-output") {
          if (
            typeof record.build !== "string" ||
            typeof record.output !== "string"
          )
            throw new Error("invalid forwarded Result output");
          resolveOutput(record.build, record.output);
        }
        if (record.kind === "build-file") {
          if (typeof record.path !== "string")
            throw new Error("invalid Result file");
          cleanBootstrapPath(record.path);
          const resultRoot =
            typeof record.build === "string"
              ? names
                  .find((n) => n.endsWith("/" + record.build + "/result.json"))
                  ?.slice(0, -"result.json".length)
              : name.match(/^(\.hypit\/results\/[^/]+\/[^/]+\/)/)?.[1];
          if (!resultRoot || !included.has(resultRoot + record.path))
            throw new Error("missing Result forwarded file");
        }
        if (
          record.kind === "build-output" &&
          typeof record.build === "string" &&
          !names.some((n) => n.endsWith("/" + record.build + "/result.json"))
        )
          throw new Error("missing forwarded Result");
        for (const [childKey, child] of Object.entries(record)) {
          if (record.kind === "build-file" && childKey === "path") continue;
          visit(child, childKey);
        }
      };
      visit(value);
      if (name === "package.json" && value && typeof value === "object")
        for (const section of [
          "dependencies",
          "devDependencies",
          "optionalDependencies",
        ]) {
          const deps = (value as Record<string, unknown>)[section];
          if (deps && typeof deps === "object")
            for (const spec of Object.values(deps))
              if (typeof spec === "string" && spec.startsWith("file:"))
                requireLocal(
                  spec === "file:/opt/hypit" ? spec : spec.slice(5),
                  name,
                  true,
                );
        }
    } else {
      for (const match of text.matchAll(
        /\b(?:source|src|uri)\s*=\s*["']([^"']+)["']/g,
      )) {
        const value = match[1]!;
        if (!/^(?:https?:|data:|@)/.test(value)) requireLocal(value, name);
      }
    }
  }
}
export async function runHypitCommand(
  command: string,
  args: string[],
  cwd: string,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal,
): Promise<string> {
  signal?.throwIfAborted();
  return await new Promise((res, rej) => {
    const child = spawn(command, args, {
      cwd,
      env,
      detached: process.platform !== "win32",
      stdio: ["ignore", "pipe", "pipe"],
    });
    let text = "",
      size = 0;
    const stop = () => {
      try {
        if (child.pid)
          process.kill(
            process.platform === "win32" ? child.pid : -child.pid,
            "SIGKILL",
          );
      } catch {}
    };
    const timeout = setTimeout(() => {
      stop();
      rej(new Error("verification command timed out"));
    }, 120000);
    const abort = () => {
      stop();
      rej(signal?.reason ?? new Error("cancelled"));
    };
    signal?.addEventListener("abort", abort, { once: true });
    child.stdout.on("data", (d: Buffer) => {
      size += d.length;
      if (size > 2 * 1024 * 1024) {
        stop();
        rej(new Error("verification output exceeds limit"));
      } else text += d.toString();
    });
    child.stderr.on("data", () => {});
    child.on("error", rej);
    child.on("close", (code) => {
      stop(); // Reap any remaining command-group descendants before snapshots.
      clearTimeout(timeout);
      signal?.removeEventListener("abort", abort);
      code === 0
        ? res(text)
        : rej(new Error(`verification command failed: ${command} (${code})`));
    });
  });
}
export async function prepareHypitToolHome(
  workspace: string,
  env: NodeJS.ProcessEnv,
  source = "/home/node",
): Promise<void> {
  if (!env.HOME || env.HOME === source) return;
  const home = resolve(workspace, ".anban-runtime-home");
  if (resolve(env.HOME) !== home)
    throw new Error("Hypit tool home must be the managed workspace home");
  const workspaceInfo = await lstat(workspace);
  if (!workspaceInfo.isDirectory() || workspaceInfo.isSymbolicLink())
    throw new Error("tool workspace must be a real directory");
  await mkdir(home, { recursive: true });
  const homeInfo = await lstat(home);
  if (!homeInfo.isDirectory() || homeInfo.isSymbolicLink())
    throw new Error("tool home must be a real directory");
  const cachePaths = [
    ".cache/hyperframes",
    ".cache/node",
    ".cache/pnpm",
    ".local/state/hypit",
    ".local/share/pnpm/store/v10/files",
    ".local/share/pnpm/store/v10/index",
    ".npm/_cacache",
  ];
  for (const cache of cachePaths) {
    const origin = join(source, cache),
      target = join(home, cache);
    try {
      await lstat(origin);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") continue;
      throw error;
    }
    let current = home;
    for (const part of cache.split("/").slice(0, -1)) {
      current = join(current, part);
      await mkdir(current, { recursive: true });
      const info = await lstat(current);
      if (!info.isDirectory() || info.isSymbolicLink())
        throw new Error("tool cache parent cannot be a link");
    }
    try {
      const existing = await lstat(target);
      if (!existing.isDirectory() || existing.isSymbolicLink())
        throw new Error("tool cache cannot be a link");
      continue;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    const originRoot = await realpath(origin);
    let count = 0,
      bytes = 0;
    const verify = async (path: string, depth = 0): Promise<void> => {
      if (depth > 64 || ++count > 100000)
        throw new Error("tool cache exceeds file quota");
      const info = await lstat(path);
      if (info.isSymbolicLink()) {
        const actual = await realpath(path);
        const rel = relative(originRoot, actual);
        if (
          (rel === ".." || rel.startsWith("../") || isAbsolute(rel)) &&
          !/^\/usr\/bin\/python3(?:\.\d+)?$/.test(actual)
        )
          throw new Error("tool cache link escapes its image cache");
        await verify(actual, depth + 1);
      } else if (info.isDirectory()) {
        for (const entry of await readdir(path))
          await verify(join(path, entry), depth + 1);
      } else if (info.isFile()) {
        bytes += info.size;
        if (bytes > 4 * 1024 ** 3)
          throw new Error("tool cache exceeds byte quota");
      } else throw new Error("tool cache special file forbidden");
    };
    await verify(origin);
    const stage = await mkdtemp(join(dirname(target), ".hypit-cache-"));
    try {
      const copy = join(stage, "cache");
      await cp(origin, copy, { recursive: true, dereference: true });
      const writable = async (path: string): Promise<void> => {
        const info = await lstat(path);
        await chmod(
          path,
          info.isDirectory() ? 0o700 : info.mode & 0o111 ? 0o700 : 0o600,
        );
        if (info.isDirectory())
          for (const name of await readdir(path))
            await writable(join(path, name));
      };
      await writable(copy);
      await rename(copy, target);
    } finally {
      await rm(stage, { recursive: true, force: true });
    }
  }
}

export async function prepareHypitWorkspace(
  workspace: string,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal,
  resume = false,
  credentialKeys?: readonly string[],
): Promise<void> {
  if (env.HOME === join(workspace, ".anban-runtime-home"))
    await prepareHypitToolHome(workspace, env);
  const input = await readJSON(join(workspace, "input.json"));
  const project = join(workspace, "project");
  const profile = await readJSON(join(workspace, "runtime-profile.json"));
  if (
    profile.format !== "hypit.runtime-local@1" ||
    profile.dataRoot !== join(project, ".hypit/execution")
  )
    throw new Error("invalid managed Hypit runtime profile");
  const managedCredentialKeys = credentialKeys ?? hypitCredentialKeys(profile);
  if (resume) {
    try {
      const existing = await lstat(join(project, "package.json"));
      if (existing.isFile() && !existing.isSymbolicLink()) {
        await validateHypitReferences(project);
        await restoreHypitDependencies(
          project,
          env,
          signal,
          managedCredentialKeys,
        );
        await officialChecks(workspace, env, signal);
        return;
      }
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code !== "ENOENT") throw e;
    }
  }
  if (input.project_archive_path) {
    if (input.project_archive_path !== ".anban-creator/project.zip")
      throw new Error("untrusted project archive path");
    // Bootstrap assets and imported project are kept separate until import passes validation.
    const imported = join(workspace, "project-import");
    await importHypitProject(
      join(workspace, input.project_archive_path),
      imported,
      input.limits,
      signal,
    );
    const backup = join(workspace, ".hypit-import-backup");
    let backedUp = false;
    try {
      await mkdir(project, { recursive: true });
      const info = await lstat(project);
      if (!info.isDirectory() || info.isSymbolicLink())
        throw new Error("invalid project root");
      const entries = await readdir(project);
      if (entries.some((e) => e !== "assets"))
        throw new Error("project import conflicts with existing workspace");
      if (entries.length) await mergeBootstrapAssets(project, imported);
      await rename(project, backup);
      backedUp = true;
      try {
        await rename(imported, project);
        await restoreHypitDependencies(
          project,
          env,
          signal,
          managedCredentialKeys,
        );
        await officialChecks(workspace, env, signal);
      } catch (e) {
        await rm(project, { recursive: true, force: true });
        await rename(backup, project);
        backedUp = false;
        throw e;
      }
    } finally {
      await rm(imported, { recursive: true, force: true });
      if (backedUp) await rm(backup, { recursive: true, force: true });
    }
  } else {
    await mkdir(project, { recursive: true });
    try {
      await writeFile(
        join(project, "package.json"),
        JSON.stringify({
          name: "video-project",
          private: true,
          type: "module",
          dependencies: { "@hypit/hypit": "file:/opt/hypit" },
        }),
        { flag: "wx", mode: 0o644 },
      );
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code !== "EEXIST") throw e;
    }
  }
}
async function restoreHypitDependencies(
  project: string,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal,
  credentialKeys: readonly string[] = [],
): Promise<void> {
  const names = await readdir(project);
  const safeEnv = { ...env };
  for (const key of Object.keys(safeEnv))
    if (
      credentialKeys.includes(key) ||
      /TOKEN|KEY|SECRET|PASSWORD|CREDENTIAL/i.test(key)
    )
      delete safeEnv[key];
  try {
    if (names.includes("pnpm-lock.yaml")) {
      const manifest = await readJSON(join(project, "package.json"));
      const specs = Object.values({
        ...manifest.dependencies,
        ...manifest.devDependencies,
        ...manifest.optionalDependencies,
      });
      if (
        specs.some(
          (spec) =>
            typeof spec === "string" && /^(?:file:|link:)\/opt\//.test(spec),
        )
      )
        throw new Error(
          "pnpm cannot restore immutable image links; use the npm project lockfile generated by the matching runtime",
        );
      await runHypitCommand(
        "pnpm",
        [
          "install",
          "--frozen-lockfile",
          "--offline",
          "--ignore-scripts",
          "--config.bin-links=false",
        ],
        project,
        safeEnv,
        signal,
      );
    } else
      await runHypitCommand(
        "npm",
        [
          names.includes("package-lock.json") ? "ci" : "install",
          "--ignore-scripts",
          "--bin-links=false",
          "--offline",
          "--no-audit",
          "--no-fund",
        ],
        project,
        safeEnv,
        signal,
      );
  } catch (error) {
    if (
      error instanceof Error &&
      error.message.startsWith("pnpm cannot restore")
    )
      throw error;
    throw new Error(
      "Hypit project dependency restore failed: use the locked dependencies and compiled components included in the runtime image/offline cache; rebuild the image to add missing packages",
      { cause: error },
    );
  }
}
async function officialChecks(
  workspace: string,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal,
  command = runHypitCommand,
): Promise<void> {
  const cwd = join(workspace, "project"),
    run = "productions/main/runs/main.svrun";
  const check = JSON.parse(
    await command("hypit", ["check", run, "--json"], cwd, env, signal),
  );
  if (check.ok !== true) throw new Error("official check did not pass");
  const plan = JSON.parse(
    await command(
      "hypit",
      [
        "plan",
        run,
        "--runtime",
        join(workspace, "runtime-profile.json"),
        "--json",
      ],
      cwd,
      env,
      signal,
    ),
  );
  if (plan.ok !== true) throw new Error("official plan did not pass");
}
export function hypitExecutionBudget(
  input: Record<string, any>,
  now = Date.now(),
): number {
  const minutes = input.limits?.timeout_minutes ?? 90;
  if (!Number.isInteger(minutes) || minutes < 1 || minutes > 90)
    throw new Error("invalid frozen Hypit timeout");
  let budget = minutes * 60000;
  if (input.execution_deadline !== undefined) {
    const deadline = Date.parse(input.execution_deadline);
    if (!Number.isFinite(deadline))
      throw new Error("invalid frozen execution deadline");
    budget = Math.min(budget, deadline - now);
  }
  if (budget <= 0) throw new Error("Hypit execution deadline exceeded");
  return budget;
}
export interface HypitRestoreContext {
  profile: unknown;
  runtime?: unknown;
  input?: Record<string, any>;
}

function runtimeMetadata(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value))
    return undefined;
  const raw = value as Record<string, unknown>;
  const patterns: Record<string, RegExp> = {
    image: /^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,1023}$/,
    agent_pack_id: /^[a-z0-9]+(?:-[a-z0-9]+)*$/,
    agent_pack_version: /^\d+\.\d+\.\d+$/,
    agent_pack_digest: /^[0-9a-f]{64}$/,
  };
  const metadata: Record<string, string> = {};
  for (const [key, pattern] of Object.entries(patterns))
    if (typeof raw[key] === "string" && pattern.test(raw[key]))
      metadata[key] = raw[key];
  return Object.keys(metadata).length ? metadata : undefined;
}

async function writeRestoreInstructions(
  project: string,
  context: HypitRestoreContext,
  keys: readonly string[],
  revision: string,
): Promise<void> {
  const profile = JSON.parse(JSON.stringify(context.profile)) as Record<
    string,
    unknown
  >;
  if (!profile || profile.format !== "hypit.runtime-local@1")
    throw new Error(
      "restore template requires the trusted native runtime profile",
    );
  const validate = (value: unknown): void => {
    if (!value || typeof value !== "object") return;
    if (Array.isArray(value)) {
      value.forEach(validate);
      return;
    }
    const object = value as Record<string, unknown>;
    if ("store" in object && object.store !== "env")
      throw new Error("restore profile credentials must use env references");
    if (
      typeof object.use === "string" &&
      object.use.startsWith("@hypit/credential-store-") &&
      object.use !== "@hypit/credential-store-env"
    )
      throw new Error("restore profile cannot contain a credential file store");
    for (const [key, child] of Object.entries(object)) {
      if (
        /secret|password|token$|apikey|api_key|authorization|accesskey/i.test(
          key,
        ) &&
        (typeof child !== "object" || child === null)
      )
        throw new Error("restore profile contains a literal credential");
      validate(child);
    }
  };
  validate(profile);
  profile.dataRoot = "/workspace/project/.hypit/execution";
  const rootInfo = await lstat(project);
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink())
    throw new Error("restore project root must be a real directory");
  const directory = join(project, "restore");
  await mkdir(directory, { recursive: true });
  const info = await lstat(directory);
  if (!info.isDirectory() || info.isSymbolicLink())
    throw new Error("restore directory must be a real directory");
  await atomicJSON(join(directory, "profile.template.json"), profile);
  const runtime = runtimeMetadata(context.runtime);
  const instructions = [
    "# Restore this Hypit project",
    "",
    "Extract this archive at exactly /workspace/project inside the same immutable managed runtime image. Absolute Result references depend on this location.",
    runtime?.image
      ? "Original image: " + runtime.image
      : "Original image metadata was not supplied. Use the matching immutable image from the source execution.",
    "Official Hypit revision: " + revision,
    runtime ? "Agent Pack/runtime provenance: " + JSON.stringify(runtime) : "",
    "",
    "The profile.template.json file is the trusted native runtime configuration with environment references only. It contains no credential values. Supply the following environment keys through your secret manager; never write their values into the project or archive:",
    ...[...new Set(keys)].sort().map((key) => "- " + key),
    "",
    "Copy restore/profile.template.json to /workspace/runtime-profile.json. Its execution dataRoot is /workspace/project/.hypit/execution. Worker state is intentionally not restored.",
    "",
    "Restore dependencies only from the matching image/offline cache using the existing lockfile. For npm use: npm ci --offline --ignore-scripts --bin-links=false --no-audit --no-fund. Use npm for managed project locks. pnpm locks with immutable /opt image links are unsupported because pnpm mutates linked executable modes; rebuild the project with the matching npm lockfile. For other pnpm projects use: pnpm install --offline --frozen-lockfile --ignore-scripts. Binary links are disabled to keep the image distribution immutable; hypit and other image tools are already available on PATH. Keep credential variables out of the package-manager environment. Missing cached dependencies require an image rebuild; do not fall back to network installation. Preserve archived project component sources and compiled files.",
    "",
    "From /workspace/project, verify without generating or paying for anything:",
    "    hypit check productions/main/runs/main.svrun --json",
    "    hypit plan productions/main/runs/main.svrun --runtime /workspace/runtime-profile.json --json",
    "",
    "The archive contains complete .hypit/results and their forwarded dependencies. Reuse these official Results and existing outputs before deciding whether new work is necessary. An observation timeout does not mean a Build failed. Inspect the official Build/Result state; never blindly resubmit an ambiguous provider request. Keep build recovery in the official Hypit Agent workflow.",
    "",
  ].join("\n");
  const stage = await mkdtemp(join(directory, ".restore-doc-"));
  try {
    const file = join(stage, "README.md");
    await writeFile(file, instructions, { mode: 0o600, flag: "wx" });
    await rename(file, join(directory, "README.md"));
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}

export async function finalizeHypit(
  workspace: string,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal,
  dependencies = {
    command: runHypitCommand,
    revision: () => readFile("/opt/hypit/.anban-source-revision", "utf8"),
  },
  credentialKeys?: readonly string[],
  restoreContext?: HypitRestoreContext,
): Promise<Record<string, string>> {
  const output = join(workspace, "output");
  const outputInfo = await lstat(output);
  if (!outputInfo.isDirectory() || outputInfo.isSymbolicLink())
    throw new Error("output must be a real directory");
  const input =
    restoreContext?.input ?? (await readJSON(join(workspace, "input.json")));
  const limits = quota(input.limits ?? {});
  const mediaSnapshot = await mkdtemp(join(tmpdir(), "anban-hypit-media-"));
  const report: Record<string, unknown> = {
    schema_version: 1,
    passed: false,
    checks: {},
  };
  try {
    await officialChecks(workspace, env, signal, dependencies.command);
    const semantic = await readJSON(join(output, "semantic-report.json"));
    if (semantic.passed !== true)
      throw new Error("semantic quality report did not pass");
    const verifiedMedia: Record<string, string> = {};
    for (const name of ["final.mp4", "cover.png"])
      verifiedMedia[name] = await snapshotFile(
        output,
        name,
        join(mediaSnapshot, name),
        limits.max_video_bytes,
        [],
        signal,
      );
    for (const name of ["final.mp4", "cover.png", "delivery-manifest.json"]) {
      const info = await lstat(join(output, name));
      if (
        !info.isFile() ||
        info.isSymbolicLink() ||
        info.size === 0 ||
        info.size > limits.max_video_bytes
      )
        throw new Error("missing or invalid delivery artifact");
    }
    const video = JSON.parse(
      await runHypitCommand(
        "ffprobe",
        [
          "-v",
          "error",
          "-show_format",
          "-show_streams",
          "-of",
          "json",
          join(mediaSnapshot, "final.mp4"),
        ],
        workspace,
        env,
        signal,
      ),
    );
    const duration = Number(video.format?.duration);
    const stream = video.streams?.find(
      (s: { codec_type: string }) => s.codec_type === "video",
    );
    if (
      !Number.isFinite(duration) ||
      duration <= 0 ||
      duration > limits.max_duration_seconds ||
      !stream?.width ||
      !stream?.height
    )
      throw new Error("video exceeds limits or has no video stream");
    const preferences = input.preferences ?? {};
    let reference:
      { duration: number; width: number; height: number } | undefined;
    if (
      input.reference ||
      !preferences.duration_seconds ||
      !preferences.aspect_ratio ||
      preferences.aspect_ratio === "source"
    ) {
      let referencePath = input.reference?.url;
      if (
        typeof referencePath !== "string" ||
        !referencePath.startsWith("project/assets/")
      ) {
        const manifest = await readJSON(join(output, "reference-media.json"));
        referencePath = manifest.path;
      }
      if (
        typeof referencePath !== "string" ||
        !referencePath.startsWith("project/assets/")
      )
        throw new Error("missing local reference media");
      cleanBootstrapPath(referencePath);
      const included = await files(join(workspace, "project"), limits);
      if (!included.includes(referencePath.slice("project/".length)))
        throw new Error("reference must be an included regular asset");
      const refInfo = await lstat(join(workspace, referencePath));
      if (refInfo.size > limits.max_asset_bytes)
        throw new Error("reference exceeds media byte limit");
      const probe = JSON.parse(
        await runHypitCommand(
          "ffprobe",
          [
            "-v",
            "error",
            "-show_format",
            "-show_streams",
            "-of",
            "json",
            join(workspace, referencePath),
          ],
          workspace,
          env,
          signal,
        ),
      );
      const refStream = probe.streams?.find(
        (s: { codec_type: string }) => s.codec_type === "video",
      );
      reference = {
        duration: Number(probe.format?.duration),
        width: Number(refStream?.width),
        height: Number(refStream?.height),
      };
      if (
        !Number.isFinite(reference.duration) ||
        reference.duration <= 0 ||
        reference.duration > limits.max_duration_seconds ||
        !reference.width ||
        !reference.height
      )
        throw new Error("reference exceeds duration limit or lacks video");
    }
    const expectedDuration =
      Number(preferences.duration_seconds) || reference?.duration;
    if (
      expectedDuration !== undefined &&
      Math.abs(duration - expectedDuration) >
        Math.max(0.25, expectedDuration * 0.02)
    )
      throw new Error(
        "video duration does not match requested/reference duration",
      );
    const ratios: Record<string, number> = {
      "9:16": 9 / 16,
      "16:9": 16 / 9,
      "1:1": 1,
    };
    const expectedRatio =
      preferences.aspect_ratio && preferences.aspect_ratio !== "source"
        ? ratios[preferences.aspect_ratio]
        : reference
          ? reference.width / reference.height
          : undefined;
    if (
      expectedRatio !== undefined &&
      Math.abs(stream.width / stream.height - expectedRatio) / expectedRatio >
        0.015
    )
      throw new Error(
        "video aspect ratio does not match requested/reference ratio",
      );
    await runHypitCommand(
      "ffmpeg",
      [
        "-v",
        "error",
        "-xerror",
        "-i",
        join(mediaSnapshot, "final.mp4"),
        "-f",
        "null",
        "-",
      ],
      workspace,
      env,
      signal,
    );
    await runHypitCommand(
      "ffmpeg",
      [
        "-v",
        "error",
        "-xerror",
        "-i",
        join(mediaSnapshot, "cover.png"),
        "-f",
        "null",
        "-",
      ],
      workspace,
      env,
      signal,
    );
    const managedCredentialKeys =
      credentialKeys ??
      hypitCredentialKeys(
        restoreContext?.profile ??
          (await readJSON(join(workspace, "runtime-profile.json"))),
      );
    const secrets = Object.entries(env)
      .filter(
        ([key, value]) =>
          (managedCredentialKeys.includes(key) ||
            /TOKEN|API_KEY|SECRET|PASSWORD|CREDENTIAL/i.test(key)) &&
          typeof value === "string" &&
          value.length > 0,
      )
      .map(([, value]) => value!);
    const revision = (await dependencies.revision()).trim();
    if (!/^[0-9a-f]{40}$/.test(revision))
      throw new Error("invalid upstream revision");
    const restore = restoreContext ?? {
      profile: await readJSON(join(workspace, "runtime-profile.json")),
      runtime: input.runtime,
    };
    await writeRestoreInstructions(
      join(workspace, "project"),
      restore,
      managedCredentialKeys,
      revision,
    );
    await validateHypitReferences(join(workspace, "project"), secrets, signal);
    await atomicJSON(join(output, "project.json"), {
      schema_version: 1,
      upstream: { repository: "https://github.com/hypit-ai/hypit", revision },
      project_root: "/workspace/project",
      run_path: "productions/main/runs/main.svrun",
      ...(runtimeMetadata(restore.runtime)
        ? { runtime: runtimeMetadata(restore.runtime) }
        : {}),
    });
    const archiveDigest = await exportHypitProject(
      join(workspace, "project"),
      join(output, "project.zip"),
      limits,
      signal,
      secrets,
    );
    const archiveCheck = await mkdtemp(
      join(tmpdir(), "anban-hypit-archive-check-"),
    );
    try {
      await importHypitProject(
        join(output, "project.zip"),
        join(archiveCheck, "project"),
        limits,
        signal,
      );
    } finally {
      await rm(archiveCheck, { recursive: true, force: true });
    }
    const deliveryHashes: Record<string, string> = {};
    for (const name of [
      "final.mp4",
      "cover.png",
      "project.json",
      "project.zip",
      "delivery-manifest.json",
    ]) {
      deliveryHashes[name] = await snapshotFile(
        output,
        name,
        undefined,
        name === "project.zip"
          ? limits.max_project_bytes
          : limits.max_video_bytes,
        name === "project.zip" ? [] : secrets,
        signal,
      );
      if (name === "project.zip" && deliveryHashes[name] !== archiveDigest)
        throw new Error("verified archive changed before delivery");
      if (verifiedMedia[name] && verifiedMedia[name] !== deliveryHashes[name])
        throw new Error("verified media changed before delivery");
    }
    report.artifact_sha256 = deliveryHashes;
    report.upstream = {
      repository: "https://github.com/hypit-ai/hypit",
      revision,
    };
    report.passed = true;
    report.checks = {
      semantic: true,
      video_probe: true,
      video_full_decode: true,
      cover_decode: true,
      project_references: true,
      official_check: true,
      official_plan: true,
      project_archive: true,
    };
    report.media = {
      duration_seconds: duration,
      width: stream.width,
      height: stream.height,
    };
  } finally {
    await rm(mediaSnapshot, { recursive: true, force: true });
    await atomicJSON(join(output, "quality-report.json"), report);
  }
  const receipt = report.artifact_sha256 as Record<string, string>;
  receipt["quality-report.json"] = await snapshotFile(
    output,
    "quality-report.json",
    undefined,
    2 * 1024 * 1024,
    [],
    signal,
  );
  return receipt;
}

/** Ask the official control plane to cancel known Builds, then stop owned workers/programs. */
export async function cleanupHypit(
  workspace: string,
  env: NodeJS.ProcessEnv,
): Promise<void> {
  const cwd = join(workspace, "project");
  const runtime = ["--runtime", join(workspace, "runtime-profile.json")];
  const deadline = Date.now() + 20000;
  const attempt = async (args: string[]) => {
    try {
      if (Date.now() >= deadline) return undefined;
      return await runHypitCommand(
        "hypit",
        args,
        cwd,
        env,
        AbortSignal.timeout(Math.max(1, Math.min(5000, deadline - Date.now()))),
      );
    } catch {
      return undefined;
    }
  };
  const active = await attempt(["activity", ...runtime, "--json"]);
  if (active) {
    try {
      const data = JSON.parse(active);
      if (Array.isArray(data.builds))
        for (const build of data.builds.slice(0, 100)) {
          if (Date.now() > deadline - 10000) break;
          if (
            typeof build.id === "string" &&
            /^[a-zA-Z0-9_.:-]{1,256}$/.test(build.id)
          )
            await attempt([
              "cancel",
              build.id,
              ...runtime,
              "--reason",
              "managed execution stopped",
            ]);
        }
    } catch {
      /* Runtime down still runs if activity is unavailable. */
    }
  }
  await attempt(["runtime", "down", ...runtime]);
  await attempt(["programs", "down", ...runtime]);
}

export function hypitFailureDetails(
  reason: unknown,
  stage: unknown = "execution",
): { error_code: string; stage: string; resume_from: string; message: string } {
  const text = reason instanceof Error ? reason.message : String(reason ?? "");
  if (/credential|secret/i.test(text))
    return {
      error_code: "hypit_credential_rejected",
      stage: "quality_validation",
      resume_from: "repair_project",
      message:
        "A managed credential was found in delivery content. Remove it from project artifacts before retrying.",
    };
  if (/deadline|timed? ?out/i.test(text))
    return {
      error_code: "hypit_execution_timeout",
      stage: "execution",
      resume_from: "inspect_results",
      message:
        "The execution deadline was reached. Inspect preserved official Results before retrying; do not blindly resubmit provider requests.",
    };
  if (
    /worker|executor.*(?:lost|exit)|volume|ENOENT|missing.*(?:project|reference)/i.test(
      text,
    )
  )
    return {
      error_code: "hypit_runtime_unavailable",
      stage: "execution",
      resume_from: "review_configuration",
      message:
        "A required worker, project file, or volume is unavailable. Restore the matching runtime and project before inspecting Results.",
    };
  if (stage === "quality_validation")
    return {
      error_code: "hypit_delivery_rejected",
      stage: "quality_validation",
      resume_from: "repair_project",
      message:
        "Media or project delivery validation failed. Repair the project and inspect existing Results before retrying.",
    };
  if (stage === "artifact_upload")
    return {
      error_code: "hypit_delivery_transfer_failed",
      stage: "artifact_upload",
      resume_from: "retry_transfer",
      message:
        "Validated delivery transfer failed. Retry artifact transfer without regenerating media.",
    };
  return {
    error_code: "hypit_execution_failed",
    stage: "execution",
    resume_from: "inspect_results",
    message:
      "Video execution did not complete. Inspect preserved official Results before retrying.",
  };
}

/** Upload only a private, byte-checked snapshot. Failed jobs expose no authored outputs. */
export async function prepareHypitUpload(
  workspace: string,
  secrets: readonly string[],
  receipt?: Record<string, string>,
  signal?: AbortSignal,
  failure = hypitFailureDetails(undefined),
): Promise<{ workspace: string; dispose: () => Promise<void> }> {
  const stage = await mkdtemp(join(tmpdir(), "anban-hypit-delivery-"));
  await mkdir(join(stage, "output"));
  try {
    if (receipt) {
      const required = [
        "final.mp4",
        "cover.png",
        "project.json",
        "project.zip",
        "delivery-manifest.json",
        "quality-report.json",
      ];
      for (const name of required) {
        if (!/^[0-9a-f]{64}$/.test(receipt[name] ?? ""))
          throw new Error("missing verified artifact receipt");
        const digest = await snapshotFile(
          join(workspace, "output"),
          name,
          join(stage, "output", name),
          name === "project.zip"
            ? HYPIT_LIMITS.max_project_bytes
            : HYPIT_LIMITS.max_video_bytes,
          name === "project.zip" ? [] : secrets,
          signal,
        );
        if (digest !== receipt[name])
          throw new Error("verified artifact changed before upload");
      }
    } else {
      // Reconstruct every field from the allowlisted code; never copy caller text.
      const safe =
        failure.error_code === "hypit_credential_rejected"
          ? hypitFailureDetails("credential")
          : failure.error_code === "hypit_execution_timeout"
            ? hypitFailureDetails("deadline")
            : failure.error_code === "hypit_runtime_unavailable"
              ? hypitFailureDetails("worker")
              : failure.error_code === "hypit_delivery_rejected"
                ? hypitFailureDetails(undefined, "quality_validation")
                : failure.error_code === "hypit_delivery_transfer_failed"
                  ? hypitFailureDetails(undefined, "artifact_upload")
                  : hypitFailureDetails(undefined);
      await atomicJSON(join(stage, "output/failure-state.json"), {
        version: "1.0",
        status: "recoverable_failure",
        ...safe,
      });
      await writeFile(
        join(stage, "output/failure-diagnosis.md"),
        [
          "# Video execution recovery",
          "",
          safe.message,
          "",
          "Stage: " + safe.stage,
          "Error code: " + safe.error_code,
          "Resume from: " + safe.resume_from,
          "",
        ].join("\n"),
        { flag: "wx", mode: 0o600 },
      );
    }
    return {
      workspace: stage,
      dispose: () => rm(stage, { recursive: true, force: true }),
    };
  } catch (error) {
    await rm(stage, { recursive: true, force: true });
    throw error;
  }
}
