const STORAGE_KEY = 'designer_active_generation'

interface ActiveGeneration {
  generationId: string
  startedAt: number
}

const MAX_AGE = 10 * 60 * 1000 // 10 minutes (backend max ~5min + buffer)

export function saveActiveGeneration(generationId: string): void {
  sessionStorage.setItem(STORAGE_KEY, JSON.stringify({
    generationId,
    startedAt: Date.now(),
  }))
}

export function loadActiveGeneration(): ActiveGeneration | null {
  const raw = sessionStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    const data: ActiveGeneration = JSON.parse(raw)
    if (Date.now() - data.startedAt > MAX_AGE) {
      clearActiveGeneration()
      return null
    }
    return data
  } catch {
    clearActiveGeneration()
    return null
  }
}

export function clearActiveGeneration(): void {
  sessionStorage.removeItem(STORAGE_KEY)
}
