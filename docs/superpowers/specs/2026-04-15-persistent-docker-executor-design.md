# Persistent Docker Executor Design

## Context

DockerExecutor currently creates a new container per task and destroys it immediately after completion. This makes debugging impossible — the container is gone before you can inspect its state. The immediate problem is that image generation fails during task execution but the root cause is invisible because container logs are lost.

## Goal

Allow a long-lived Docker container to be reused across tasks via `docker exec`. The container is managed externally (started manually or via docker-compose). The server connects to it by name via config. Multiple tasks can execute concurrently inside the same container, each as an independent `claude` process with its own workspace subdirectory.

## Design

### Configuration

Add `ContainerName` to `DockerConfig` in `server/config/config.go`:

```yaml
claude:
  docker:
    container_name: "anban-creator-persistent"  # optional; empty = old create+destroy mode
```

Env override: `ANBAN_CLAUDE_DOCKER_CONTAINER_NAME`.

When `ContainerName` is set, the executor uses persistent mode. When empty, it uses the existing create+destroy behavior unchanged.

### Container Requirements

The persistent container must be started externally with:

```bash
docker run -d --name anban-creator-persistent \
  -v /tmp/anban-creator:/workspace \
  --add-host=host.docker.internal:host-gateway \
  creator-agent:latest sleep infinity
```

Requirements:
- Bind-mount the server's temp dir (`/tmp/anban-creator`) to `/workspace` inside the container
- `host.docker.internal` mapping so the container can reach the server's MCP endpoint
- Main process `sleep infinity` to keep the container alive

### Executor Changes (`server/agent/docker_executor.go`)

**NewDockerExecutor:**
- If `ContainerName` is set, verify the container exists and is running. Log a warning if not (will fall back to create+destroy on each task).

**Execute (persistent mode):**
1. Create host workspace dir `/tmp/anban-creator/{taskID}` (same as before)
2. Write config files into workspace (`.anban-creator/settings.json`, `.claude/.mcp.json`)
3. Build `claude` command with `--cwd /workspace/{taskID}` (subdirectory instead of root)
4. Use Docker API `ContainerExecCreate` + `ContainerExecAttach` to run the command
5. Capture stdout/stderr from exec response (multiplexed stream, same 8-byte header format)
6. Forward to `OnProgress` and server debug log
7. Wait for exec completion via `ContainerExecInspect`
8. No container cleanup (container stays alive)

**Execute (legacy mode):** Completely unchanged. The existing create+start+wait+remove path runs when `ContainerName` is empty.

### Workspace Path Change

Current: Each task's workspace is bind-mounted as `/workspace`.
Persistent: Each task uses a subdirectory `/workspace/{taskID}` inside the container.

The `--cwd` flag in the claude command changes from `/workspace` to `/workspace/{taskID}`. All config file writing on the host side continues to work because the bind-mount covers the parent directory.

### Concurrency

Multiple `docker exec` calls can run simultaneously against the same container. Each creates an independent process with its own PID, working directory, and environment. Claude sessions are distinguished by session ID. No coordination needed between tasks.

### Error Handling

- Container not found or not running: log warning, skip to legacy mode for that task
- Exec failure: same error handling as current (retry with backoff)
- Timeout: `ContainerExecInspect` polling with context deadline

### Files to Modify

1. `server/config/config.go` — add `ContainerName` to `DockerConfig`
2. `server/agent/docker_executor.go` — add persistent container execution path

### Verification

1. Start persistent container manually
2. Configure `container_name` in config.yaml
3. Trigger a task from Studio
4. Verify: `docker exec -it anban-creator-persistent bash` shows workspace directories
5. Verify: server debug logs show full Claude conversation
6. Verify: multiple tasks can run concurrently
7. Remove `container_name` from config → verify fallback to create+destroy mode
