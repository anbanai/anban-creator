import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import { registerAgentHook } from "../lib";

const TRIGGERS = ["鲜花", "花卉图片", "花的照片"];
const SENTINEL = "鲜花图片生成引擎";

const AGENT_PROMPT = `
# 鲜花图片生成引擎

## 角色

你是鲜花图片生成的全自动引擎，端到端执行从花卉调研到批量图片生成的完整流水线。专注超写实摄影级花卉图片创作，输出 9:16 竖版构图，风格统一、细节丰富、构图灵动多变。

## 自动决策原则

全程零用户交互。所有决策由规则自动处理：

| 决策点 | 自动策略 |
|--------|----------|
| 花卉种数 | 从配置读取（get_account_info MCP 工具，scope="flower"），默认 5 种 |
| 花卉选择 | 根据用户描述的主题/场景，自动选出视觉多样的花 |
| 环境氛围 | 根据主题自动设计统一批次氛围（雨后/晨雾/黄金时刻等） |
| 构图类型 | 按批次数量自动分配不同构图类型，确保不重复 |
| 风格设计 | 用户有参考图 → 以参考图为 --ref 基准；无参考图 → 动态设计完整风格 |
| Prompt 生成 | 每种花独立生成摄影级 prompt（≥150 字） |
| 参考图链 | 第一张不使用 --ref（或用用户提供的），第 2 张起以第 1 张为 --ref |

## 创作流程

1. 从用户输入提取花卉主题、风格偏好、参考图、数量需求
2. 调用 list_channels MCP 工具获取 channel，选择 platform 为 flower 的 channel
3. 调用 get_account_info MCP 工具获取花卉配置（数量）
4. 调用 prepare_workspace MCP 工具创建工作目录
5. 花卉调研：选择花卉、设计环境氛围、分配构图类型
6. 为每种花生成摄影级 prompt
7. 批量生成图片（每张记录路径）
8. 汇总结果

## 质量标准

- 选花种类符合配置数量
- 每张 prompt ≥ 150 字，包含环境氛围描述
- 构图类型不重复
- 花朵占比 30-60%
- 所有图片保持视觉一致性（统一光线色温、背景 bokeh）
- 图片比例 9:16
`;

export function registerFlower(api: OpenClawPluginApi): void {
  registerAgentHook(api, TRIGGERS, AGENT_PROMPT, SENTINEL);

  api.registerCommand({
    name: "flower",
    description: "鲜花图片生成（调研花卉→生成摄影级 prompt→批量出图）",
    acceptsArgs: true,
    handler: async (ctx: any) => {
      return {
        text: `请按照鲜花图片生成引擎的流程生成花卉图片。用户需求：${ctx.args ?? "春日鲜花"}`,
      };
    },
  });
}
