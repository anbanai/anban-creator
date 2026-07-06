<template>
  <view class="page tasks-page">
    <!-- Filter bar: status tabs + search + project filter + bulk toggle -->
    <view class="tasks-page__tabs">
      <AbTabs v-model="activeStatus" :tabs="statusTabs" />
    </view>

    <view class="tasks-page__filters">
      <view class="tasks-page__search">
        <AbInput
          v-model="searchKeyword"
          placeholder="搜索任务标题或要求..."
          type="text"
        />
      </view>
      <view class="tasks-page__project" @tap="pickProject">
        <text v-if="activeProjectName" class="tasks-page__project-value">
          {{ activeProjectName }}
        </text>
        <text v-else class="tasks-page__project-placeholder">全部账号</text>
        <text class="tasks-page__project-arrow">›</text>
      </view>
      <view
        class="tasks-page__bulk-toggle"
        :class="{ 'tasks-page__bulk-toggle--active': bulkMode }"
        @tap="toggleBulkMode"
      >
        <text>{{ bulkMode ? '退出选择' : '批量' }}</text>
      </view>
    </view>

    <view v-if="planId" class="tasks-page__context">
      <text class="tasks-page__context-label">当前仅显示此计划生成的任务</text>
      <text class="tasks-page__context-clear" @tap="clearPlanFilter">查看全部任务</text>
    </view>

    <!-- Bulk action bar (only in bulk mode) -->
    <view v-if="bulkMode" class="tasks-page__bulkbar">
      <view class="tasks-page__bulkbar-info">
        <text class="tasks-page__bulkbar-select" @tap="selectAllCompleted">
          {{ allCompletedSelected ? '取消全选已完成' : '全选已完成' }}
        </text>
        <text class="tasks-page__bulkbar-count">
          已选 {{ selectedIds.length }} 个，{{ selectedCompletedTasks.length }} 个可下载
        </text>
      </view>
      <view class="tasks-page__bulkbar-actions">
        <text
          v-if="selectedIds.length > 0"
          class="tasks-page__bulkbar-clear"
          @tap="clearSelection"
        >
          清空
        </text>
        <AbButton
          size="sm"
          type="primary"
          :loading="bulkDownloading"
          :disabled="selectedCompletedTasks.length === 0"
          @click="bulkDownload"
        >
          下载选中
        </AbButton>
      </view>
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
      <view
        v-for="task in filteredItems"
        :key="task.id"
        class="tasks-page__item-wrapper"
      >
        <!-- Bulk selection checkbox (only in bulk mode) -->
        <view
          v-if="bulkMode"
          class="tasks-page__checkbox"
          :class="{ 'tasks-page__checkbox--checked': isSelected(task.id) }"
          @tap.stop="toggleSelect(task.id)"
        >
          <text v-if="isSelected(task.id)" class="tasks-page__checkbox-mark">✓</text>
        </view>

        <TaskCard
          :task="task"
          :thumbnail-urls="getThumbnailUrls(task)"
          @tap="onTaskTap(task)"
        />

        <!-- Per-task quick actions shortcut -->
        <text
          v-if="!bulkMode"
          class="tasks-page__item-actions"
          @tap.stop="openTaskActions(task)"
        >
          ⋯
        </text>
      </view>
    </view>

    <!-- Empty state -->
    <AbEmpty
      v-if="!refreshing && filteredItems.length === 0"
      :title="emptyTitle"
      :description="emptyDescription"
      :action-text="emptyActionText"
      @action="goToCreate"
    />

    <!-- Load more -->
    <view v-if="loading" class="tasks-page__loading">
      <AbLoading size="sm" text="加载中" />
    </view>
    <text v-if="!hasMore && filteredItems.length > 0" class="tasks-page__end">
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
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { onLoad, onShow, onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import type { Task, TaskFile, Project } from '@/types'
import { tasksApi } from '@/api/tasks'
import { projectsApi } from '@/api/projects'
import { POLL_INTERVAL_RUNNING } from '@/utils/constants'
import TaskCard from '@/components/business/TaskCard.vue'
import AbTabs from '@/components/common/AbTabs.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'

// Studio-parity status set: all / pending / running / completed / failed / cancelled.
const statusTabs = [
  { key: '', label: '全部' },
  { key: 'pending', label: '待执行' },
  { key: 'running', label: '运行中' },
  { key: 'completed', label: '已完成' },
  { key: 'failed', label: '失败' },
  { key: 'cancelled', label: '已取消' },
]

const activeStatus = ref('')
const activeProjectId = ref('')
const activeProjectName = ref('')
const planId = ref('')
const searchKeyword = ref('')
const items = ref<Task[]>([])
const loading = ref(false)
const refreshing = ref(false)
const hasMore = ref(true)
const offset = ref(0)
const pageSize = 20
const PLAN_FILTER_KEY = 'anban_task_plan_filter'

// Bulk selection state
const bulkMode = ref(false)
const selectedIds = ref<string[]>([])
const bulkDownloading = ref(false)

// Project list for the project filter action sheet
const projects = ref<Project[]>([])
let pollingTimer: ReturnType<typeof setInterval> | null = null

const filteredItems = computed(() => {
  const q = searchKeyword.value.trim().toLowerCase()
  if (!q) return items.value
  return items.value.filter(
    (t) =>
      (t.title || '').toLowerCase().includes(q) ||
      (t.prompt || '').toLowerCase().includes(q),
  )
})

const selectedIdSet = computed(() => new Set(selectedIds.value))
const selectedCompletedTasks = computed(() =>
  filteredItems.value.filter(
    (t) => t.status === 'completed' && selectedIdSet.value.has(t.id),
  ),
)
const completedOnPage = computed(() =>
  filteredItems.value.filter((t) => t.status === 'completed'),
)
const allCompletedSelected = computed(
  () =>
    completedOnPage.value.length > 0 &&
    completedOnPage.value.every((t) => selectedIdSet.value.has(t.id)),
)

const emptyTitle = computed(() => {
  if (searchKeyword.value.trim()) return '没有匹配的任务'
  if (activeProjectId.value) return '该账号下没有任务'
  const map: Record<string, string> = {
    pending: '没有待执行的任务',
    running: '没有执行中的任务',
    completed: '没有已完成的任务',
    failed: '没有失败的任务',
    cancelled: '没有已取消的任务',
  }
  return map[activeStatus.value] || '还没有任务'
})

const emptyDescription = computed(() => {
  if (searchKeyword.value.trim() || activeProjectId.value) {
    return '尝试其他筛选条件或创建新任务'
  }
  if (activeStatus.value) return ''
  return '创建第一个任务开始创作吧'
})

const emptyActionText = computed(() => {
  if (searchKeyword.value.trim() || activeProjectId.value || activeStatus.value) {
    return ''
  }
  return '新建任务'
})

function getThumbnailUrls(task: Task): string[] {
  if (!task.result?.files) return []
  return task.result.files
    .filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
    .map((f: TaskFile) => f.url)
}

function isSelected(id: string): boolean {
  return selectedIdSet.value.has(id)
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
      project_id: activeProjectId.value || undefined,
      plan_id: planId.value || undefined,
      limit: pageSize,
      offset: offset.value,
    })

    const rawItems = res.items || []
    const newItems = rawItems.filter(matchesPlanFilter)
    if (reset) {
      items.value = newItems
    } else {
      items.value = [...items.value, ...newItems]
    }

    offset.value += rawItems.length
    hasMore.value = rawItems.length >= pageSize
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
    const hasRunning = items.value.some(
      (t) => t.status === 'running' || t.status === 'pending',
    )
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

async function loadProjects() {
  try {
    projects.value = await projectsApi.list({ status: 'active' })
  } catch (err) {
    console.error('Failed to load projects:', err)
    projects.value = []
  }
}

function pickProject() {
  const labels = ['全部账号', ...projects.value.map((p) => p.name)]
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      if (res.tapIndex === 0) {
        activeProjectId.value = ''
        activeProjectName.value = ''
      } else {
        const p = projects.value[res.tapIndex - 1]
        if (p) {
          activeProjectId.value = p.id
          activeProjectName.value = p.name
        }
      }
      fetchTasks(true)
    },
  })
}

// --- Bulk selection ---
function toggleBulkMode() {
  bulkMode.value = !bulkMode.value
  if (!bulkMode.value) {
    selectedIds.value = []
  }
}

function onTaskTap(task: Task) {
  if (bulkMode.value) {
    toggleSelect(task.id)
    return
  }
  goToDetail(task.id)
}

function toggleSelect(id: string) {
  if (selectedIdSet.value.has(id)) {
    selectedIds.value = selectedIds.value.filter((x) => x !== id)
  } else {
    selectedIds.value = [...selectedIds.value, id]
  }
}

function selectAllCompleted() {
  if (allCompletedSelected.value) {
    const completedIds = new Set(completedOnPage.value.map((t) => t.id))
    selectedIds.value = selectedIds.value.filter((id) => !completedIds.has(id))
  } else {
    const completedIds = completedOnPage.value.map((t) => t.id)
    selectedIds.value = Array.from(new Set([...selectedIds.value, ...completedIds]))
  }
}

function clearSelection() {
  selectedIds.value = []
}

// Keep selection in sync when the list refreshes: drop ids no longer visible.
watch(items, (next) => {
  const visible = new Set(next.map((t) => t.id))
  const pruned = selectedIds.value.filter((id) => visible.has(id))
  if (pruned.length !== selectedIds.value.length) {
    selectedIds.value = pruned
  }
})

async function bulkDownload() {
  const targets = selectedCompletedTasks.value
  if (targets.length === 0) {
    uni.showToast({ title: '请选择已完成的任务', icon: 'none' })
    return
  }
  bulkDownloading.value = true
  uni.showLoading({ title: `下载中 0/${targets.length}` })
  let ok = 0
  for (let i = 0; i < targets.length; i++) {
    try {
      await tasksApi.downloadTaskZip(targets[i].id)
      ok++
      uni.showLoading({ title: `下载中 ${i + 1}/${targets.length}` })
    } catch (err) {
      console.error('Bulk download task failed:', targets[i].id, err)
    }
  }
  uni.hideLoading()
  if (ok === 0) {
    uni.showToast({ title: '下载失败', icon: 'none' })
  } else if (ok < targets.length) {
    uni.showToast({ title: `已下载 ${ok}/${targets.length}`, icon: 'none' })
  } else {
    uni.showToast({ title: `已下载 ${ok} 个任务`, icon: 'success' })
  }
  bulkDownloading.value = false
}

// --- Per-task quick actions ---
function openTaskActions(task: Task) {
  const actions: string[] = ['查看详情']
  if (task.status === 'completed') {
    actions.push('下载 ZIP')
    actions.push(task.published ? '取消发布标记' : '标记发布')
  }
  if (task.status === 'running' || task.status === 'pending') {
    actions.push('取消任务')
  }
  if (task.status === 'failed' || task.status === 'cancelled') {
    actions.push('重试任务')
  }
  actions.push('删除任务')

  uni.showActionSheet({
    itemList: actions,
    success: (res) => {
      const label = actions[res.tapIndex]
      handleTaskAction(label, task)
    },
  })
}

function handleTaskAction(label: string, task: Task) {
  if (label === '查看详情') {
    goToDetail(task.id)
  } else if (label === '下载 ZIP') {
    downloadSingleZip(task.id)
  } else if (label === '标记发布' || label === '取消发布标记') {
    togglePublished(task)
  } else if (label === '取消任务') {
    cancelTask(task.id)
  } else if (label === '重试任务') {
    retryTask(task.id)
  } else if (label === '删除任务') {
    deleteTask(task.id)
  }
}

async function downloadSingleZip(taskId: string) {
  uni.showLoading({ title: '下载中' })
  try {
    await tasksApi.downloadTaskZip(taskId)
    uni.hideLoading()
    uni.showToast({ title: '已保存', icon: 'success' })
  } catch (err: any) {
    uni.hideLoading()
    uni.showToast({ title: err?.message || '下载失败', icon: 'none' })
  }
}

async function togglePublished(task: Task) {
  const next = !task.published
  try {
    await tasksApi.markPublished(task.id, next)
    // Optimistically update local list; polling will confirm.
    const idx = items.value.findIndex((t) => t.id === task.id)
    if (idx >= 0) {
      items.value[idx] = { ...items.value[idx], published: next }
    }
    uni.showToast({ title: next ? '已标记发布' : '已取消发布', icon: 'success' })
  } catch (err: any) {
    uni.showToast({ title: err?.message || '更新发布状态失败', icon: 'none' })
  }
}

function cancelTask(taskId: string) {
  uni.showModal({
    title: '取消任务',
    content: '确定要取消这个任务吗？',
    success: async (res) => {
      if (!res.confirm) return
      try {
        await tasksApi.cancel(taskId)
        const idx = items.value.findIndex((t) => t.id === taskId)
        if (idx >= 0) {
          items.value[idx] = { ...items.value[idx], status: 'cancelled' }
        }
        uni.showToast({ title: '任务已取消', icon: 'success' })
      } catch (err: any) {
        uni.showToast({ title: err?.message || '取消失败', icon: 'none' })
      }
    },
  })
}

function retryTask(taskId: string) {
  uni.showModal({
    title: '重试任务',
    content: '将创建一个新的计费任务并复用原配置，继续？',
    success: async (res) => {
      if (!res.confirm) return
      try {
        const newTask = await tasksApi.retry(taskId)
        uni.showToast({ title: '已创建重试任务', icon: 'success' })
        setTimeout(() => {
          uni.navigateTo({ url: `/pages/tasks/detail?id=${newTask.id}` })
        }, 500)
      } catch (err: any) {
        uni.showToast({ title: err?.message || '重试失败', icon: 'none' })
      }
    },
  })
}

function deleteTask(taskId: string) {
  uni.showModal({
    title: '删除任务',
    content: '删除后无法恢复，确定删除？',
    confirmColor: '#dc2626',
    success: async (res) => {
      if (!res.confirm) return
      try {
        await tasksApi.delete(taskId)
        items.value = items.value.filter((t) => t.id !== taskId)
        uni.showToast({ title: '已删除', icon: 'success' })
      } catch (err: any) {
        uni.showToast({ title: err?.message || '删除失败', icon: 'none' })
      }
    },
  })
}

function goToDetail(id: string) {
  uni.navigateTo({ url: `/pages/tasks/detail?id=${id}` })
}

function goToCreate() {
  uni.navigateTo({ url: '/pages/tasks/create' })
}

function clearPlanFilter() {
  planId.value = ''
  uni.removeStorageSync(PLAN_FILTER_KEY)
  fetchTasks(true)
}

function applyPlanFilter(nextPlanId: string) {
  if (!nextPlanId) return
  planId.value = nextPlanId
  fetchTasks(true)
}

function matchesPlanFilter(task: Task): boolean {
  if (!planId.value) return true
  return String(task.plan_id ?? '') === planId.value
}

// Watch status filter
watch(activeStatus, () => {
  fetchTasks(true)
})

onMounted(() => {
  fetchTasks(true)
  loadProjects()
  startPolling()
})

onLoad((query) => {
  if (query?.plan_id) {
    planId.value = String(query.plan_id)
  }
})

onShow(() => {
  const storedPlanId = uni.getStorageSync(PLAN_FILTER_KEY)
  if (storedPlanId) {
    uni.removeStorageSync(PLAN_FILTER_KEY)
    applyPlanFilter(String(storedPlanId))
  }
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

  &__filters {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    padding: $ab-space-sm $ab-space-md;
    background-color: $ab-surface;
    border-bottom: 2rpx solid $ab-border;
    position: sticky;
    top: 88rpx;
    z-index: 9;
  }

  &__search {
    flex: 1;
    min-width: 0;
  }

  &__project {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    padding: $ab-space-sm $ab-space-md;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    background-color: $ab-surface;
    max-width: 220rpx;

    &-value {
      font-size: $ab-text-sm;
      color: $ab-text;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    &-placeholder {
      font-size: $ab-text-sm;
      color: $ab-text-tertiary;
    }

    &-arrow {
      font-size: $ab-text-lg;
      color: $ab-text-tertiary;
      margin-left: $ab-space-xs;
    }
  }

  &__bulk-toggle {
    flex-shrink: 0;
    padding: $ab-space-sm $ab-space-md;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    background-color: $ab-surface;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;

    &--active {
      border-color: $ab-primary;
      color: $ab-primary;
      background-color: $ab-primary-bg;
    }
  }

  &__context {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: $ab-space-sm;
    padding: $ab-space-sm $ab-space-md;
    background: linear-gradient(90deg, $ab-primary-bg, $ab-surface);
    border-bottom: 2rpx solid $ab-border;
  }

  &__context-label {
    font-size: $ab-text-xs;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }

  &__context-clear {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    flex-shrink: 0;
  }

  &__bulkbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: $ab-space-sm;
    padding: $ab-space-sm $ab-space-md;
    background-color: $ab-primary-bg;
    border-bottom: 2rpx solid $ab-border;
    position: sticky;
    top: 168rpx;
    z-index: 8;

    &-info {
      flex: 1;
      min-width: 0;
      display: flex;
      flex-direction: column;
      gap: 2rpx;
    }

    &-select {
      font-size: $ab-text-sm;
      color: $ab-primary;
      font-weight: $ab-font-medium;
    }

    &-count {
      font-size: $ab-text-xs;
      color: $ab-text-secondary;
    }

    &-actions {
      display: flex;
      align-items: center;
      gap: $ab-space-sm;
      flex-shrink: 0;
    }

    &-clear {
      font-size: $ab-text-xs;
      color: $ab-text-secondary;
      padding: $ab-space-xs $ab-space-sm;
    }
  }

  &__list {
    padding: $ab-space-md;
  }

  &__skeleton {
    padding: $ab-space-md;
  }

  &__item-wrapper {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__checkbox {
    flex-shrink: 0;
    width: 44rpx;
    height: 44rpx;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    margin-top: $ab-space-sm;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: $ab-surface;

    &--checked {
      border-color: $ab-primary;
      background-color: $ab-primary;
    }

    &-mark {
      color: #fff;
      font-size: $ab-text-sm;
      font-weight: $ab-font-medium;
    }
  }

  // Make TaskCard take the remaining width inside the wrapper
  &__item-wrapper :deep(.task-card) {
    flex: 1;
    min-width: 0;
    margin-bottom: 0;
  }

  &__item-actions {
    position: absolute;
    top: $ab-space-sm;
    right: $ab-space-sm;
    width: 56rpx;
    height: 56rpx;
    line-height: 56rpx;
    text-align: center;
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
    z-index: 2;
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
