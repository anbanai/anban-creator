<template>
  <view class="page tasks-page">
    <!-- Status filter tabs -->
    <view class="tasks-page__tabs">
      <AbTabs v-model="activeStatus" :tabs="statusTabs" />
    </view>

    <!-- Loading skeleton -->
    <view v-if="refreshing && items.length === 0" class="tasks-page__skeleton">
      <view v-for="i in 3" :key="i" class="skeleton-card">
        <view class="skeleton-card__line skeleton-card__line--title" style="width: 60%;" />
        <view class="skeleton-card__line skeleton-card__line--sub" style="width: 80%;" />
        <view class="skeleton-card__bar" />
      </view>
    </view>

    <!-- Task list -->
    <view v-else class="tasks-page__list">
      <TaskCard
        v-for="task in items"
        :key="task.id"
        :task="task"
        :thumbnail-urls="getThumbnailUrls(task)"
        @tap="goToDetail(task.id)"
      />

      <!-- Retry button for failed tasks shown inline -->
    </view>

    <!-- Empty state -->
    <AbEmpty
      v-if="!refreshing && items.length === 0"
      :title="emptyTitle"
      :description="emptyDescription"
      action-text="新建任务"
      @action="goToCreate"
    />

    <!-- Load more -->
    <view v-if="loading" class="tasks-page__loading">
      <AbLoading size="sm" text="加载中" />
    </view>
    <text v-if="!hasMore && items.length > 0" class="tasks-page__end">
      — 已经到底了 —
    </text>

    <!-- Fixed bottom button -->
    <view class="tasks-page__fab">
      <AbButton type="primary" block size="lg" @click="goToCreate">
        + 新建任务
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import type { Task, TaskFile } from '@/types'
import { tasksApi } from '@/api/tasks'
import { POLL_INTERVAL_RUNNING } from '@/utils/constants'
import TaskCard from '@/components/business/TaskCard.vue'
import AbTabs from '@/components/common/AbTabs.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'

const statusTabs = [
  { key: '', label: '全部' },
  { key: 'running', label: '执行中' },
  { key: 'completed', label: '已完成' },
  { key: 'failed', label: '失败' },
]

const activeStatus = ref('')
const items = ref<Task[]>([])
const loading = ref(false)
const refreshing = ref(false)
const hasMore = ref(true)
const offset = ref(0)
const pageSize = 20
let pollingTimer: ReturnType<typeof setInterval> | null = null

const emptyTitle = computed(() => {
  const map: Record<string, string> = {
    running: '没有执行中的任务',
    completed: '没有已完成的任务',
    failed: '没有失败的任务',
  }
  return map[activeStatus.value] || '还没有任务'
})

const emptyDescription = computed(() => {
  if (activeStatus.value) return ''
  return '创建第一个任务开始创作吧'
})

function getThumbnailUrls(task: Task): string[] {
  if (!task.result?.files) return []
  return task.result.files
    .filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
    .map((f: TaskFile) => f.url)
}

async function fetchTasks(reset = false) {
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
    const res = await tasksApi.list({
      status: activeStatus.value || undefined,
      limit: pageSize,
      offset: offset.value,
    })

    const newItems = res.items || []
    if (reset) {
      items.value = newItems
    } else {
      items.value = [...items.value, ...newItems]
    }

    offset.value += newItems.length
    hasMore.value = newItems.length >= pageSize
  } catch (err) {
    console.error('Failed to load tasks:', err)
    if (reset) items.value = []
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

// Auto-poll for running tasks
function startPolling() {
  stopPolling()
  pollingTimer = setInterval(async () => {
    // Only poll if there are running tasks visible
    const hasRunning = items.value.some((t) => t.status === 'running' || t.status === 'pending')
    if (!hasRunning) return

    try {
      const res = await tasksApi.list({
        status: 'running,pending',
        limit: 50,
      })

      const runningTasks = res.items || []
      if (runningTasks.length === 0) return

      // Merge updated tasks into the list
      const runningMap = new Map(runningTasks.map((t) => [t.id, t]))
      items.value = items.value.map((t) => {
        const updated = runningMap.get(t.id)
        if (updated) return updated
        return t
      })
    } catch (err) {
      // Polling errors are non-critical
      console.error('Task polling error:', err)
    }
  }, POLL_INTERVAL_RUNNING)
}

function stopPolling() {
  if (pollingTimer) {
    clearInterval(pollingTimer)
    pollingTimer = null
  }
}

function goToDetail(id: string) {
  uni.navigateTo({ url: `/pages/tasks/detail?id=${id}` })
}

function goToCreate() {
  uni.navigateTo({ url: '/pages/tasks/create' })
}

// Watch status filter
import { watch } from 'vue'
watch(activeStatus, () => {
  fetchTasks(true)
})

onMounted(() => {
  fetchTasks(true)
  startPolling()
})

onUnmounted(() => {
  stopPolling()
})

onPullDownRefresh(async () => {
  await fetchTasks(true)
  uni.stopPullDownRefresh()
})

onReachBottom(() => {
  if (!loading.value && hasMore.value) {
    fetchTasks(false)
  }
})
</script>

<style lang="scss" scoped>
.tasks-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding-bottom: 160rpx;

  &__tabs {
    background-color: $ab-surface;
    position: sticky;
    top: 0;
    z-index: 10;
  }

  &__list {
    padding: $ab-space-md;
  }

  &__skeleton {
    padding: $ab-space-md;
  }

  &__loading {
    display: flex;
    justify-content: center;
    padding: $ab-space-lg 0;
  }

  &__end {
    display: block;
    text-align: center;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    padding: $ab-space-md 0;
  }

  &__fab {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    padding: $ab-space-md $ab-space-lg;
    padding-bottom: calc(#{$ab-space-md} + env(safe-area-inset-bottom));
    background-color: $ab-surface;
    box-shadow: 0 -4rpx 12rpx rgba(0, 0, 0, 0.06);
    z-index: 20;
  }
}

.skeleton-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;

  &__line {
    height: 28rpx;
    background-color: $ab-divider;
    border-radius: 8rpx;
    margin-bottom: $ab-space-xs;

    &--title { width: 60%; }
    &--sub { width: 80%; height: 22rpx; }
  }

  &__bar {
    height: 12rpx;
    background-color: $ab-divider;
    border-radius: $ab-radius-full;
    margin-top: $ab-space-sm;
    width: 70%;
  }
}
</style>
