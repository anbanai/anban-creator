const ASSISTANT_MESSAGE_HANDOFF_PREFIX = 'anban:assistant-message-handoff:'

export function assistantMessageHandoffKey(userId: string, modeId: string, projectId?: string, contentId?: string): string {
  return `${ASSISTANT_MESSAGE_HANDOFF_PREFIX}${userId}:${modeId}:${projectId ?? ''}:${contentId ?? ''}`
}

export function saveAssistantMessageHandoff(key: string, message: string): void {
  const value = message.trim()
  if (!value) {
    clearAssistantMessageHandoff(key)
    return
  }
  sessionStorage.setItem(key, JSON.stringify({ message: value }))
}

export function readAssistantMessageHandoff(key: string): string | undefined {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return undefined
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
    const message = (value as { message?: unknown }).message
    return typeof message === 'string' && message.trim() ? message : undefined
  } catch {
    return undefined
  }
}

export function clearAssistantMessageHandoff(key: string): void {
  sessionStorage.removeItem(key)
}
