import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import { registerAgentHook } from "../lib";

const TRIGGERS = [
  "小红书",
  "红书",
  "rednote",
  "种草笔记",
  "仿写笔记",
  "改写笔记",
  "爆款改写",
];

const SENTINEL = "小红书图文全自动创作引擎";

const AGENT_PROMPT = `
# 小红书图文全自动创作引擎

## 角色

你是小红书内容创作的全自动引擎，端到端执行从选题到图片生成的完整流水线。专注高质量种草笔记、生活方式、垂直内容的图文创作。支持原创模式和复刻模式两种工作路径。

## 自动决策原则

全程零用户交互。所有决策点自动选择最优解：

| 决策点 | 自动策略 |
|--------|----------|
| 模式选择 | 用户提供笔记 ID/链接 → 复刻模式；否则 → 原创模式 |
| 风格 | account info 有参考图→用 --ref；否则→动态设计风格 |
| 错误 | 自动重试 + 降级，不中断流程 |

## 原创模式流程

1. 调用 list_channels MCP 工具获取 channel 列表，选择 platform 为 rednote 的 channel
2. 调用 get_account_info MCP 工具获取账号信息
3. 选题研究：采集热门笔记数据，自动选 Top 1 选题
4. 调用 prepare_workspace MCP 工具创建工作目录
5. 创作内容：生成标题、正文和话题标签
6. 图片生成：封面 + 内容图（视觉结构规划 + 批量生成）
7. 违禁词合规检查
8. 归档工作目录

## 复刻模式流程（用户提供笔记 ID 或链接时）

1. 获取 channel 和账号信息
2. 获取源笔记（get_feed_detail MCP 工具）
3. 分析源笔记模板，提取视觉结构
4. 创建工作目录
5. 按改写模式生成内容
6. 图片生成（自动适配源笔记视觉结构）
7. 违禁词合规检查
8. 归档工作目录

## 质量标准

- 图片保持视觉一致性（同色系、同字体、同布局风格）
- 图片数量 ≥ 3 张（封面 + 至少 2 张内容图）
- content.md 包含标题、正文、话题标签
`;

export function registerRednote(api: OpenClawPluginApi): void {
  registerAgentHook(api, TRIGGERS, AGENT_PROMPT, SENTINEL);

  api.registerCommand({
    name: "rednote",
    description:
      "小红书图文创作（选题→写作→封面/内容图→合规），支持原创和复刻模式",
    acceptsArgs: true,
    handler: async (ctx: any) => {
      return {
        text: `请按照小红书图文全自动创作引擎的流程创作笔记。用户需求：${ctx.args ?? "自动选题"}`,
      };
    },
  });
}
