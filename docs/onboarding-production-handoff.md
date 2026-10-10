# 访谈式画像与语音：正式产品接入交接

分支：`feature/project-creator-onboarding`。本次仅提交代码，不合并主分支、不构建推送生产镜像、不部署。

## 改动范围

| 层 | 本次改动 |
| --- | --- |
| Studio | 项目页新增「聊一聊，建立画像」；正式登录路由 `/projects/new/interview`；六维画像进度、够用即确认；录音→文字核对→发送；确认后保存项目、进入第一篇创作和作品页 |
| Server | 登录保护的 `/api/v1/onboarding/capabilities`、`POST /chat`、`POST /transcribe`；供应商请求、候选结构/原文引用校验、超时、跨实例限额与并发锁 |
| 原有业务接口 | 继续使用项目创建、画像确认与版本冲突保护、任务计费和生成接口。候选模型无权直接写项目、扣积分或发布 |
| 数据库 / Harness | 不新增表，不修改数据库结构，不修改 Skill、Agent、Agent Pack 或运行时镜像 |

已有任务详情「作品优先、中间过程折叠」也在此分支，由 Studio 镜像一起带入。

正式入口不再依赖 `onboarding-online.html`、Vite 插件、Windows Python/Whisper 或本机 `.secrets`。开发预览只保留为同一套组件的另一种传输适配器。

## 需要更新的镜像

**两张：Server + Studio。** 使用同一提交构建并使用不可变 tag/digest：

- Server：`deploy/docker/Dockerfile.server`，仓库根目录为 build context。
- Studio：`deploy/docker/Dockerfile.studio`，仓库根目录为 build context。构建时 `STUDIO_PREVIOUS_IMAGE` 必须传当前线上 Studio 镜像的 digest，保留旧标签页的历史 assets；`VITE_API_BASE_URL=/api/v1`。
- 无须更新现有内容 Agent 镜像、MCP/小红书 sidecar、数据库和 Redis 镜像，也无须单独运行语音容器。

## Server 配置

将 `server/config.example.yaml` 中完整 `onboarding:` 段合入实际挂载的 `/app/conf/config.yaml`。**只加环境变量而不加 YAML 引用不会生效。** 新段默认关闭，旧配置无需更改即可启动。

| 环境变量 | 用途 |
| --- | --- |
| `ANBAN_ONBOARDING_ENABLED` | 验收时设 `true`；停用设 `false` 并滚动重启 Server |
| `ANBAN_ONBOARDING_CHAT_BASE_URL` | OpenAI-compatible Chat Completions 根地址。例如 DeepSeek 官方 `https://api.deepseek.com`，不填 Anthropic 接口地址 |
| `ANBAN_ONBOARDING_CHAT_API_KEY` | 服务端专用模型密钥，通过 Secret 注入，不进入前端、Git、镜像 build args |
| `ANBAN_ONBOARDING_CHAT_MODEL` | 账号实际可调用的模型 ID；没有前端写死的模型或自动降级 |
| `ANBAN_ONBOARDING_SPEECH_BASE_URL` | 支持 multipart `/audio/transcriptions` 的服务根地址，通常含 `/v1`；不填完整 endpoint |
| `ANBAN_ONBOARDING_SPEECH_API_KEY` | 语音供应商密钥，单独 Secret |
| `ANBAN_ONBOARDING_SPEECH_MODEL` | 供应商实际支持的转写模型 ID |
| `ANBAN_ONBOARDING_DAILY_USER_LIMIT` | 单账号每日请求上限，默认 60 |
| `ANBAN_ONBOARDING_DAILY_GLOBAL_LIMIT` | 全平台每日请求上限，默认 1000 |

语音三项可以一起留空：文字访谈可独立启用，语音退回浏览器自身识别（浏览器不支持时显示替代方法），不会静默上传到另一个供应商。要获得正式的服务器转写能力，技术合伙人需选定并验证供应商后填写这三项。

接口契约：语音供应商须支持 `file`、`model`、`language=zh`、`response_format=json`，返回 `{"text":"..."}`；需接受浏览器产出的 WebM/Opus、Ogg/Opus 或 MP4。不兼容该协议的厂商需另写适配器，不能仅替换地址。参考 [Audio transcription API](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create)。DeepSeek 用于文字理解，不用于音频转写。

Server 要能出站访问两个配置的 HTTPS 服务，并使用现有 Redis。Redis 未配置或出错时拒绝发起模型/语音调用，避免多实例失控。无须开放本机预览端口。正式网页必须 HTTPS；Ingress 保持请求体至少 8 MB、处理超时至少 120 秒，避免 60 秒网关超时。仓库现有 Studio nginx 为 25 MB / 1 小时，不需额外改动。

## 成本、隐私与恢复边界

- 访谈和转写目前由平台承担供应商费用，不新增用户积分 SKU；后续作品生成仍按原任务流程计费。每日配额合并计算聊天和语音尝试，失败请求也计入；按 UTC 日期重置。生产开放前运营应确认配额。
- 每账号最多一个在途调用，Redis 锁跨副本共享；聊天最多 5,000 输出 token，供应商调用不自动重试。原文会随每轮发送给模型，录音只在用户点击录音后上传给配置的语音服务。
- 每条输入最多 6,000 字符、总对话最多 40,000 字符；请求预留下一条回复的空间（最多 59 条消息 / 38,200 字符）。接近上限可先保存画像再从项目继续创作。语音客户端单段最多 55 秒，服务端硬限制 8 MB、输出 6,000 字符、请求 100 秒。服务端不把 55 秒当已校验的音频时长，不保留录音文件、不记录对话正文/密钥。
- 未确认对话仅保存在当前浏览器标签页的按账号隔离临时草稿，刷新可以恢复，但关闭标签页/换设备不保证恢复；恢复后必须再次确认。正式画像通过既有 Server 版本接口保存。
- 创建/生成响应不明时保留编号和恢复收据，禁止自动重复提交；去项目/任务列表核对。重新访谈不会删除已保存项目。
- 公众号生成沿用现有渠道配置要求；没有绑定公众号时先引导配置，不偷偷提交付费任务。本次不改公众号发布策略。

## 验收、发布和回滚

1. 先在测试/预发布部署同提交 Server、Studio，配置文本服务、Redis、语音服务 Secret。
2. 使用现有账号，从项目页进入访谈；验证一次回答更新多个维度、撤回信息降低进度、身份/读者/平台明确后即可结束。
3. 录音停止后先出现可编辑文字，不应自动发送；拒绝麦克风权限、服务不可用时仍可打字。
4. 确认画像后手动保存；刷新恢复、退出登录/换账号不得串画像；创建响应不明不得重复建项目。
5. 用户确认费用后生成一篇测试作品，在结果页直接查看最终正文；检查现有画像版本和任务计费记录。此步骤会产生真实供应商费用，需用测试账号执行。
6. 发布前运行 CI 的 Go `check` 和 Studio 测试/构建作业。现有 `.github/workflows/ci.yml` 仅在 main push 或面向 main 的 PR 触发；推本分支本身不等于 CI 已跑。`release.yml` 是 DSH 软件包流程，不是这次 Server/Studio 部署。
7. 生产部署流水线由技术合伙人使用现有编排执行；本分支不自动合并或部署。回滚时关闭 onboarding 并回退两张镜像；若回退到不认识新 YAML 段的旧 Server，须同步移除 `onboarding:` 段。无需数据库回滚。

注意：本地原有 `harness` 工作目录与主仓库锁定的子模块版本不同，本次未改动它；流水线需确认锁定提交可拉取，不能把本地旧 Harness 当本次更新发布。

## 本次验证记录

- Studio 全量：`bun run test -- --maxWorkers=2`，137 个文件、1,145 项通过。最后补充对话预算边界及转写错误提示后，相关 6 个文件、32 项再次通过。第一次默认并发测试遇到 worker 启动超时，降并发后解决。
- Studio 最终 `bun run build`（TypeScript + Vite）通过；已有大分包提示仍存在。
- Go 新增服务测试：通过。覆盖严格候选校验、虚构原文、供应商截断/超长/失败、语音 multipart、缺失转写字段、跨副本并发锁、账号与全局配额、Redis 故障关闭调用和对话预算。
- Go 新增配置 / Handler / Router 测试：通过。验证未登录拒绝、体积限制、错误脱敏、完整配置要求。
- 生产目标编译：Linux/amd64、`CGO_ENABLED=0` 的 Server 构建成功。
- **Go 全量已执行但未通过，不能作为合并绿灯。** 当前 Windows 缺少 CGO C 编译器，既有 SQLite 测试无法编译/运行；另有 Linux shell、文件权限、绝对路径、CRLF 文档拆分及打开文件替换等平台相关失败。需要技术合伙人在 Linux CI 执行 `go test -race ./...`，确认全部通过后再决定合并。
- 内置浏览器检查正式受保护路由：未登录会进入登录页。本次没有为验证新接口重复创建线上项目或提交付费内容任务。
- 真实语音供应商、正式 DeepSeek 配置和完整账号链路的预发布联调尚待配置后执行；本次不声称已经在线上跑通新接口。


## 随时转入创作（2026-10-10）

- 访谈中始终显示“开始创作”。6 项中达到 4 项（显示 67%）直接进入创作准备；不足时灰色按钮仍可点击，提示后可继续。完整度仅作建议，不作为保存门槛。
- 移除结束访谈和单独确认画像步骤。保存当前画像和付费生成仍由用户分别主动操作；允许在创作准备页补写选题，不会自动扣费或发布。
- 未提供维度保留待补充和证据边界。返回访谈可继续更新；再次保存使用原项目及版本检查，线上其他修改不会被覆盖，未确认的提交不会自动重试。
- 本次更新 Studio 界面和 Server 内嵌访谈提示词，需更新 Studio、Server 镜像；无数据库迁移、无新环境变量、无需更新 Agent 镜像。
