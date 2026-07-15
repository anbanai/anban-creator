# Studio 统一 Prompt 输入组件设计

- 日期：2026-07-15
- 状态：已完成产品设计确认，待实施计划
- 适用入口：首页、创建任务、继续任务、克隆任务、创建/编辑计划、设计师
- 技术方向：AI Elements Prompt Input 交互内核 + Anban 业务适配层

## 1. 背景

Studio 目前有多套互不一致的 Prompt 与附件输入：

- 首页把 Prompt 与 `ReferenceMaterialInput` 分成上下两个区域；
- 创建任务和计划使用普通 `Input`/`Textarea`，仅部分任务类型另有参考素材区；
- 继续任务使用独立的原生文件输入和文件说明列表；
- 克隆任务直接执行，不能在确认前还原和编辑原输入；
- 设计师使用独立的 `DesignerPromptBar`，参考图保存在侧边栏材料坞中。

这些入口在文件选择、粘贴、拖拽、预览、上传状态、键盘行为和错误反馈上不一致。目标是让用户在所有主 Prompt 入口使用同一个稳定、简单、功能完整的输入组件，同时允许页面通过受控属性提供必要的上下文与业务差异。

## 2. 目标

### 2.1 体验目标

- 所有适用入口使用相同的组件骨架、基础高度、附件展示和工具位置。
- Prompt 与文件在同一个输入面板内共同提交。
- 支持 `+` 文件选择、剪贴板文件/截图粘贴、组件内拖拽和浏览器全屏拖拽。
- 附件可点击预览；图片支持缩放、切换、下载和关闭。
- 组件从统一最小高度开始，随 Prompt 和附件自动向上增长；达到可视区域上限后内部滚动。
- 输入面板上方可显示项目选择、执行位置、分支、模型等页面上下文。
- 克隆任务还原原 Prompt 和全部原始输入素材，用户可直接确认或修改后创建新任务。
- 继续任务始终从空输入开始，把本次输入追加到当前任务和工作区。
- 设计师使用同一输入组件，并把参考图收敛到组件附件区。

### 2.2 技术目标

- 复用 AI Elements `PromptInput`/`Attachments` 的成熟交互模式，不安装无关聊天组件和依赖。
- 保留 Anban 现有 OSS 直传、pending upload、`EntryAttachment` 和执行器素材落盘契约。
- 上传后的持久化身份以 OSS object key 为准，不把临时签名 URL 当作持久化数据。
- 预览和下载时由 Studio 携带 key/上传身份向 Server 请求新的完整签名 URL。
- 页面不再分别实现文件准入、粘贴、拖拽、预览和上传状态机。
- 行为变更由 Studio、Handler、Service 和浏览器测试覆盖。

## 3. 非目标

- 不把视频源、商品图、蒙太奇源素材等具有专门业务语义的上传区混入通用附件。
- 不引入 AI Elements 的聊天消息、模型选择、语音、截图或会话状态管理。
- 不使用第三方公共 URL 或客户端拼接 OSS 地址绕过 Server 签名。
- 不让通用组件直接调用任务、计划、设计师等业务 API。
- 不改变继续任务复用当前任务与工作区的业务语义。
- 不改变克隆任务创建新任务、重新计费和保留来源关系的业务语义。

## 4. 统一组件

新增 `AgentPromptInput`，由 Studio 所有适用入口直接使用。

### 4.1 受控值

组件使用统一受控模型：

```ts
interface AgentPromptValue {
  prompt: string
  attachments: PromptAttachment[]
}

type PromptAttachmentStatus =
  | 'local'
  | 'uploading'
  | 'uploaded'
  | 'inherited'
  | 'failed'

interface PromptAttachment {
  id: string
  type: InputAttachmentType
  status: PromptAttachmentStatus
  file?: File
  fileName: string
  contentType?: string
  size?: number
  uploadId?: string
  key?: string
  instruction?: string
  progress?: number
  error?: string
}
```

`PromptAttachment` 不持久化签名 URL。组件可以为本地文件创建短期 Object URL，也可以通过签名解析器为已上传或继承附件取得短期远程预览 URL。

### 4.2 组件属性

核心属性保持明确，不允许页面依靠内部实现细节定制：

```ts
interface AgentPromptInputProps {
  value: AgentPromptValue
  onChange: (value: AgentPromptValue) => void
  onSubmit: (value: AgentPromptValue) => void | Promise<void>
  attachmentAdapter: PromptAttachmentAdapter
  attachmentPolicy: PromptAttachmentPolicy
  contextBar?: ReactNode
  leadingTools?: ReactNode
  trailingTools?: ReactNode
  status?: ReactNode
  placeholder?: string
  submitLabel: string
  submitIcon?: LucideIcon
  submitting?: boolean
  disabled?: boolean
  autoFocus?: boolean
}
```

`contextBar` 位于输入主体上沿。项目选择是默认上下文项；页面可传可选择、只读或隐藏状态。`leadingTools` 和 `trailingTools` 只扩展工具栏，不改变附件区、文本区、`+` 与提交按钮的稳定顺序。

### 4.3 视觉与尺寸

- 所有页面共享一个最小主体高度，不提供 compact/large 视觉分支。
- Prompt 输入随内容自动增长，附件换行也增加组件高度。
- 最大高度使用可视区域约束；达到上限后附件区和文本区在组件内部滚动。
- 附件位于顶部，Prompt 位于中部，底部左侧是 `+`，右侧是圆形提交按钮。
- 上下文栏紧贴组件上沿，但与主体保持清晰边界。
- 桌面和移动端使用同一信息层级；移动端工具项可以收进菜单，但不可隐藏文件添加和提交。

### 4.4 输入行为

- 点击 `+` 打开允许多选的系统文件选择器。
- 粘贴包含文件时添加文件；剪贴板中的普通文本按浏览器默认行为进入 Prompt。
- 组件内拖入和全屏拖入调用同一个文件准入函数。
- Enter 提交，Shift+Enter 换行；输入法合成期间不得提交。
- 上传中、存在失败附件或页面业务校验未通过时禁止提交。
- 选择、粘贴和拖入之间按 `name + size + lastModified + type` 去重。
- 每个被拒绝文件都返回明确原因，不静默截断。

### 4.5 全屏拖拽协调器

新增全局协调器，登记当前可接收文件的 `AgentPromptInput`：

1. 最上层打开的 Dialog 中组件优先；
2. 否则使用最近获得焦点的可见组件；
3. 只有外部 `Files` 拖拽激活遮罩；
4. 内部附件排序、文本、链接和生成图片拖动不激活；
5. drop、Escape、最后一次 dragleave、组件卸载或策略变为不可接收时清理状态。

协调器使用 drag-depth 计数避免跨子元素移动时遮罩闪烁。遮罩显示可接收类型、文件数和剩余容量。

## 5. 附件上传与 OSS 契约

### 5.1 上传

通用附件继续使用 `POST /uploads/prepare` 获取由 Server 授权的 OSS 直传信息。Studio 使用 `ali-oss` 上传到 Server 分配的 object key。上传目的默认是 `ai_entry_attachment`；设计师使用 `designer_reference`。

Server 必须继续校验：

- 当前用户；
- upload purpose；
- 文件名、MIME、扩展名和大小；
- pending upload 的 upload ID、object key、有效期和归属。

Studio 不自行生成 object key，也不把 OSS credential、bucket 规则或签名逻辑写入组件。

### 5.2 持久化

新写入的存储型 `EntryAttachment` 以以下字段为持久化事实：

```json
{
  "type": "image",
  "upload_id": "verified-upload-id",
  "key": "uploads/pending/<user>/<upload>/<file>",
  "file_name": "product.png",
  "content_type": "image/png",
  "size": 123456,
  "instruction": "保留包装正面"
}
```

Handler 不信任客户端提交的 `upload_id` 或 `key`。它使用 pending upload 记录核验二者和用户归属，finalize 后把 Server 记录中的 upload ID/key 写入标准化附件。临时 `public_url`、上传 URL和签名下载 URL不进入新数据快照。

当前数据库中的 URL-only 附件仍需可读。Server 在读取时从受信任的自有 URL 提取 key；新写入只采用 key-first 形式。

### 5.3 签名解析

新增固定的认证接口用于附件预览和下载：

```text
POST /api/v1/uploads/resolve-download-url
```

请求包含 `upload_id` 和 `key`；对任务/计划内的继承附件还包含资源类型和资源 ID。Server 在签名前必须验证：

- pending upload 属于当前用户且 key 完全匹配；或
- key 确实被当前用户拥有的任务/计划引用；
- key 属于配置的存储提供方；
- 请求用途允许预览或下载。

响应返回完整 URL 和过期时间：

```json
{
  "url": "https://bucket.oss-...?...signature...",
  "expires_at": "2026-07-15T12:00:00Z"
}
```

Studio 按 attachment key 缓存签名结果，在过期前复用；预览失败且签名已过期时只刷新一次。组件不得把返回 URL写回表单值或业务实体。

Agent bootstrap 和执行器下载也改为 key-first：有经过 Server 验证的 upload ID/key 时直接由存储服务签发下载 URL；URL-only 历史记录继续走受信任 URL 提取 key 的读取路径。

### 5.4 设计师引用注册

设计师不再把浏览器文件 multipart 上传到 Server。Studio 使用 `designer_reference` purpose 直传 OSS 后调用：

```text
POST /api/v1/designer/register-reference
```

请求包含 `upload_id` 和 `key`。Server 从 pending upload 记录验证当前用户、purpose、key、文件类型和完成状态，然后返回生成接口需要的 `file_id`。`file_id` 只作为设计师生成期标识；持久化存储身份仍是 object key。现有 URL 导入只用于已经存在且经过 Server 所有权校验的内部图片，不作为新上传文件的主路径。

## 6. 预览

统一 `AttachmentPreviewDialog` 根据附件类型选择渲染器：

- 图片：全屏查看、前后切换、缩放、适配窗口、下载和关闭；
- 视频：原生 video controls；
- 音频：原生 audio controls；
- PDF：使用签名 URL 在浏览器可用的嵌入预览中打开；
- Word、Excel、PowerPoint 等浏览器不能可靠渲染的文件：显示文件名、类型、大小和下载操作；
- 文本：在大小安全限制内请求并显示纯文本，否则只下载。

本地文件预览使用 Object URL，并在附件删除、替换、提交清空和组件卸载时释放。远程预览只使用 Server 返回的签名 URL。

## 7. 页面映射

### 7.1 首页

- 保留项目选择、内容类型和本地/云端执行信息，移入或组合为 `contextBar`。
- 支持图片、音频、视频、文档和文本。
- 提交时把已核验的 key-first 附件交给 AI Entry。

### 7.2 创建任务

- 所有任务类型的主 Prompt 使用统一组件。
- 项目可选择；类型、执行位置等可放在上下文栏或表单相邻区域。
- 通用附件支持全部统一类型，并进入 `Task.InputAttachments`。
- 视频源、商品图和蒙太奇源素材保留专用结构化输入。

### 7.3 创建与编辑计划

- Prompt 和附件组成计划输入快照。
- 项目可选择。
- 更新继续区分字段省略、空数组和非空数组。
- 每次触发计划时把附件 key 快照复制到新任务，不复制或持久化旧签名 URL。

### 7.4 继续任务

- 项目上下文只读。
- 每次打开都是空 Prompt、空附件。
- 提交使用当前 resume multipart/持久化流程，把本次输入追加到当前任务和工作区。
- 继续任务新增文件最终也必须保存到 OSS，并在 `EntryAttachment` 中记录 object key；不得持久化签名 URL。

### 7.5 克隆任务

- 点击克隆先打开确认 Dialog，不再立即创建。
- 项目默认固定为原项目，显示为只读上下文。
- 回填原任务 Prompt 和所有非 resume 原始输入附件。
- 用户可以编辑 Prompt、删除原附件和添加新附件。
- 提交最终完整快照；Service 创建新任务、保留 `InputSourceTaskID` 和冻结配置并重新计费。
- Handler/Service 接受经过验证的 Prompt/附件覆盖参数，不能由客户端重建或丢失原任务的其他冻结配置。

### 7.6 设计师

- 用统一组件替换 `DesignerPromptBar`。
- 现有侧边栏参考图材料坞移除，避免重复上传入口；设置面板继续保留模型和输出参数。
- 附件策略只允许当前模型支持的参考图片，并使用 `maxReferenceImages` 限制。
- 设计师本地参考文件在提交生成时上传到 OSS，Server 返回 `file_id` 供生成接口使用。
- 上下文栏提供项目选择和“无项目”；实际 `project_id` 写入生成请求和历史记录。
- 编辑模式可通过 `submitLabel`、`submitIcon`、placeholder 和 status props 显示编辑/取消状态，但不改变组件骨架。

## 8. 错误处理与可访问性

- 单文件失败保留附件卡片、原因、重试和移除操作，不清空其他输入。
- 上传中禁止提交；提交请求使用已有 mutation 防重机制。
- 签名解析返回 403 时显示无权访问，不降级为直接访问 OSS key。
- OSS/STS 不可用时沿用现有明确的管理员配置提示。
- 所有图标按钮有可访问名称和 Tooltip；附件删除、重试、预览和下载可键盘操作。
- Dialog 有标题；全屏预览支持 Escape；焦点关闭后回到触发附件。
- 组件通过 aria-live 宣告上传完成、拒绝原因和全屏拖入目标变化。
- 输入法合成、移动端软键盘和 reduced-motion 均必须验证。

## 9. 测试

### 9.1 Studio 单元测试

- 文件选择、粘贴、组件内拖拽和全屏拖拽进入同一准入函数；
- 最上层 Dialog/最近焦点的路由优先级；
- 类型、大小、数量、去重和设计师 provider 容量；
- 上传进度、单文件失败、重试、删除和防重复提交；
- Enter、Shift+Enter 和输入法合成；
- 自动增高、最大高度和内部滚动；
- Object URL 创建与释放；
- 签名 URL缓存、过期刷新和禁止写回受控值；
- 项目栏和工具栏 slots 不改变基础结构。

### 9.2 页面集成测试

- 首页提交项目、Prompt 和 key-first 附件；
- 创建任务和计划提交通用附件；计划编辑保留 omitted/empty/replace 语义；
- 继续任务每次打开为空并提交本次输入；
- 克隆还原原输入并提交最终快照；
- 设计师项目选择、模型附件限制、参考上传和生成请求；
- 现有专用视频/商品/蒙太奇素材不回退。

### 9.3 Go 测试

- prepare upload 仅允许 OSS，并执行 purpose、类型和大小策略；
- Handler 从 pending upload Server 记录派生 upload ID/key，不信任客户端；
- 签名解析验证用户、资源引用、upload ID和 key；
- 任务/计划允许统一附件类型并保存 key-first 快照；
- 计划触发复制 key 而不是签名 URL；
- 克隆覆盖 Prompt/附件但保留冻结配置、来源和计费语义；
- resume 附件写入 OSS key；
- agent bootstrap 使用 key 获取短期下载 URL；
- URL-only 当前数据仍可安全读取，外部或其他租户 key 不可签名。

### 9.4 浏览器验证

- 桌面和移动端：首页、任务 Dialog、计划 Dialog、继续/克隆 Dialog 和设计师；
- 项目选择弹层、自动增长、可视区域上限和无重叠；
- DataTransfer 模拟全屏拖入、焦点路由和拒绝状态；
- 图片缩放/下载、视频/音频/PDF 预览和文档降级；
- 控制台无错误，上传/预览请求不泄露 credential 或持久化签名 URL。

## 10. 完成标准

- 六个适用入口全部使用 `AgentPromptInput`，不再保留平行的主 Prompt/附件实现。
- 基础高度、附件区、Prompt 区、`+` 和提交按钮在各入口一致。
- 项目上下文按页面支持可选择、只读或“无项目”。
- 选择、粘贴、局部拖入和全屏拖入行为一致。
- 所有支持类型具备预览或明确下载降级。
- 新上传附件只持久化经过 Server 验证的 OSS key/上传身份。
- 所有完整远程 URL均由 Server 根据 key 临时签发，不进入任务、计划或组件持久化状态。
- 克隆和继续任务分别符合“完整还原后编辑”和“空输入追加”的确认语义。
- Studio 测试、Studio 构建、Go 全量测试、相关二进制构建和浏览器验证全部通过。
