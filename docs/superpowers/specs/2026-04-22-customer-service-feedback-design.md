# 客服与反馈功能设计

## Context

Studio 前端目前缺少用户与开发者之间的沟通渠道。用户遇到问题或有新功能需求时无法快速反馈，导致问题发现滞后、用户流失。需要增加两个轻量功能：企微客服二维码（即时联系）和应用内反馈表单（异步收集），以最低使用门槛让用户第一时间触达开发者。

## 设计概要

采用**右下角单悬浮按钮 + 向上展开面板**方案。一个 FAB 入口，面板内通过 tab 切换「联系客服」和「意见反馈」两个功能。

## 详细设计

### 1. FAB 悬浮按钮

- **位置**：`fixed bottom-6 right-6`，z-index 50
- **样式**：圆形 primary 按钮，`MessageCircle` 图标（lucide-react），带阴影
- **交互**：点击切换展开/收起面板，点击面板外部区域关闭面板
- **显示范围**：仅在已登录的 AppLayout 内显示

### 2. 展开面板

- **定位**：从 FAB 向上弹出，`absolute bottom-full right-0 mb-2`
- **尺寸**：宽 320px
- **结构**：顶部两个 tab（「联系客服」「意见反馈」），使用已有 tabs 组件
- **样式**：圆角卡片，边框 + 阴影，与项目整体风格一致

### 3. 联系客服 Tab

- 中央展示企微二维码图片（200x200px，居中）
- 二维码下方提示文字：「扫码添加企业微信客服」（text-sm text-muted-foreground）
- 图片来源：静态文件 `studio/public/wechat-qr.png`

### 4. 意见反馈 Tab

**表单字段（极简）：**
- 类型选择：两个 Tag 按钮「问题反馈」「功能建议」，单选，默认「问题反馈」
- 文本框：Textarea，placeholder "请描述您遇到的问题或想要的功能..."，3-4 行
- 提交按钮：底部 primary 按钮 "提交反馈"

**提交流程：**
1. 点击提交 → POST `/api/v1/feedback`，body: `{ type: "bug" | "suggestion", content: string }`
2. 成功 → sonner toast 提示 + 清空表单
3. 失败 → sonner toast 报错
4. 提交中按钮 loading，防重复提交

### 5. 后端 API

**Endpoint：** `POST /api/v1/feedback`
- 需要 JWT 认证
- Body: `{ "type": "bug" | "suggestion", "content": string }`
- 校验：content 非空且长度 1-1000
- 响应：201 Created

**数据模型（Feedback）：**
| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| user_id | uint | 外键关联用户 |
| type | string | "bug" 或 "suggestion" |
| content | string | 反馈内容，max 1000 |
| created_at | timestamp | 创建时间 |
| updated_at | timestamp | 更新时间 |

**新增文件：**
- `server/model/feedback.go` — GORM 模型 + 自动迁移
- `server/handler/feedback.go` — HTTP handler
- `server/service/feedback.go` — 业务逻辑

### 6. 前端新增文件

- `studio/src/components/layout/FeedbackFab.tsx` — FAB + 展开面板完整组件
- `studio/public/wechat-qr.png` — 企微二维码静态图片（需用户提供）
- `studio/src/lib/api/feedback.ts` — 反馈 API 调用函数

### 7. 集成点

- `AppLayout.tsx` 中引入 `<FeedbackFab />`，放在 `<main>` 同级

## 文件变更清单

| 操作 | 文件 |
|------|------|
| 新增 | `studio/src/components/layout/FeedbackFab.tsx` |
| 新增 | `studio/src/lib/api/feedback.ts` |
| 新增 | `studio/public/wechat-qr.png`（用户提供） |
| 新增 | `server/model/feedback.go` |
| 新增 | `server/handler/feedback.go` |
| 新增 | `server/service/feedback.go` |
| 修改 | `studio/src/components/layout/AppLayout.tsx`（引入 FeedbackFab） |
| 修改 | `server/main.go`（注册路由 + 自动迁移） |

## 验证方式

1. 启动 dev server（`make server-dev` + `make web-dev`）
2. 登录后确认右下角出现 FAB 按钮
3. 点击 FAB → 面板展开，两个 tab 切换正常
4. 「联系客服」tab 展示二维码图片
5. 「意见反馈」tab 提交表单 → 检查数据库 feedback 表有新记录
6. 未登录状态下不显示 FAB
7. 点击面板外部区域关闭面板
