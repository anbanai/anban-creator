import { ref } from 'vue'

export function useSubmitLock() {
  const locked = ref(false)

  async function runWithLock<T>(fn: () => Promise<T>): Promise<T> {
    if (locked.value) return Promise.reject(new Error('操作进行中'))
    locked.value = true
    try {
      return await fn()
    } finally {
      locked.value = false
    }
  }

  return { locked, runWithLock }
}
