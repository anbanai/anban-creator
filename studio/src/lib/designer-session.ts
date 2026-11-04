const key = (userId: string) => `designer_active_generation:${userId}`
const batchKey = (id: string) => `designer_batch:${id}`

export interface DesignerBatch {
  fingerprint: string
  operations: { id: string; state: 'pending' | 'admitted' | 'rejected' }[]
}

export function saveDesignerBatch(id: string, batch: DesignerBatch): void {
  sessionStorage.setItem(batchKey(id), JSON.stringify(batch))
}

export function loadDesignerBatch(id: string): DesignerBatch | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(batchKey(id)) ?? 'null')
    return typeof value?.fingerprint === 'string' && Array.isArray(value.operations) && value.operations.length >= 2 && value.operations.length <= 4 && value.operations.every((operation: DesignerBatch['operations'][number]) => typeof operation.id === 'string' && ['pending', 'admitted', 'rejected'].includes(operation.state)) ? value : null
  } catch { return null }
}

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
  try {
    const value = JSON.parse(sessionStorage.getItem(key(userId)) ?? 'null')
    if (typeof value?.generationId === 'string') sessionStorage.removeItem(batchKey(value.generationId))
  } catch { /* Invalid recovery data can be discarded. */ }
  sessionStorage.removeItem(key(userId))
}
