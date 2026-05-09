import { ref, onMounted } from 'vue'
import type { PaginatedResponse } from '@/types'

interface UseListOptions<T> {
  fetchFn: (params: { limit: number; offset: number }) => Promise<PaginatedResponse<T>>
  pageSize?: number
  immediate?: boolean
}

export function useList<T>(options: UseListOptions<T>) {
  const {
    fetchFn,
    pageSize = 20,
    immediate = true,
  } = options

  const items = ref<T[]>([]) as { value: T[] }
  const total = ref(0)
  const loading = ref(false)
  const refreshing = ref(false)
  const hasMore = ref(true)
  const offset = ref(0)

  async function fetch(reset = false) {
    if (reset) {
      offset.value = 0
      hasMore.value = true
    }

    if (!hasMore.value && !reset) return

    if (reset) {
      refreshing.value = true
    } else {
      loading.value = true
    }

    try {
      const res = await fetchFn({ limit: pageSize, offset: offset.value })
      const newItems = res.items || []

      if (reset) {
        items.value = newItems
      } else {
        items.value = [...items.value, ...newItems]
      }

      total.value = res.total ?? items.value.length
      offset.value += newItems.length
      hasMore.value = newItems.length >= pageSize
    } catch (err) {
      console.error('useList fetch error:', err)
      if (reset) items.value = []
    } finally {
      loading.value = false
      refreshing.value = false
    }
  }

  async function refresh() {
    await fetch(true)
    uni.stopPullDownRefresh()
  }

  async function loadMore() {
    if (loading.value || !hasMore.value) return
    await fetch(false)
  }

  if (immediate) {
    onMounted(() => fetch(true))
  }

  return {
    items,
    total,
    loading,
    refreshing,
    hasMore,
    fetch,
    refresh,
    loadMore,
  }
}
