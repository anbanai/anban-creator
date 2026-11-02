# 微信公众号扫码绑定配置

项目设置中的“扫码绑定公众号”由服务端微信开放平台配置控制。线上 ACK 部署按以下步骤开启；这组密钥只配置在 Server，不要放到 Studio、浏览器变量或镜像构建参数中。

## 准备开放平台信息

先在微信开放平台创建并完成认证的第三方平台，准备：

- 第三方平台 AppID 和 AppSecret。
- 消息校验 Token。
- 消息加解密 EncodingAESKey。
- 一个独立的 Token 加密密钥，可用 `openssl rand -base64 32` 生成。

服务端 `server.public_base_url` 必须是微信可访问的 HTTPS 根地址，例如 `https://creator.anbanai.com`，不能带 `/api/...` 路径。将以下地址配置到开放平台对应的事件回调设置：

- 授权事件接收 URL：`<server.public_base_url>/api/v1/wechat/open-platform/events`
- 消息与事件接收 URL：`<server.public_base_url>/api/v1/wechat/open-platform/events`

授权后浏览器回跳地址由 Server 自动拼接为 `<server.public_base_url>/api/v1/wechat/open-platform/authorization/callback`，不需要手工配置成事件接收地址。

## 在 ACK 配置

1. 将 `deploy/wechat-open-platform-secret.example.yaml` 复制到安全目录，例如 `/secure/path/wechat-open-platform-secret.yaml`，填入开放平台的四项凭证和生成的加密密钥。模板默认命名空间为 `anbanai-prod`；如果 Server 在其他命名空间，修改 Secret 的 namespace，使其与 Server Deployment 一致。

2. 将 Secret 应用到集群：

   ```bash
   kubectl apply -f /secure/path/wechat-open-platform-secret.yaml
   ```

3. 编辑生产 ConfigMap：

   ```bash
   kubectl -n anbanai-prod edit configmap creator-prod
   ```

   在 `data.creator-api.yaml` 对应的 Server 配置内容中合入 `server/config.example.yaml` 里的完整配置段，保留现有配置：

   ```yaml
   wechat_open_platform:
     component_app_id: "${ANBAN_WECHAT_COMPONENT_APP_ID}"
     component_app_secret: "${ANBAN_WECHAT_COMPONENT_APP_SECRET}"
     message_token: "${ANBAN_WECHAT_COMPONENT_TOKEN}"
     encoding_aes_key: "${ANBAN_WECHAT_COMPONENT_ENCODING_AES_KEY}"
     token_encryption_key: "${ANBAN_WECHAT_TOKEN_ENCRYPTION_KEY}"
   ```

   `server/Deployment.yaml` 已通过可选 `envFrom.secretRef` 注入 `anban-wechat-open-platform`。配置段和 Secret 必须同时存在；只建 Secret 不会启用功能。

4. ConfigMap 以 `subPath` 挂载，改完后需要创建新 Server Pod。当前生产 Deployment 名称是 `creator-api-prod`：

   ```bash
   kubectl -n anbanai-prod rollout restart deployment/creator-api-prod
   kubectl -n anbanai-prod rollout status deployment/creator-api-prod --timeout=10m
   ```

   如果线上 Deployment 使用了不同名称，用 `kubectl -n anbanai-prod get deployments` 查出 Server Deployment 并替换命令中的名称。启动失败时先检查 Server 日志和 Pod 状态；五项配置缺一、AESKey 格式错误或加密密钥不是 32 字节都会阻止服务启动。

## 验证

1. 登录 Studio，在公众号项目卡片上点齿轮“平台设置”。接口 `GET /api/v1/projects/:id/platform-configs/wechat` 应返回 `authorization_enabled: true`；此时设置窗口会显示“扫码绑定公众号”。
2. 点击“扫码绑定公众号”并完成授权，检查项目设置显示账号已连接；再开启“自动存入公众号草稿箱”。自动保存到草稿箱需要授予草稿权限；正式发布还需要对应发布权限，并且发布仍需在任务详情确认。
3. 公众号账号统计需授权用户管理/用户统计权限（scope 2），然后在“内容分析”页手动点击账号统计同步。当前同步不是后台定时自动拉取；缺少该权限时，同步按钮不会启用。

本功能的代码入口、回调路由和部署 Secret 分别由 Server 与 `server/Deployment.yaml` 管理。未配置开放平台时，旧的 AppID/AppSecret 方式仍可用，但扫码区会隐藏。
