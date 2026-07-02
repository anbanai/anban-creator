# Anban Creator Desktop (Tauri v2)

A high-performance desktop shell around the existing **Studio** web frontend.
Same data, same features — but **task execution moves local**: the desktop
registers as a local executor, claims tasks, and runs Claude Code on the user's
machine (full filesystem + shell: ffmpeg, command execution, local files).

## Architecture (Hybrid: cloud data + local execution)

```
Studio SPA (webview) ──(JWT + /api + SSE)──►  Cloud server (anbanai)
      │
      ▼  (IPC: window.__TAURI__)
Rust local executor ──(user API key)──► POST /api/v1/agent/claim ──► Cloud
      │  (claims the oldest pending local-target task)
      ▼  spawn sidecar
anban + Node + claude-code + claudecode plugin + ffmpeg
      │  (real local workspace + shell)
      └──(/agent/progress + /agent/upload)──► Cloud ──(SSE)──► Studio UI
```

- **Cloud server** stays the source of truth for data + scheduling. New: a local
  claim protocol (`POST /api/v1/agent/claim`, atomic CAS) + a 30s fallback that
  re-routes unclaimed local tasks to cloud Asynq (so nothing is stuck when the
  desktop is offline).
- **Desktop** = Tauri shell + the same `studio/dist` build + a Rust local
  executor + bundled runtime deps. Studio needs **zero new npm deps** — all
  native ops go through custom IPC commands reached via the always-on
  `window.__TAURI__` global (`app.withGlobalTauri = true`).

## Layout

```
desktop/
├── package.json              # @tauri-apps/cli + plugin JS deps
├── populate-resources.sh     # fills src-tauri/resources/ with bundled deps
├── src-tauri/
│   ├── tauri.conf.json       # frontendDist=../studio/dist; withGlobalTauri; CSP
│   ├── Cargo.toml            # tauri2 + dialog/fs/opener + tokio/reqwest
│   ├── capabilities/default.json
│   └── src/
│       ├── main.rs / lib.rs  # plugin + window + command registration
│       ├── config.rs         # AppConfig (JSON in platform config dir)
│       ├── paths.rs          # resolve bundled resource paths
│       ├── provision.rs      # dependency-readiness status
│       ├── commands.rs       # IPC commands (get_api_base, save_blob, …)
│       ├── executor.rs       # tokio claim loop (poll → spawn → emit events)
│       └── sidecar.rs        # spawn anban, mirror DockerExecutor argv
└── README.md
```

## Build & run

### Prerequisites

- Rust toolchain (`rustup`) with the `aarch64-apple-darwin` /
  `x86_64-apple-darwin` target.
- Bun + Node (for the Studio build).
- Go (for the agent sidecar build).
- Tauri system dependencies on macOS: Xcode command-line tools.

### 1. Populate bundled resources (once, and after dep changes)

```bash
bash desktop/populate-resources.sh
```

Fills `src-tauri/resources/` with: `anban` (native), `node`,
`@anthropic-ai/claude-code`, the `claudecode` plugin, and `ffmpeg`.

### 2. Install + run

```bash
cd desktop
bun install
bun tauri dev      # development (loads Studio from the vite dev server)
# or
bun tauri build    # production .app / .dmg (builds Studio first)
```

## First-run (user)

1. **Log in** to Studio (JWT, same as web) — all cloud features work unchanged.
2. Open **本地工作台 / 设置** (the desktop settings surface):
   - **Anban Creator API Key** — create one in Studio → API 密钥, paste it here
     (the local executor authenticates `/agent/claim` with it).
   - **Claude 鉴权** — paste an `ANTHROPIC_API_KEY`, or run the bundled
     `claude` OAuth login once.
   - **本地工作区根目录** — pick where per-task workspaces live.
3. **Enable local execution** → the claim loop starts. New tasks created in
   Studio (with "在本机运行" on) are claimed and run on this machine.

## How local execution flows

- Studio creates a task with `execution_target: "local"` (when the desktop
  reports a ready executor). The server sets a 30s claim deadline and does
  **not** enqueue it to cloud Asynq.
- The Rust executor polls `POST {api_base}/agent/claim` every ~2s; on a 200 it
  gets the full task config and spawns `anban run` with the argv from
  `server/agent/docker_executor.go::buildAgentCommand` (same flags the cloud
  Docker executor uses), supplying the local workspace + bundled plugin dir.
- The agent runs Claude Code locally (via `claude-agent-sdk-go`) and reports
  progress/results back to the cloud itself (`/agent/progress`, `/agent/upload`)
  — which flow through Redis pub/sub → SSE → Studio UI exactly like cloud runs.
- If no desktop claims within 30s, the server's fallback worker flips the task
  to cloud execution (no double-run: atomic CAS claim + skip-enqueue guarantee).

## Notes / known limits

- **macOS-first**. Windows is Phase 2 (multi-arch binaries + signing).
- **Signing**: set `bundle.macOS.signingIdentity` in `tauri.conf.json` and run
  notarization (`xcrun notarytool`) for distribution. Dev builds run unsigned.
- **The Rust here is scaffolded, not build-verified in CI** — the Tauri build
  is run locally (`bun tauri build`). Expect to iterate on exact
  `tauri-plugin-*` v2 method names during the first compile.
- **localStorage seeding**: `lib.rs` injects `anban_creator_api_base` into the
  webview via an initialization script so axios resolves the cloud base before
  the first request. The Studio `http-client.ts` reads it synchronously.
- See the root project's `CLAUDE.md` for the server-side claim protocol
  (`server/service/local_executor_claim.go`) and the studio bridge
  (`studio/src/lib/tauri.ts`).
