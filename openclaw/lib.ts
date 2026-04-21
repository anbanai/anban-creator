import type { OpenClawPluginApi } from "openclaw/plugin-sdk";

// Sentinel marker injected into system messages when an agent is activated.
// Used to prevent multiple agents from activating on the same message.
export const AGENT_SENTINEL = "\n<!-- anbanwriter:agent-active -->";

/**
 * Extract text content from the last user message, handling both
 * string and multi-part (array) content formats.
 */
export function extractUserText(messages?: Array<{ role: string; content: string | Array<{ type: string; text?: string }> }>): string {
  const lastUserMsg = messages
    ?.filter((m) => m.role === "user")
    .pop()?.content;

  if (typeof lastUserMsg === "string") return lastUserMsg;
  if (Array.isArray(lastUserMsg)) {
    return (
      lastUserMsg
        .filter((block) => block.type === "text")
        .map((block) => block.text ?? "")
        .join("")
    );
  }
  return "";
}

/**
 * Check if any agent has already been activated in the current conversation
 * (prevents multiple agents from activating simultaneously).
 */
export function isAgentActive(
  messages?: Array<{ role: string; content: string }>
): boolean {
  return messages?.some(
    (m) => m.role === "system" && m.content?.includes(AGENT_SENTINEL)
  ) ?? false;
}

/**
 * Register an llm_input hook with mutual exclusion and multi-part message support.
 *
 * @param triggers - Keywords that activate the agent
 * @param agentPrompt - The prompt to inject into the system message
 * @param sentinel - Unique string to detect self-double-injection
 */
export function registerAgentHook(
  api: OpenClawPluginApi,
  triggers: string[],
  agentPrompt: string,
  sentinel: string
): void {
  api.registerHook("llm_input", async (event: any) => {
    // Skip if another agent already activated
    if (isAgentActive(event.messages)) return;

    // Extract user text (handles string and multi-part content)
    const userMessage = extractUserText(event.messages);
    if (!userMessage) return;

    // Check triggers
    if (!triggers.some((t) => userMessage.includes(t))) return;

    // Inject agent prompt into system message
    const systemMsg = event.messages?.find((m: any) => m.role === "system");
    const promptWithSentinel = agentPrompt + "\n" + AGENT_SENTINEL;

    if (systemMsg) {
      systemMsg.content += "\n\n" + promptWithSentinel;
    } else {
      event.messages?.unshift({
        role: "system",
        content: promptWithSentinel,
      });
    }

    api.logger.info(`[anbanwriter] agent activated (matched: ${triggers.find((t) => userMessage.includes(t))})`);
  });
}
