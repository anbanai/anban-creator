const key = (userId: string) => `designer_active_generation:${userId}`

export function saveActiveGeneration(userId: string, generationId: string): void {
  sessionStorage.setItem(key(userId), JSON.stringify({ generationId, startedAt: Date.now() }))
}

export function loadActiveGeneration(userId: string): { generationId: string; startedAt: number } | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(key(userId)) ?? 'null')
    return typeof value?.generationId === 'string' ? value : null
  } catch {
    clearActiveGeneration(userId)
    return null
  }
}

export function clearActiveGeneration(userId: string): void {
  sessionStorage.removeItem(key(userId))
}
