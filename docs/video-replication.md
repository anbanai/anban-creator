# 视频复刻运行与升级

视频复刻使用原版 [Hypit](https://github.com/hypit-ai/hypit)，Anban 负责托管身份、配置、调度、文件及 Studio，Agent 调度镜像内的官方 Skill。我们不维护 Hypit 引擎或 Provider 分支。

## 构建与启用

```sh
make docker-hypit-agent-image
make docker-hypit-smoke
```

独立镜像从官方完整 SHA 构建，默认 Linux amd64，源码位于只读 `/opt/hypit`，构建及 smoke 都校验原始文件的 SHA-256。锁定的依赖、官方媒体下载器、字体、HyperFrames 推荐浏览器都在构建时安装。Kubernetes 将任务调度到 Linux amd64 节点；Runner 把镜像预装的工具文件初始化到任务 HOME。`docker-hypit-smoke` 使用官方八秒示例，在断网、非 root、只读容器根目录和新任务 HOME 下验证检查、规划、渲染、完整解码及工程重新导入，不使用付费生成账户；它不等于真实任务端到端验收。

Server 的 `hypit.enabled` 默认关闭；内部验证阶段同时沿用平台的管理员访问限制。按 `server/config.example.yaml` 配置官方 Runtime Profile、环境凭据和镜像；设置 `ANBAN_AGENT_IMAGE_HYPIT`（线上使用 digest）。Provider 的选择、模型和路由完全使用原生 Profile；不要把它们写进用户的复刻表单，也不要将密钥放进项目说明或工程文件。

任务卷须允许执行预装浏览器和工具，不能挂载为 `noexec`；容器根目录仍可设为只读。

开启前按业务所需能力提供官方 Provider 和额度。在 Studio 创建“视频复刻”项目，上传参考或填写官方可下载链接，输入替换要求，提交一条任务。单任务提供成片、封面、project.json、project.zip、delivery-manifest.json、quality-report.json。定时计划复用相同输入校验。再次改编关联原任务并复用原版本和工程。

## 工程与恢复

工作目录固定 `/workspace/project`，任务输入为 `/workspace/input.json`，官方运行配置为 `/workspace/runtime-profile.json`。媒体先进入工程 assets，官方 Results 目录仍为 `.hypit/results`，不改写其格式或名称。

`project.zip` 包含源文件、Runs、素材、组件、依赖清单、完整 Results、分析说明、`restore/README.md` 和脱敏的 `restore/profile.template.json`，不包含凭据、运行中执行状态、缓存或 node_modules。使用 project.json 指定的原镜像版本，把工程挂到 `/workspace/project`；用自己的环境凭据配置运行环境，再通过官方 check/plan 验证。使用相同容器路径保留原有绝对引用；不能把解压到任意宿主目录当作完成迁移。

托管工程使用 npm 及 package-lock.json；恢复时启用 `--offline --ignore-scripts --bin-links=false`，防止包管理器改写只读上游 CLI 权限。原镜像缺少依赖时明确失败并要求重建镜像。含只读 Distribution 链接的 pnpm 工程暂不支持直接恢复，需要以匹配的 npm 工程依赖声明归档。

观察命令超时先查询同一 Build。容器中 Worker 退出后，其执行上下文已经丢失；保存任务卷中的 Results 和回执，通过官方 build-record/satisfy 在新 Build 中复用成功输出。未知远端提交结果不得盲目重发。任务卷丢失会明确失败，不伪装为从头恢复。

## 更新与回滚

1. 将 `HYPIT_SOURCE_REF` 改为经核查的官方完整 commit SHA，重建镜像；Skill 与源码随同版本更新。
2. 运行本地 smoke、Go/Runner/Studio 回归及真实 Provider 用例。
3. 更新 Server 镜像 digest，保留旧镜像供既有任务恢复和工程再次改编。
4. 回滚时恢复旧 digest；禁止在正在执行的容器内 git pull 或自动升级。

Agent 的创作规则只修改 `harness/packs/hypit/agent.claude.md`，随后 `make agent-pack-generate && make agent-pack-check`。Codex 原生适配读取生成的同一 Markdown 正文。修改 harness 时同时更新两份原生 manifest 版本。

## 验收记录

每次上线应保存：源码 SHA、镜像 digest、Provider Profile 的非敏感摘要、真实上传参考任务、链接参考任务、定时任务、一次恢复和再次改编的任务 id，以及可播放视频和质量报告。模拟测试或官方本地示例不能代替上述真实业务证据。

检查真实成片的比例、时长、完整解码、音画与替换要求；再次改编的 plan 应复用未修改媒体。Provider pricing 并非通用硬预算，Provider 账户硬配额须独立配置。费用只依据真实回执，不从 Agent 的描述估造。

首版固定上游的 `OperationReceipt` 只有请求 ID 和可选查询 URL，没有统一的实际金额或币种。工程保留原生回执供 Provider 账单对账；这些请求与平台积分结算、Claude 用量独立。目前不提供生成 Provider 的实际费用汇总，也不把 `pricing` 估价写成已扣款金额。

先进行内部验证。Hypit 的原许可证、包名、CLI 和来源记录保持完整；通用交付文件名不改变许可证义务，对外托管前取得上游要求的商业授权。

## 上线准备清单

本次允许在默认关闭状态合入 main。部署代码与启用业务分开：先发布并验证基础设施，再由内部管理员启用测试，对外开放另受商业授权和安全验收约束。

1. **发布物**：发布相互匹配的 Server、Studio 和独立 Agent 镜像。将 `ANBAN_AGENT_IMAGE_HYPIT` / `claude.runtime_images.hypit` 设为已推送的 registry digest，保存本次源码 SHA、Pack 版本和镜像清单；保留历史镜像供恢复和再次改编。
2. **数据库和调度**：备份数据库，验证 Server 启动时的增量迁移，确认 `projects.hypit_defaults`、`tasks.hypit_input`、`tasks.hypit_runtime_snapshot`、`plans.hypit_input` 及既有执行、交付、计费表完整。确认 `task.hypit` 三档 1600 / 2000 / 6000 积分 SKU、管理员测试余额、Redis 和计划调度可用。
3. **账户和配置**：保持 `hypit.enabled: false`，准备原生 `hypit.runtime-local@1` Profile、`media.local`、软件渲染与两个 Worker 的 `hyperframes.local`，以及所需生成、转录 Provider 的 endpoints / bindings 和环境凭据引用。准备 Claude 执行账户、execution-token secret、Agent 可访问的 Server / MCP 地址及 TLS CA。能力接口的 `configured` 只表示配置齐备，账户实际能力、余额和硬配额须另外验证；不得把 Provider 密钥写入工程或日志。
4. **执行资源**：每任务 Linux amd64、4 CPU、8 GiB 内存和 20 GiB 持久工作空间；任务卷必须允许执行。完整归档校验会临时展开工程并保存媒体快照，`/tmp` 另需至少 10 GiB 可写磁盘临时空间，不能使用受 8 GiB 内存限制的内存型 tmpfs 承载最大工程；Docker 需预留容器可写层磁盘，Kubernetes 需预留磁盘型 emptyDir / 节点 ephemeral-storage。Docker 部署配置 socket、宿主组和网络；Kubernetes 配置 ServiceAccount / RBAC、PVC、镜像拉取 Secret、CA 和节点资源。验证取消、Worker 退出、精确冻结超时及卷丢失时的失败行为。
5. **存储和传输**：配置 OSS 直传、STS / RAM、浏览器 CORS、对象读取及签名权限，持久保存工程和交付文件。实际验证 256 MiB 素材、512 MiB 成片、2 GiB 工程的上传和下载。Runner 优先使用内部 Server 地址；若经过公开 ingress 或 Studio 代理，必须为授权 artifact 路径单独配置请求大小、30 分钟传输时限和关闭请求缓冲。现有 Studio nginx 全局上限为 `25m`，不能直接承载大工程；不要无限放宽其他 API。
6. **真实验收**：内部管理员启用后，完成上传参考、链接参考、定时任务、中断恢复、再次改编五类真实任务。保存任务 ID、镜像 digest、脱敏配置摘要、成片和质量报告；验证完整解码并人工检查音画，确认未修改素材复用、未知付费请求不重复提交。工程须在新任务通过官方 check / plan。实际生成费用按原生请求回执和 Provider 账单独立对账，目前没有统一实际金额汇总。
7. **对外启用及回滚**：完成下述依赖安全核查、真实业务验收和商业授权后再开放。出问题时关闭入口、恢复上一 digest，保留任务卷、对象和请求回执，避免丢失恢复依据。

时长偏好未填写时继承项目默认，显式 `0` 表示跟随参考；20 个素材名额只计算补充素材，参考仍计入媒体总字节数。历史任务的再次改编按源任务的冻结配置和限制准入，但继续受当前全局关闭开关、权限和凭据有效性约束。

## 依赖安全核查（2026-09-21）

Runner 的生产依赖 `npm audit --omit=dev` 为 0 条公告。官方基线锁文件的 `pnpm audit --prod` 报告 9 条：1 高、6 中、2 低，不能声明依赖零漏洞，也没有为消除告警擅改上游代码或锁文件。

| 依赖与公告 | 当前托管路径核查 |
|---|---|
| nanoid 3.3.16，GHSA-2v37-7h3g-55p8（高） | 漏洞需要 customAlphabet / customRandom 接收大小 0；当前 PostCSS 使用 `nanoid/non-secure` 的 `nanoid(6)`，未发现该触发路径 |
| Hono 4.12.32（6 中、1 低） | 当前官方截图服务仅绑定 `127.0.0.1`，使用文件 GET / range 处理；所报 CORS、SSR memo、proxy、language、SSG、点号 body 及 query helper 路径未在该服务启用 |
| esbuild 0.27.7，GHSA-g7r4-m6w7-qqqr（低） | 公告针对 Windows 开发服务器；本托管镜像为 Linux，不启动该开发服务器 |

以上是当前固定版本和受限用途的可达性判断，不是对未来依赖或任意工程代码的保证。对外启用前重新扫描待发布镜像的系统包和依赖并复核适用性；优先采用上游修复版本，按“更新 SHA → 重建 → 验证 → 更新 digest”升级。不要暴露任务容器的本地渲染端口，也不要把内部管理员验证阶段直接改为不受限的公共代码执行服务。

本次另行尝试了 Docker Scout 镜像 CVE 扫描，但长时间停在索引阶段，已终止且没有得到报告。系统包扫描未验收，不能把 JavaScript 依赖审计或渲染 smoke 作为替代；上线前须对最终 registry digest 完成扫描和风险处置记录。

## 本次实现验证（2026-09-21）

当时验证的官方基线为 `5d257c5a50291398d2bca34afb93c22f1ab5c295`，CLI 为 `0.2.11`。本地已构建 `creator-agent-hypit:latest`，镜像 ID 为 `sha256:479c8a7c6a79f49cbc4a93e169ac26006f245b3ba75994503b56173604facb7d`（历史本地 image ID，不是已发布的 registry digest；不代表后续升级版本的验证结果）。

| 验证 | 结果 |
|---|---|
| 完整 Go 测试、Server 构建和 vet | 通过 |
| Runner 测试、类型检查、构建 | 192 项测试、674 个断言通过 |
| Studio 测试与构建 | 106 个文件、913 项测试通过 |
| Pack 生成一致性及工作流审计 | 通过，审计回归 9 项通过 |
| 双宿主 manifest 合同 | 5 项测试通过，版本均为 4.2.4 |
| 官方源码逐文件 SHA-256、非 root 检查 | 通过 |
| 断网、只读根目录、新 HOME 渲染与完整解码 | 通过，官方 540×960 示例 |
| 工程归档后重新导入、官方 check/plan | 通过 |
| 实际 TCP 大文件上传 | 600 MiB 定长和分块上传通过，约 300 KiB 分配量 |
| 慢速上传读取时限 | 实际 TCP 缩短时限回归通过；仅授权视频/工程上传延长到 30 分钟 |

独立审查发现的素材所有权、冻结快照、大工程交付、Worker 清理、凭据隔离和导入重名问题均已修复并通过回归。合并前追加审查覆盖实际归档字节的凭据检查、官方 check/plan 执行后的迟写、失败任务上传、原生 Result 值和转发依赖、冻结输入及绝对超时；已补充并重跑相应泄漏与兼容性探针。上传只使用与验证摘要一致的六项交付快照；失败只上传 Runtime 安全生成的约定诊断。Server/Studio 的显式零时长、历史任务能力准入及素材数量边界也已修复。Kubernetes 资源、节点选择和任务卷合同已测试；未实际部署 Kubernetes 集群。

**尚未完成真实业务验收。** 当前没有配置可执行真实托管任务的 Provider/平台账户，因此真实上传参考、链接参考、定时任务、中断恢复及再次改编的五类任务尚无端到端证据，真实业务成片的人工音画验收也未完成。功能继续默认关闭、限内部管理员验证；不能把上述本地 smoke 标记为业务链路已打通。

本次实现已按默认关闭的授权边界合入本地 main（实现提交 `a99fc18a`，harness `f32cb752`）。合并后重新运行 Go 全量测试 / build / vet、Runner 192 项测试 / typecheck / build、Studio 913 项测试 / build 和 Pack / 工作流检查，全部通过。未推送远端，未部署或启用服务。

## 依赖升级复核（2026-09-24）

构建默认源码升级为 `5a568f4be485ab5e735fe95533cd5f77a85c66ee`（Hypit `0.2.12`），包含上游异步 Provider 轮询错误处理修复。新验证镜像为 `creator-agent-hypit:review-20260924`，本地 image ID 为 `sha256:7c23c68b3535fcd2a312cd75fc34571ad72bf234017b94839b2a4f72b24e15dc`，不是 registry digest；旧镜像的历史验证记录不作为新版本证据。

- Go 全量测试、Server 构建、vet、模块校验和定向 MCP / 双宿主合同测试通过。
- Runner 208 项测试、类型检查和构建通过；`npm ci` 审计报告 0 条漏洞。
- Studio 961 项测试和构建通过。
- harness 456 项测试通过、1 项跳过；类型检查、构建、打包验证及 article / seednote Profile smoke 通过。离线打包首次因本机缺少 pnpm 元数据缓存失败，补齐缓存后原检查通过。
- Pack 生成一致性、工作流审计及其 9 项回归通过。
- 新镜像构建、官方源码逐文件校验、断网 / 非 root / 只读根目录的 540×960 示例渲染、完整解码、归档重新导入及官方 check / plan 全部通过。首次构建下载 `uv` 文件中断并被哈希校验拒绝，原命令重试后成功，未绕过校验。

本次只完成本地升级验证；未部署或启用业务，真实 Provider 任务的端到端验收仍未完成。
