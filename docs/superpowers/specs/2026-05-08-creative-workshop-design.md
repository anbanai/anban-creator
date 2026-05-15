# 创意工坊 & 模板系统设计文档

> 日期：2026-05-08
> 状态：待审核

## 背景

Anbanwriter 现有内容创作能力覆盖微信公众号文章、种草笔记笔记、小绿书图片帖。用户提出 4 项新功能需求，统一纳入"创意工坊"产品模块：

1. **商业海报** — 用户输入文字内容，AI 自动排版成海报
2. **复刻种草笔记爆款** — 基于现有 seednote 管道增强，增加模板持久化
3. **拆解爆文** — 输入笔记/主页链接，AI 多维度输出分析报告
4. **模板系统** — 管理员在数据库配置模板，Studio 展示供用户选择

## 架构方案

**Studio 前端驱动 + 后端 API 扩展**

- Studio 新增 2 个页面模块，后端新增模板/海报/拆解 API
- 海报生成复用现有 `app/image/` Provider 接口
- 复刻复用现有 `tasks` 管道和 `seednote` Agent
- 拆解复用现有 `server/platform/seednote.go` 抓取能力

## Studio 页面

| 页面 | 路由 | 功能 |
|------|------|------|
| **创意工坊** | `/workshop` | 海报制作 + 爆文拆解 + 爆款复刻，Tab 切换 |
| **模板库** | `/templates` | 浏览/筛选/预览管理员配置的模板（图+文） |

侧边栏新增这 2 个入口。

## 数据模型

所有 ID 使用 `string` + `char(36)` + UUID，与现有模型一致（`server/model/task.go` 等统一使用此约定）。

### `templates` 表

统一模板存储，支撑海报、种草笔记复刻、后续文章/小绿书模板。

```go
type Template struct {
    ID            string         `gorm:"type:char(36);primaryKey" json:"id"`
    Type          string         `gorm:"type:varchar(20);not null" json:"type"`       // "poster" / "seednote" / "article" / "xls" (xls = 小绿书)
    Name          string         `gorm:"type:varchar(100);not null" json:"name"`
    Category      string         `gorm:"type:varchar(50)" json:"category"`            // 行业/场景分类
    ThumbnailURL  string         `gorm:"type:varchar(500)" json:"thumbnail_url"`
    Structure     string         `gorm:"type:json;serializer:json" json:"structure"`  // 模板结构定义
    StylePrompt   string         `gorm:"type:text" json:"style_prompt"`               // AI 风格提示词
    ExampleContent string        `gorm:"type:json;serializer:json" json:"example_content"` // 示例内容
    Tags          string         `gorm:"type:json;serializer:json" json:"tags"`       // 标签数组
    SortOrder     int            `gorm:"default:0" json:"sort_order"`
    IsActive      bool           `gorm:"default:true" json:"is_active"`
    CreatedAt     time.Time      `json:"created_at"`
    UpdatedAt     time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }
```

### `viral_analyses` 表

爆文拆解报告存储。

```go
type ViralAnalysis struct {
    ID             string    `gorm:"type:char(36);primaryKey" json:"id"`
    UserID         string    `gorm:"type:char(36);not null;index" json:"user_id"`
    SourceType     string    `gorm:"type:varchar(20);not null" json:"source_type"` // "note" / "profile"
    SourceURL      string    `gorm:"type:varchar(500);not null" json:"source_url"`
    SourceData     string    `gorm:"type:json" json:"source_data"`                 // 原始抓取数据
    AnalysisResult string    `gorm:"type:json" json:"analysis_result"`             // AI 拆解结果
    Status         string    `gorm:"type:varchar(20);not null;default:'pending'" json:"status"` // "pending" / "analyzing" / "completed" / "failed"
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}

func (ViralAnalysis) TableName() string { return "viral_analyses" }
```

`analysis_result` JSON 结构：

```json
{
  "title_analysis": {
    "score": 85,
    "technique": "数字+痛点+悬念",
    "breakdown": "...",
    "rewrite_suggestions": ["...", "..."]
  },
  "cover_analysis": {
    "score": 90,
    "style": "高颜值生活照",
    "breakdown": "...",
    "key_elements": ["大字标题", "对比色"]
  },
  "copywriting_analysis": {
    "score": 78,
    "structure": "钩子-干货-互动",
    "breakdown": "...",
    "formulas_used": ["AIDA"],
    "word_count": 850
  },
  "tag_analysis": {
    "score": 70,
    "tags": ["#美妆", "#护肤"],
    "breakdown": "...",
    "suggested_tags": ["..."]
  },
  "interaction_analysis": {
    "score": 82,
    "techniques": ["提问引导", "争议点"],
    "breakdown": "...",
    "cta_type": "评论引导"
  },
  "overall_score": 81,
  "viral_factors": {
    "topic": 35, "title": 25, "content": 20,
    "visual": 10, "interaction": 10
  },
  "suggestions": ["可复刻要点1", "..."]
}
```

### `poster_tasks` 表

海报生成任务。

```go
type PosterTask struct {
    ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
    UserID          string    `gorm:"type:char(36);not null;index" json:"user_id"`
    TemplateID      *string   `gorm:"type:char(36)" json:"template_id"`              // nullable，null = 纯 AI 生成
    InputContent    string    `gorm:"type:json" json:"input_content"`                // 用户输入（标题、卖点、品牌信息）
    StylePreference string    `gorm:"type:varchar(50)" json:"style_preference"`      // 风格偏好
    Images          string    `gorm:"type:json" json:"images"`                       // [{url, width, height}]
    Conversation    string    `gorm:"type:json" json:"conversation"`                 // Vercel AI SDK 对话历史
    Status          string    `gorm:"type:varchar(20);not null;default:'drafting'" json:"status"` // "drafting" / "generating" / "completed" / "failed"
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}

func (PosterTask) TableName() string { return "poster_tasks" }
```

### AutoMigrate 注册

在 `server/model/model.go` 的 `AutoMigrate()` 函数中追加 `&Template{}`, `&ViralAnalysis{}`, `&PosterTask{}`。

## API 路由

所有 `/api/v1/*` 路由走 JWT 认证 + 速率限制，与现有 `server/router/router.go` 一致。

```
# 模板（JWT 认证，所有用户可读）
GET    /api/v1/templates              — 模板列表（支持 type/category/tag 筛选，offset+limit 分页）
GET    /api/v1/templates/:id          — 模板详情

# 海报（JWT 认证，用户级）
POST   /api/v1/posters                — 创建海报任务（扣积分）
POST   /api/v1/posters/:id/generate   — 基于对话内容生成海报图片（扣积分）
GET    /api/v1/posters/:id            — 获取海报详情（含对话+图片）
GET    /api/v1/posters                — 海报历史列表（offset+limit 分页）

# 爆文拆解（JWT 认证，用户级）
POST   /api/v1/viral-analyses         — 提交拆解任务（扣积分）
GET    /api/v1/viral-analyses/:id     — 获取拆解报告
GET    /api/v1/viral-analyses         — 拆解历史列表（offset+limit 分页）

# 爆款复刻（复用现有 tasks 管道）
POST   /api/v1/tasks                  — 增加 template_id 和 clone_depth 参数
```

### 分页格式

所有列表接口使用 `offset` + `limit` 查询参数，返回 `{"items": [...], "total": N}`，与现有 `server/handler/task.go` 分页模式一致。

## 积分系统

在 `server/model/constants.go` 中新增积分类型：

```go
CreditTypePosterGeneration  = "poster_generation"   // 海报生成
CreditTypeViralAnalysis     = "viral_analysis"       // 爆文拆解
```

积分规则：
- 海报生成：每次生成扣 1 积分（2-3 张变体算 1 次）
- 爆文拆解：每次分析扣 1 积分
- 爆款复刻：复用现有 task 积分规则
- 失败不扣积分（生成/分析失败时回退）

## 功能 1：商业海报

### 交互流程

1. **选模板（可选）** — 左侧展示模板库缩略图，可跳过
2. **填写内容** — 输入标题、卖点（多条）、品牌/价格信息
3. **选风格** — 风格标签（简约/国潮/ins风/商务等）
4. **AI 生成** — 后端通过 `app/image/` 管道生成 2-3 张海报变体
5. **迭代调整** — 用户对话微调（"背景换个颜色"、"字大一点"），Vercel AI SDK `useChat` 管理状态
6. **下载/导出** — 下载或保存到任务历史

### 技术实现

**前端**：
- 新增依赖：`ai` (Vercel AI SDK)
- 使用 `useChat` hook 管理对话流
- 对话消息通过 `POST /api/v1/posters/:id/generate` 发送到后端
- 后端返回生成的图片 URL

**后端**：
- `server/service/poster.go` — 解析用户输入，加载模板 structure + style_prompt，组合 prompt 发给 `app/image/` 生成图片
- 复用现有 Provider 接口（OpenAI/Gemini/Volcengine）

**海报文字排版策略**：

海报的核心是将用户文字内容（标题、卖点）渲染到图片上。采用 **AI 原生文字渲染** 方案：

- 将用户文字内容结构化嵌入图片生成 prompt（如：`"一张 3:4 竖版海报，顶部大标题'XXX'，中间三个卖点要点，底部品牌名'YYY'，风格：简约温暖"`）
- 依赖 DALL-E 3 / Gemini 的原生文字渲染能力直接生成含文字的图片
- 迭代调整时通过对话修改 prompt 重新生成

此方案的优势：无需后端图片合成逻辑，完全复用现有 `app/image/` Provider 接口。劣势是 AI 生成的文字可能偶尔有误，用户可通过对话迭代修正。

**Prompt 策略**：
- 用户文字内容（标题、卖点）结构化嵌入 prompt
- 指定海报布局规则：标题位置、正文区域、留白比例
- 风格标签映射到视觉参数（配色、字体风格、装饰元素）

### 边界

- 不做拖拽编辑器
- 不做品牌素材库管理
- 先做种草笔记 3:4 竖版海报
- 不做后端文字叠加/图片合成（依赖 AI 原生渲染）

## 功能 2：复刻种草笔记爆款

### 交互流程

两种入口：
1. **从拆解报告跳转** — 一键"复刻此笔记"，自动带入分析数据
2. **从模板库选择** — 选择种草笔记模板开始

### 技术实现

- **模板持久化**：复刻完成后提取的 5 维模板保存到 `templates` 表（type = "seednote"）
- **前端触发**：`POST /api/v1/tasks` 增加 `template_id` 和 `clone_depth` 参数
- **Agent 侧**：现有 `claudecode/agents/seednote.md` replicate 模式不变，新增 MCP 工具 `save_template` 持久化模板
- **模板保存时机**：Agent 在复刻任务完成后通过 MCP `save_template` 工具将提取的模板写入 `templates` 表。前端也可在任务完成后提供"保存为模板"按钮，触发 `POST /api/v1/tasks/:id/save-template`
- **新增 MCP 工具**：`save_template`、`list_templates`、`get_template`

### 数据流

```
拆解报告 → 保存为模板 → templates 表
     ↓                        ↓
  一键复刻 ← 模板库选择 ← GET /api/v1/templates
     ↓
POST /api/v1/tasks (template_id, clone_depth)
     ↓
Agent 执行现有 replicate 管道
     ↓
任务完成 → Agent 调用 save_template MCP 工具持久化模板
```

## 功能 3：拆解爆文

### 交互流程

1. **输入链接** — 粘贴种草笔记笔记分享链接或主页链接
2. **抓取数据** — 调用 `server/platform/seednote.go` 抓取
3. **AI 拆解** — 5 维度分析，生成结构化报告
4. **展示报告** — 卡片+评分环形图+总分雷达图
5. **一键复刻** — 底部 CTA 跳转复刻流程

### 技术实现

**后端**：
- `server/service/viral_analysis.go` — 解析 URL → 抓取数据 → AI 分析 → 存储
- 主页链接批量抓取近期笔记，取互动量最高 3-5 篇
- 分析 prompt 基于现有 `seednote-writing/references/viral-elements.md` 的 5 要素权重
- 抓取失败处理：对无效/被屏蔽的 URL 返回明确错误码，不做重试爬取

**前端**：
- 输入框 + 提交按钮（顶部）
- 5 个维度卡片，每个含评分环形图 + 文字分析
- 底部总分雷达图 + "复刻此笔记" CTA
- 左侧历史列表

### 边界

- 不做竞品对比分析
- 不做自动监控
- 不做预测评分

## 功能 4：模板系统

### 管理端

- 管理员直接操作数据库填充模板数据
- 后续可扩展 Admin API

### 模板 structure 示例

海报模板：
```json
{
  "layout": "top-title-center",
  "title": { "position": "top", "max_chars": 20 },
  "selling_points": { "count": 3, "style": "bullet" },
  "color_scheme": "warm",
  "image_slots": 1
}
```

种草笔记模板：
```json
{
  "title_template": "数字+情绪词+话题",
  "cover_style": "高颜值/对比/拼图",
  "body_structure": "hook-content-cta",
  "interaction_design": "提问引导",
  "tag_strategy": "大词+长尾词+品牌词"
}
```

### Studio 前端

- **筛选栏**：按类型、分类、标签筛选
- **网格展示**：缩略图 + 模板名 + 使用次数
- **点击预览**：弹窗展示详情（预览图 + 结构说明 + 示例内容）
- **使用模板**：CTA 跳转到对应创作页面

### 添加种草笔记频道时推荐模板

- 频道创建完成后，`POST /api/v1/channels` 返回 `recommended_templates`
- 匹配逻辑：频道画像分析结果（行业、风格）与模板 `category` + `tags` 匹配
- 前端弹出推荐卡片，用户可选可跳过

## 新增/修改文件清单

### 后端新增文件

```
server/
├── model/
│   ├── template.go              — Template GORM 模型
│   ├── viral_analysis.go        — ViralAnalysis GORM 模型
│   └── poster_task.go           — PosterTask GORM 模型
├── repository/
│   ├── template.go              — TemplateRepository 接口实现
│   ├── viral_analysis.go        — ViralAnalysisRepository 接口实现
│   └── poster_task.go           — PosterTaskRepository 接口实现
├── service/
│   ├── template.go              — TemplateService（推荐匹配逻辑）
│   ├── viral_analysis.go        — ViralAnalysisService（拆解业务逻辑）
│   └── poster.go                — PosterService（海报生成逻辑）
├── handler/
│   ├── template.go              — TemplateHandler（模板 API）
│   ├── viral_analysis.go        — ViralAnalysisHandler（拆解 API）
│   └── poster.go                — PosterHandler（海报 API）
└── mcp/
    └── template_tools.go        — save_template / list_templates / get_template
```

### 后端修改文件

```
server/model/model.go            — AutoMigrate() 追加 3 个模型
server/model/constants.go        — 新增 CreditTypePosterGeneration, CreditTypeViralAnalysis
server/repository/repository.go  — Repository 接口新增 Templates/ViralAnalyses/PosterTasks 访问器
                                   + TemplateRepository/ViralAnalysisRepository/PosterTaskRepository 子接口
                                   + repository struct 新字段
                                   + New() 构造函数实例化
                                   + txRepository 事务镜像
server/mcp/tools.go              — RegisterTools() 追加 registerTemplateTools(server)
server/mcp/mcp.go                — Services struct 追加 TemplateSvc 字段
server/router/router.go          — Services struct 追加 TemplateHandler/ViralAnalysisHandler/PosterHandler
                                   + 路由注册块
server/services.go               — setupCoreServices() 追加新 service 实例化
server/handlers.go               — setupHandlers() 追加新 handler 实例化
server/service/channel.go        — CreateChannel 返回 recommended_templates
```

### 前端新增文件

```
studio/src/
├── pages/
│   ├── WorkshopPage.tsx          — 创意工坊（海报+拆解+复刻）
│   └── TemplatesPage.tsx         — 模板库
├── components/
│   ├── workshop/
│   │   ├── PosterTab.tsx         — 海报制作面板
│   │   ├── ViralAnalysisTab.tsx  — 爆文拆解面板
│   │   ├── CloneTab.tsx          — 爆款复刻面板
│   │   ├── PosterChat.tsx        — Vercel AI SDK 对话组件
│   │   └── AnalysisReport.tsx    — 拆解报告展示
│   ├── templates/
│   │   ├── TemplateGrid.tsx      — 模板网格
│   │   ├── TemplateCard.tsx      — 模板卡片
│   │   └── TemplatePreview.tsx   — 模板预览弹窗
│   └── channels/
│       └── TemplateRecommend.tsx  — 频道创建后推荐卡片
├── lib/api/
│   ├── templates.ts              — 模板 API
│   ├── posters.ts                — 海报 API
│   └── viral-analyses.ts         — 拆解 API
└── types/
    ├── template.ts
    ├── poster.ts
    └── viral-analysis.ts
```

### 前端修改文件

```
studio/src/lib/api/index.ts      — api 对象追加 templates/posters/viralAnalyses 方法
studio/src/types/index.ts        — re-export 新类型
```

### 新增依赖

```
studio/package.json              — 新增 "ai" (Vercel AI SDK)
```

## 验证计划

1. **模型迁移**：启动 server，确认 3 张新表自动创建
2. **模板 API**：curl 测试 GET 列表（分页+筛选）+ GET 详情
3. **积分扣减**：curl 测试海报生成和爆文拆解的积分扣减 + 失败回退
4. **海报生成**：提交内容 → 检查图片包含用户文字 → 迭代对话生效
5. **爆文拆解**：提交真实种草笔记链接 → 5 维度分析完整、评分合理
6. **爆款复刻**：拆解报告跳转复刻 → 生成图文风格一致 → 模板持久化
7. **频道推荐**：创建频道 → 推荐模板与账号画像匹配
8. **E2E**：Studio 前端全流程走通

## 数据清理

与现有 tasks 的 `FindCompletedOlderThan` 清理机制对齐：
- `poster_tasks`：完成后 30 天自动清理
- `viral_analyses`：完成后 90 天自动清理
- 在 `server/workers.go` 的周期清理任务中追加这两项
