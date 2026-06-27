<template>
  <view class="plans-page">
    <!-- Search + filter bar -->
    <view class="filter-bar">
      <view class="filter-bar__search" @tap="onSearchTap">
        <text class="filter-bar__search-icon">🔍</text>
        <input
          class="filter-bar__search-input"
          v-model="searchFilter"
          placeholder="搜索计划..."
          placeholder-class="filter-bar__search-placeholder"
          confirm-type="search"
        />
        <text
          v-if="searchFilter"
          class="filter-bar__search-clear"
          @tap.stop="searchFilter = ''"
        >×</text>
      </view>
      <view class="filter-bar__project" @tap="pickProjectFilter">
        <text class="filter-bar__project-value" v-if="projectFilterName">{{ projectFilterName }}</text>
        <text class="filter-bar__project-placeholder" v-else>全部账号</text>
        <text class="filter-bar__project-arrow">›</text>
      </view>
    </view>

    <!-- Status tabs -->
    <view class="status-tabs">
      <view
        v-for="tab in statusTabs"
        :key="tab.value"
        :class="['status-tabs__item', { active: statusFilter === tab.value }]"
        @tap="statusFilter = tab.value"
      >
        <text class="status-tabs__label">{{ tab.label }}</text>
      </view>
    </view>

    <!-- Loading skeleton -->
    <view v-if="loading && plans.length === 0" class="plans-page__skeleton">
      <view v-for="i in 3" :key="i" class="skel-card">
        <view class="skel-card__row">
          <AbSkeleton width="72rpx" height="72rpx" circle />
          <view class="skel-card__meta">
            <AbSkeleton width="55%" height="30rpx" />
            <AbSkeleton width="80%" height="24rpx" />
          </view>
        </view>
        <AbSkeleton class="skel-card__line" height="24rpx" />
        <AbSkeleton width="60%" height="24rpx" />
      </view>
    </view>

    <!-- Error bar -->
    <view v-if="errorMsg && !loading" class="error-bar">
      <text class="error-bar__text">{{ errorMsg }}</text>
      <text class="error-bar__retry" @tap="loadPlans">重试</text>
    </view>

    <!-- Plan list -->
    <view v-if="filteredPlans.length > 0" class="plans-list">
      <PlanCard
        v-for="plan in filteredPlans"
        :key="plan.id"
        :plan="plan"
        :project="projectMap[plan.project_id]"
        @tap="onPlanTap(plan)"
        @pause="onPause(plan)"
        @resume="onResume(plan)"
        @edit="onEdit(plan)"
        @delete="onDelete(plan)"
      />
    </view>

    <!-- Empty: no plans at all -->
    <AbEmpty
      v-if="!loading && plans.length === 0 && !errorMsg"
      title="还没有计划"
      description="创建定时计划，自动执行创作任务"
      action-text="新建计划"
      @action="goCreate"
    />

    <!-- Empty: filter yielded nothing -->
    <AbEmpty
      v-if="!loading && plans.length > 0 && filteredPlans.length === 0"
      title="未找到匹配的计划"
      description="尝试调整搜索或筛选条件"
    />

    <!-- No more indicator -->
    <view v-if="!hasMore && plans.length > 0 && filteredPlans.length > 0" class="no-more">
      <text class="no-more__text">-- 已经到底了 --</text>
    </view>

    <!-- Load more spinner -->
    <view v-if="loading && plans.length > 0" class="load-more">
      <AbLoading size="sm" />
    </view>

    <!-- Fixed bottom button -->
    <view class="bottom-bar" v-if="plans.length > 0">
      <AbButton type="primary" block @click="goCreate">
        ＋ 新建计划
      </AbButton>
    </view>

    <!-- Bottom spacer for fixed bar -->
    <view class="bottom-spacer" />
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import { plansApi } from '@/api/plans'
import { projectsApi } from '@/api/projects'
import type { Plan, Project } from '@/types'
import PlanCard from '@/components/business/PlanCard.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbSkeleton from '@/components/common/AbSkeleton.vue'

const plans = ref<Plan[]>([])
const allProjects = ref<Project[]>([])
const loading = ref(false)
const hasMore = ref(true)
const offset = ref(0)
const pageSize = 20
const errorMsg = ref('')

// Filters (mirror studio: project filter + search + status tabs).
// Studio uses server-side project_id; here we filter client-side because the
// miniapp list page already loads all plans in pages and the project filter is
// primarily cosmetic on small screens.
const searchFilter = ref('')
const projectFilter = ref('')
const statusFilter = ref<'all' | 'active' | 'paused'>('all')

const statusTabs = [
  { label: '全部', value: 'all' as const },
  { label: '运行中', value: 'active' as const },
  { label: '已暂停', value: 'paused' as const },
]

// Project lookup map for card display (avatar/name).
const projectMap = computed<Record<string, Project>>(() => {
  const map: Record<string, Project> = {}
  for (const p of allProjects.value) map[p.id] = p
  return map
})

const projectFilterName = computed(() => {
  if (!projectFilter.value) return ''
  return projectMap.value[projectFilter.value]?.name || '全部账号'
})

// Client-side filtering on top of the paginated list. Matches studio's
// useMemo filter (search against prompt) plus status tab + project filter.
const filteredPlans = computed<Plan[]>(() => {
  let list = plans.value
  if (projectFilter.value) {
    list = list.filter((p) => p.project_id === projectFilter.value)
  }
  if (statusFilter.value !== 'all') {
    list = list.filter((p) => p.status === statusFilter.value)
  }
  const q = searchFilter.value.trim().toLowerCase()
  if (q) {
    list = list.filter((p) => (p.prompt || '').toLowerCase().includes(q))
  }
  return list
})

async function fetchPlans(reset = false) {
  if (reset) {
    offset.value = 0
    hasMore.value = true
  }

  if (!hasMore.value && !reset) return

  if (reset) {
    errorMsg.value = ''
    loading.value = true
  } else {
    loading.value = true
  }

  try {
    const res = await plansApi.list({ limit: pageSize, offset: offset.value })
    const newItems = res.items || []

    if (reset) {
      plans.value = newItems
    } else {
      plans.value = [...plans.value, ...newItems]
    }

    offset.value += newItems.length
    hasMore.value = newItems.length >= pageSize
  } catch (err) {
    console.error('Failed to load plans:', err)
    if (reset) {
      errorMsg.value = '加载失败'
      plans.value = []
    }
  } finally {
    loading.value = false
  }
}

// Load all projects (for filter + card display).
async function loadProjects() {
  try {
    allProjects.value = await projectsApi.list({ status: 'active' })
  } catch (err) {
    console.error('Failed to load projects:', err)
    allProjects.value = []
  }
}

async function loadPlans() {
  await fetchPlans(true)
}

function pickProjectFilter() {
  const items = ['全部账号', ...allProjects.value.map((p) => p.name)]
  uni.showActionSheet({
    itemList: items,
    success: (res) => {
      if (res.tapIndex === 0) {
        projectFilter.value = ''
      } else {
        const proj = allProjects.value[res.tapIndex - 1]
        if (proj) projectFilter.value = proj.id
      }
    },
  })
}

// Search input on small screens: focus hint — the input itself is editable.
function onSearchTap() {
  // no-op: input handles its own focus. Hook reserved for future behavior.
}

// Pull-down refresh
onPullDownRefresh(async () => {
  await Promise.all([fetchPlans(true), loadProjects()])
  uni.stopPullDownRefresh()
})

// Reach bottom for pagination
onReachBottom(async () => {
  if (loading.value || !hasMore.value) return
  await fetchPlans(false)
})

// --- Event handlers ---

function onPlanTap(plan: Plan) {
  uni.navigateTo({ url: `/pages/tasks/index?plan_id=${plan.id}` })
}

function onPause(plan: Plan) {
  uni.showModal({
    title: '暂停计划',
    content: `确定要暂停「${plan.prompt || plan.title || '未命名计划'}」吗？`,
    success: async (res) => {
      if (!res.confirm) return
      try {
        await plansApi.pause(plan.id)
        const target = plans.value.find((p) => p.id === plan.id)
        if (target) target.status = 'paused'
        uni.showToast({ title: '已暂停', icon: 'none' })
      } catch (err: any) {
        const msg = err?.message || '暂停失败'
        uni.showToast({ title: msg, icon: 'none' })
      }
    },
  })
}

async function onResume(plan: Plan) {
  try {
    await plansApi.resume(plan.id)
    const target = plans.value.find((p) => p.id === plan.id)
    if (target) target.status = 'active'
    uni.showToast({ title: '已恢复', icon: 'none' })
  } catch (err: any) {
    const msg = err?.message || '恢复失败'
    uni.showToast({ title: msg, icon: 'none' })
  }
}

function onEdit(plan: Plan) {
  uni.navigateTo({ url: `/pages/plans/create?id=${plan.id}` })
}

function onDelete(plan: Plan) {
  uni.showModal({
    title: '删除计划',
    content: `确定要删除「${plan.prompt || plan.title || '未命名计划'}」吗？此操作不可撤销。`,
    confirmColor: '#dc2626',
    success: async (res) => {
      if (!res.confirm) return
      try {
        await plansApi.delete(plan.id)
        plans.value = plans.value.filter((p) => p.id !== plan.id)
        uni.showToast({ title: '已删除', icon: 'none' })
      } catch (err: any) {
        const msg = err?.message || '删除失败'
        uni.showToast({ title: msg, icon: 'none' })
      }
    },
  })
}

function goCreate() {
  uni.navigateTo({ url: '/pages/plans/create' })
}

// Initial load
loadPlans()
loadProjects()
</script>

<style lang="scss" scoped>
.plans-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

// Filter bar
.filter-bar {
  display: flex;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-sm;

  &__search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    background-color: $ab-surface;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    padding: 0 $ab-space-md;
    min-height: 72rpx;
  }

  &__search-icon {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__search-input {
    flex: 1;
    font-size: $ab-text-sm;
    color: $ab-text;
    min-width: 0;
  }

  &__search-placeholder {
    color: $ab-text-tertiary;
    font-size: $ab-text-sm;
  }

  &__search-clear {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
    padding: 0 $ab-space-xs;
  }

  &__project {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    background-color: $ab-surface;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    padding: 0 $ab-space-md;
    min-height: 72rpx;
    max-width: 220rpx;
  }

  &__project-value {
    font-size: $ab-text-sm;
    color: $ab-text;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__project-placeholder {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__project-arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}

// Status tabs
.status-tabs {
  display: flex;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;
  padding-bottom: $ab-space-xs;

  &__item {
    padding: $ab-space-xs $ab-space-md;
    border-radius: $ab-radius-sm;

    &.active {
      background-color: $ab-primary-bg;
    }
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  .active &__label {
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}

.plans-list {
  padding-top: $ab-space-xs;
}

// Loading skeleton cards
.plans-page__skeleton {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
  padding-top: $ab-space-xs;
}

.skel-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__row {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
  }

  &__meta {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 12rpx;
  }

  &__line {
    margin-top: $ab-space-md;
  }
}

// Error bar
.error-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-warning-bg;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;
  margin-bottom: $ab-space-md;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-warning;
    flex: 1;
  }

  &__retry {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
    flex-shrink: 0;
    margin-left: $ab-space-md;
  }
}

// No more
.no-more {
  text-align: center;
  padding: $ab-space-lg 0;

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Load more
.load-more {
  display: flex;
  justify-content: center;
  padding: $ab-space-md 0;
}

// Bottom bar
.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-sm $ab-space-md;
  padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom, 0px));
  background-color: $ab-surface;
  box-shadow: $ab-shadow-md;
  z-index: 10;
}

// Bottom spacer for fixed bar
.bottom-spacer {
  height: 140rpx;
}
</style>
