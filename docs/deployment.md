# Studio 更新发布

## Claude 与 DSH 独立模型配置

Claude 托管 Agent 和 DSH 交互会话使用两套独立的模型路由。Claude 只读取
`claude.profiles`；DSH 只读取 `dsh.routes` 和当前的 `dsh.route`。修改一方
不会隐式修改另一方。两套配置可以引用同一个 Kubernetes Secret，但字段和
校验保持独立。

```yaml
claude:
  profiles:
    effective:
      provider: deepseek
      envs:
        ANTHROPIC_BASE_URL: https://api.deepseek.com/anthropic
        ANTHROPIC_AUTH_TOKEN: ${ANBAN_DEEPSEEK_API_KEY}
        ANTHROPIC_MODEL: deepseek-flash

dsh:
  enabled: true
  # The DSH process and its resumable Studio session share this lease. Keep it
  # long enough for users to return after an extended break; tune lower only
  # when the runtime resource budget requires it.
  timeout_sec: 86400
  route: effective
  routes:
    effective:
      provider: anthropic
      api: anthropic-messages
      base_url: https://api.deepseek.com/anthropic
      api_key: "${ANBAN_DEEPSEEK_API_KEY}"
      model: deepseek-flash
```

`api_key` uses the same `${...}` configuration expansion as Claude's `envs`.
The Server resolves it once when loading the configuration, then injects it into
the isolated DSH child through an internal per-session variable. The Bridge maps
that internal variable to DSH's official `apiKeyEnv` field, writes `api`,
`baseURL`, and `models` into temporary per-session DSH settings, and starts the
fixed official runtime profile: `dsh --profile sdk`. `provider` and `model` are
also sent through the official SDK `initialize({ provider, model })` handshake.

The DSH child receives only `DSH_HOME`, the internal per-session
`ANBAN_DSH_BRIDGE_PROVIDER_API_KEY` provider-key variable, and the internal
`ANBAN_DSH_BRIDGE_*` Bridge protocol variables for the session, MCP URL, runtime
token, patch, initial context, and temporary home.
Do not configure the removed `interactive:` block, `provider_api_key_env`,
`api_key_env`, or `ANBAN_DSH_API_KEY`, `ANBAN_DSH_BASE_URL`,
`ANBAN_DSH_MODEL`, `ANBAN_DSH_PROVIDER`, or `ANBAN_DSH_PROFILE` names. The
Server rejects startup when any of these legacy `ANBAN_DSH_*` variables is
present, so remove stale deployment entries before switching to `dsh.routes`.

For Kubernetes or Compose, keep using the existing `ANBAN_*` environment
references already used by Claude. No additional DSH credential variable or
Secret mapping is required.

On ACK, the DSH runtime is a separate image from the Server image. The Yunxiao
release must build and push `deploy/docker/Dockerfile.runtime-dsh` from the same
commit as the Server, then update the mounted `dsh.image` value to that image
(prefer an immutable `@sha256` digest) before rolling out the Server. A Server
rebuild alone does not update existing DSH runtime Pods; an old Bridge can
close during `session/start` and the API will then report `41000`.

当前先恢复为一个固定的 Studio Deployment（例如 `creator-studio-web-base`），沿用原有 ACS 更新流程。`version_switch` 固定为 `base`；它用于资源名、Pod 标签和日志，不影响 Nginx。Deployment selector 只匹配稳定的 `app` 标签，不能加 `version`，否则会尝试修改已有 Deployment 的不可变 selector。

## 当前更新流程

构建新镜像时继续传入当前线上镜像，让新镜像保留旧标签页可能请求的 hash JS/CSS：

```bash
docker build -f deploy/docker/Dockerfile.studio \
  --build-arg STUDIO_PREVIOUS_IMAGE="$CURRENT_STUDIO_IMAGE" \
  -t "$NEW_STUDIO_IMAGE" .
docker push "$NEW_STUDIO_IMAGE"
```

然后保持 `version_switch=base`，渲染并 apply 模板。Deployment 和 Service 的名字保持不变：

```bash
export version_switch=base
envsubst < studio/Deployment.yaml > /tmp/studio-rendered.yaml
kubectl apply -f /tmp/studio-rendered.yaml
kubectl -n "$namespace" rollout status deployment/creator-studio-web-base --timeout=10m
```

## 已知边界

- `/index.html` 不缓存；带 hash 的 `/assets/` 资源缓存一年。新镜像继承旧 assets 可支持升级后仍打开的旧页面。
- 滚动更新期间旧、新 Pod 会同时接收请求。旧 Pod 尚无新版本 JS 时，个别新页面请求可能短暂得到 404；前端的加载恢复逻辑可尝试恢复，但这套临时方案不能保证切换期间绝无失败。
- 先不使用 `studio/deploy.py` 的逐版本发布流程；它要求每次用新的 `version_switch` 和 Deployment 名称，与当前固定 `base` 更新方式不同。

## 后续改进

要让新旧 Pod 始终提供同一份 hash 资源，可把 `/assets/` 发布到共享 OSS/CDN：先上传新资源，再发布引用它们的 HTML，并按保留期清理旧资源。接入前需要确定 Studio 专用的 OSS 桶或 CDN 域名及流水线发布权限。

## Agent Pack Catalog 与云效构建

Agent runtime image references may use normal registry tags or immutable
digests. Digest pinning is recommended for production rollback safety, but it
is not required by the Server configuration or ACK renderer.

云效构建 Agent 或 Server 镜像时不需要配置
`ANBAN_AGENT_PACK_CATALOG_DIGEST` Build Arg，也不需要写入 Catalog OCI
label。构建前在同一提交上运行：

```bash
make agent-pack-generate
make agent-pack-check
docker build -f deploy/docker/Dockerfile.agent-seednote -t "$IMAGE" .
```

`make agent-pack-check` 校验 Server 与 Harness 的生成 Catalog 一致。发布
manifest 仍记录 Catalog digest 作为审计信息；Runtime 启动时会计算镜像内
Catalog digest，并与 Server bootstrap 返回的 digest 比较，发现漂移则拒绝
执行。Server Deployment 模板不依赖 Catalog digest 变量。ACK 发布必须用
下面的渲染脚本生成完整清单；脚本默认把 TokenReview ClusterRole 和
ClusterRoleBinding 放在 Server 命名空间资源之前，避免只发布 Server 清单
导致所有 Agent 在 bootstrap 阶段失败：

```bash
scripts/render-ack-server-deployment.sh \
  --manifest release/agent-runtime-manifest.json \
  --output /tmp/server-release.yaml
kubectl apply -f /tmp/server-release.yaml
```

发布机运行这些脚本时需要安装 `go`、`jq` 和 `envsubst`。DSH 镜像引用由
`go run ./cmd/validate-image-reference` 使用与 Server 相同的 Docker
reference parser 校验，因此裸名称、registry tag、digest 和 `tag@digest`
的接受规则保持一致；校验不会强制要求 digest。

The release manifest must include the separate DSH runtime image under
`images.dsh` and the video editor under `images["video-edit"]`. Set
`DSH_RUNTIME_IMAGE=...` and `VIDEO_EDIT_AGENT_IMAGE=...` when running
`make agent-runtime-release-manifest`; Make passes the editor image as
`AGENT_IMAGE_VIDEO_EDIT`. The ACK renderer maps the manifest entry to
`video_edit_agent_image_repo`, which becomes `ANBAN_AGENT_IMAGE_VIDEO_EDIT` in
the Server Deployment. This is the runtime image setting consumed by
`claude.runtime_images.video-edit`.

Local/Compose deployments provide `ANBAN_AGENT_IMAGE_VIDEO_EDIT` and
`VIDEO_EDIT_ELEVENLABS_API_KEY` through `.env`. Kubernetes reads the Scribe key
from the optional `video-edit-elevenlabs` Secret's `api-key` entry; create that
Secret in the Server namespace before dispatching video-edit tasks. The Server
requires the credential even when a task uses the default subtitle mode.
`server/config.example.yaml` maps the key to
`video_edit.elevenlabs_api_key`, and the Server keeps the credential scoped to
video-edit executions.

只有在发布流水线已经单独 apply `deploy/k8s/ack-agent-runtime.yaml` 时，
才使用 `--server-only`。Server 与 Agent 镜像仍必须从同一提交构建并更新
到清单中；Catalog 不一致会被 Runtime 拒绝执行。
