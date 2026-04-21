import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import { emptyPluginConfigSchema } from "openclaw/plugin-sdk";
import { registerWechatArticle } from "./commands/wechatarticle";
import { registerWechatXls } from "./commands/wechatxls";
import { registerRednote } from "./commands/rednote";
import { registerFlower } from "./commands/flower";
import { registerQualityHooks } from "./hooks/handler";

export default {
  id: "anbanwriter",
  name: "案板创作助手",
  description:
    "WeChat & Xiaohongshu AI content creation suite — writing, visual design, multi-platform publishing",
  configSchema: emptyPluginConfigSchema(),

  register(api: OpenClawPluginApi) {
    // Register agent workflow hooks.
    // Each agent registers an llm_input hook that detects trigger keywords
    // in the user's message and injects the corresponding agent prompt into
    // the LLM system message. This lets the LLM autonomously orchestrate
    // the full creation pipeline via MCP tools.
    //
    // A shared sentinel (AGENT_SENTINEL) ensures mutual exclusion — only
    // one agent can activate per message. Priority: wechatarticle > rednote
    // > wechatxls > flower (registration order determines priority).
    registerWechatArticle(api);
    registerWechatXls(api);
    registerRednote(api);
    registerFlower(api);

    // Register quality verification hooks
    registerQualityHooks(api);

    api.logger.info("anbanwriter plugin registered");
  },
};
