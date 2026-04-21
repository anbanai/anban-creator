import type { OpenClawPluginApi } from "openclaw/plugin-sdk";

/**
 * Register quality verification hooks for anbanwriter content creation.
 *
 * Listens for after_tool_call events from anbanwriter MCP tools and
 * generates structured delivery summaries.
 */
export function registerQualityHooks(api: OpenClawPluginApi): void {
  api.registerHook("after_tool_call", async (event: any) => {
    const toolName = event?.tool_name ?? "";

    // Only process anbanwriter MCP tools
    const handled = handleToolComplete(toolName, event?.tool_input ?? {}, event?.tool_output);
    if (handled && api.logger) {
      api.logger.info(`[anbanwriter] quality check: ${toolName} completed`);
    }
  });
}

function handleToolComplete(
  toolName: string,
  input: Record<string, any>,
  output: any
): string | null {
  switch (toolName) {
    case "publish_draft":
    case "draft":
      return summarizePublish(input, output);
    case "archive_workspace":
      return summarizeArchive(input, output);
    default:
      return null;
  }
}

function summarizePublish(
  input: Record<string, any>,
  output: any
): string {
  const title = input?.title ?? "unknown";
  const draftUrl = output?.draft_url ?? output?.url;

  const lines = [
    `**草稿发布完成**`,
    `- 标题：${title}`,
    draftUrl ? `- 草稿链接：${draftUrl}` : "",
    `- 请在公众号后台检查草稿内容和格式`,
  ];

  return lines.filter(Boolean).join("\n");
}

function summarizeArchive(
  input: Record<string, any>,
  output: any
): string {
  const archivePath = output?.archive_path ?? output?.path ?? input?.name;
  const contentType = input?.content_type ?? "unknown";

  const lines = [
    `**工作目录归档完成**`,
    `- 类型：${contentType}`,
    `- 归档路径：${archivePath}`,
    `- 请检查产出文件完整性`,
  ];

  return lines.filter(Boolean).join("\n");
}
