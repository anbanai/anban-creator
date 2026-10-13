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
执行。Server Deployment 模板不依赖 Catalog digest 变量，因此云效可以直
接渲染 `server/Deployment.yaml`，运行时一致性校验仍然保留。
