<template>
  <view class="page">
    <!-- Date Range Tabs -->
    <view class="date-tabs">
      <scroll-view class="date-tabs__scroll" scroll-x :show-scrollbar="false">
        <view class="date-tabs__nav">
          <view
            v-for="tab in dateRangeTabs"
            :key="tab.key"
            class="date-tabs__item"
            :class="{ 'date-tabs__item--active': activeDateRange === tab.key }"
            @tap="onDateRangeChange(tab.key)"
          >
            <text class="date-tabs__label">{{ tab.label }}</text>
          </view>
        </view>
      </scroll-view>
      <view class="date-tabs__filter" @tap="showFilter = true">
        <text class="date-tabs__filter-icon">&#x25BC;</text>
        <text class="date-tabs__filter-text">筛选</text>
      </view>
    </view>

    <!-- Content -->
    <view v-if="loading && !items.length" class="timeline-loading">
      <AbLoading text="加载中..." />
    </view>

    <view v-else-if="!items.length && !loading" class="timeline-empty">
      <AbEmpty title="暂无时间轴记录" description="选择的范围内没有任何活动" />
    </view>

    <scroll-view
      v-else
      class="timeline-scroll"
      scroll-y
      :refresher-enabled="true"
      :refresher-triggered="refreshing"
      @refresherrefresh="onPullRefresh"
    >
      <view class="timeline-list">
        <template v-for="(group, monthKey) in groupedItems" :key="monthKey">
          <!-- Month Header -->
          <view class="timeline-month-header">
            <text class="timeline-month-header__text">{{ group.label }}</text>
          </view>

          <template v-for="(dateGroup, dateKey) in group.dates" :key="dateKey">
            <!-- Date Header -->
            <view class="timeline-date-header">
              <text class="timeline-date-header__text">{{ dateGroup.label }}</text>
            </view>

            <!-- Timeline Items -->
            <view
              v-for="item in dateGroup.items"
              :key="item.id"
              class="timeline-item"
              :class="{ 'timeline-item--task': item.type === 'task' }"
              @tap="onItemTap(item)"
            >
              <!-- Timeline Line + Dot -->
              <view class="timeline-item__line">
                <view
                  class="timeline-item__dot"
                  :class="`timeline-item__dot--${getDotColor(item)}`"
                />
                <view class="timeline-item__connector" />
              </view>

              <!-- Content -->
              <view class="timeline-item__content">
                <view class="timeline-item__row">
                  <text class="timeline-item__time">{{ formatTimeCN(item.created_at) }}</text>
                  <AbBadge size="sm" variant="neutral">
                    {{ timelineItemTypeLabel[item.type] || item.type }}
                  </AbBadge>
                  <AbBadge size="sm" variant="info">
                    {{ contentTypeLabel[item.content_type] || item.content_type }}
                  </AbBadge>
                </view>

                <text class="timeline-item__title">{{ item.title || '未命名' }}</text>

                <view class="timeline-item__row">
                  <AbBadge size="sm" :variant="getBadgeVariant(item.status, item.type)">
                    {{ item.type === 'plan'
                      ? planStatusLabel[item.status as PlanStatus] || item.status
                      : taskStatusLabel[item.status as TaskStatus] || item.status
                    }}
                  </AbBadge>
                  <text
                    v-if="item.status === 'running' && item.progress != null"
                    class="timeline-item__progress"
                  >
                    {{ item.progress }}%
                  </text>
                </view>

                <!-- Error message -->
                <text v-if="item.status === 'failed' && item.error" class="timeline-item__error">
                  {{ item.error }}
                </text>

                <!-- Plan inline actions -->
                <view v-if="item.type === 'plan' && item.plan_id" class="timeline-item__actions">
                  <view
                    v-if="item.status === 'active'"
                    class="timeline-item__btn timeline-item__btn--pause"
                    @tap.stop="onPausePlan(item)"
                  >
                    <text class="timeline-item__btn-text">暂停</text>
                  </view>
                  <view
                    v-if="item.status === 'paused'"
                    class="timeline-item__btn timeline-item__btn--resume"
                    @tap.stop="onResumePlan(item)"
                  >
                    <text class="timeline-item__btn-text">恢复</text>
                  </view>
                </view>
              </view>
            </view>
          </template>
        </template>
      </view>
    </scroll-view>

    <!-- Filter Popup -->
    <view v-if="showFilter" class="filter-mask" @tap="showFilter = false" />
    <view class="filter-popup" :class="{ 'filter-popup--show': showFilter }">
      <view class="filter-popup__header">
        <text class="filter-popup__title">筛选</text>
        <text class="filter-popup__reset" @tap="onResetFilters">重置</text>
      </view>

      <scroll-view class="filter-popup__body" scroll-y>
        <!-- Type -->
        <view class="filter-section">
          <text class="filter-section__label">类型</text>
          <view class="filter-section__options">
            <view
              v-for="opt in typeOptions"
              :key="opt.key"
              class="filter-chip"
              :class="{ 'filter-chip--active': filter.type === opt.key }"
              @tap="filter.type = opt.key"
            >
              <text class="filter-chip__text">{{ opt.label }}</text>
            </view>
          </view>
        </view>

        <!-- Content Type -->
        <view class="filter-section">
          <text class="filter-section__label">内容类型</text>
          <view class="filter-section__options">
            <view
              v-for="opt in contentTypeOptions"
              :key="opt.key"
              class="filter-chip"
              :class="{ 'filter-chip--active': filter.content_type === opt.key }"
              @tap="filter.content_type = opt.key"
            >
              <text class="filter-chip__text">{{ opt.label }}</text>
            </view>
          </view>
        </view>

        <!-- Status -->
        <view class="filter-section">
          <text class="filter-section__label">状态</text>
          <view class="filter-section__options">
            <view
              v-for="opt in statusOptions"
              :key="opt.key"
              class="filter-chip"
              :class="{ 'filter-chip--active': filter.status === opt.key }"
              @tap="filter.status = opt.key"
            >
              <text class="filter-chip__text">{{ opt.label }}</text>
            </view>
          </view>
        </view>

        <!-- Channel -->
        <view class="filter-section">
          <text class="filter-section__label">账号</text>
          <picker
            :value="channelPickerIndex"
            :range="channelNames"
            range-key="name"
            @change="onChannelPick"
          >
            <view class="filter-select">
              <text class="filter-select__text">
                {{ selectedChannelName || '全部账号' }}
              </text>
              <text class="filter-select__arrow">></text>
            </view>
          </picker>
        </view>
      </scroll-view>

      <view class="filter-popup__footer">
        <view class="filter-popup__confirm" @tap="onApplyFilter">
          <text class="filter-popup__confirm-text">确认筛选</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { timelineApi } from '@/api/timeline'
import type { TimelineItem } from '@/types'
import type { TaskStatus, PlanStatus } from '@/types'
import {
  taskStatusLabel,
  planStatusLabel,
  timelineItemTypeLabel,
  contentTypeLabel,
  getBadgeVariant,
} from '@/utils/labels'
import { formatTimeCN, formatDateLabelCN, formatMonthCN, getMonthRange } from '@/utils/format'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbBadge from '@/components/common/AbBadge.vue'

// Date range tabs
const dateRangeTabs = [
  { key: '7d', label: '近7天' },
  { key: '30d', label: '近30天' },
  { key: '90d', label: '近90天' },
  { key: 'month', label: '本月' },
]
const activeDateRange = ref('7d')

function getDateRange() {
  const now = new Date()
  const to = formatDateYMD(now)
  let from: string
  switch (activeDateRange.value) {
    case '7d': {
      const d = new Date(now)
      d.setDate(d.getDate() - 6)
      from = formatDateYMD(d)
      break
    }
    case '30d': {
      const d = new Date(now)
      d.setDate(d.getDate() - 29)
      from = formatDateYMD(d)
      break
    }
    case '90d': {
      const d = new Date(now)
      d.setDate(d.getDate() - 89)
      from = formatDateYMD(d)
      break
    }
    case 'month': {
      const range = getMonthRange(now)
      return { from: range.from, to: range.to }
    }
    default: {
      const d = new Date(now)
      d.setDate(d.getDate() - 6)
      from = formatDateYMD(d)
    }
  }
  return { from, to }
}

function formatDateYMD(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// State
const items = ref<TimelineItem[]>([])
const loading = ref(false)
const refreshing = ref(false)

// Filters
const showFilter = ref(false)
const filter = reactive({
  type: '' as string,
  content_type: '' as string,
  status: '' as string,
  channel_id: '' as string,
})

const typeOptions = [
  { key: '', label: '全部' },
  { key: 'task', label: '任务' },
  { key: 'plan', label: '计划' },
]

const statusOptions = [
  { key: '', label: '全部' },
  { key: 'pending', label: '待执行' },
  { key: 'running', label: '运行中' },
  { key: 'completed', label: '已完成' },
  { key: 'failed', label: '失败' },
]

// Channels for filter picker (populated from timeline items)
const channels = ref<Array<{ id: string; name: string }>>([])
const channelPickerIndex = ref(0)
const selectedChannelName = ref('')

const channelNames = computed(() => {
  const all = [{ id: '', name: '全部账号' }, ...channels.value]
  return all
})

function onChannelPick(e: any) {
  const idx = e.detail.value as number
  const all = [{ id: '', name: '全部账号' }, ...channels.value]
  if (idx >= 0 && idx < all.length) {
    filter.channel_id = all[idx].id
    selectedChannelName.value = all[idx].name
  }
}

// Grouping
interface DateGroup {
  label: string
  dateStr: string
  items: TimelineItem[]
}

interface MonthGroup {
  label: string
  dates: Record<string, DateGroup>
}

const groupedItems = computed<Record<string, MonthGroup>>(() => {
  const result: Record<string, MonthGroup> = {}

  for (const item of items.value) {
    const dateStr = item.created_at?.slice(0, 10) || ''
    if (!dateStr) continue

    const monthLabel = formatMonthCN(dateStr)
    const dateLabel = formatDateLabelCN(dateStr)

    if (!result[monthLabel]) {
      result[monthLabel] = { label: monthLabel, dates: {} }
    }

    if (!result[monthLabel].dates[dateStr]) {
      result[monthLabel].dates[dateStr] = {
        label: dateLabel,
        dateStr,
        items: [],
      }
    }

    result[monthLabel].dates[dateStr].items.push(item)
  }

  // Sort items within each date group by time (desc)
  for (const month of Object.values(result)) {
    for (const dateGroup of Object.values(month.dates)) {
      dateGroup.items.sort((a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      )
    }
  }

  return result
})

// Fetch
async function fetchItems(showRefresh = false) {
  if (showRefresh) {
    refreshing.value = true
  } else {
    loading.value = true
  }

  try {
    const range = getDateRange()
    const params: Record<string, string> = {
      from: range.from,
      to: range.to,
    }
    if (filter.type) params.type = filter.type
    if (filter.content_type) params.content_type = filter.content_type
    if (filter.status) params.status = filter.status
    if (filter.channel_id) params.channel_id = filter.channel_id

    const res = await timelineApi.get(params as any)
    items.value = res.items || []

    // Extract unique channels from items
    const channelMap = new Map<string, string>()
    for (const item of items.value) {
      if (item.channel_id && item.channel_name && !channelMap.has(item.channel_id)) {
        channelMap.set(item.channel_id, item.channel_name)
      }
    }
    channels.value = Array.from(channelMap.entries()).map(([id, name]) => ({ id, name }))
  } catch (err: any) {
    uni.showToast({ title: err?.message || '加载失败', icon: 'none' })
    if (showRefresh) items.value = []
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

function onPullRefresh() {
  fetchItems(true)
}

function onDateRangeChange(key: string) {
  activeDateRange.value = key
  fetchItems()
}

// Filter actions
function onResetFilters() {
  filter.type = ''
  filter.content_type = ''
  filter.status = ''
  filter.channel_id = ''
  selectedChannelName.value = ''
  channelPickerIndex.value = 0
}

function onApplyFilter() {
  showFilter.value = false
  fetchItems()
}

// Item interactions
function getDotColor(item: TimelineItem): string {
  if (item.status === 'completed') return 'success'
  if (item.status === 'failed') return 'danger'
  if (item.status === 'running') return 'info'
  if (item.status === 'paused') return 'warning'
  return 'neutral'
}

function onItemTap(item: TimelineItem) {
  if (item.type === 'task' && item.task_id) {
    uni.navigateTo({ url: `/pages/tasks/detail?id=${item.task_id}` })
  }
}

async function onPausePlan(item: TimelineItem) {
  if (!item.plan_id) return
  try {
    const { post } = await import('@/api/index')
    await post(`/plans/${item.plan_id}/pause`)
    uni.showToast({ title: '已暂停', icon: 'success' })
    fetchItems()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '操作失败', icon: 'none' })
  }
}

async function onResumePlan(item: TimelineItem) {
  if (!item.plan_id) return
  try {
    const { post } = await import('@/api/index')
    await post(`/plans/${item.plan_id}/resume`)
    uni.showToast({ title: '已恢复', icon: 'success' })
    fetchItems()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '操作失败', icon: 'none' })
  }
}

onMounted(() => {
  fetchItems()
})
</script>

<style lang="scss" scoped>
.page {
  min-height: 100vh;
  background-color: $ab-background;
  display: flex;
  flex-direction: column;
}

// Date Range Tabs
.date-tabs {
  display: flex;
  align-items: center;
  background-color: $ab-surface;
  border-bottom: 2rpx solid $ab-border;
  padding: 0 $ab-space-sm;
  flex-shrink: 0;

  &__scroll {
    flex: 1;
    white-space: nowrap;
  }

  &__nav {
    display: inline-flex;
    align-items: center;
  }

  &__item {
    padding: $ab-space-sm $ab-space-md;
    flex-shrink: 0;
    border-radius: $ab-radius-full;
    transition: all 0.2s ease;

    &--active {
      background-color: $ab-primary-bg;

      .date-tabs__label {
        color: $ab-primary;
        font-weight: $ab-font-semibold;
      }
    }
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    white-space: nowrap;
  }

  &__filter {
    display: flex;
    align-items: center;
    padding: $ab-space-sm $ab-space-md;
    margin-left: $ab-space-xs;
    flex-shrink: 0;
  }

  &__filter-icon {
    font-size: 18rpx;
    color: $ab-text-secondary;
    margin-right: 4rpx;
  }

  &__filter-text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }
}

// Loading / Empty
.timeline-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 120rpx 0;
}

.timeline-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 120rpx 0;
}

// Timeline List
.timeline-scroll {
  flex: 1;
  height: 0;
}

.timeline-list {
  padding: $ab-space-md $ab-space-md $ab-space-xl;
}

// Month Header
.timeline-month-header {
  padding: $ab-space-sm 0;

  &__text {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }
}

// Date Header
.timeline-date-header {
  padding: $ab-space-xs 0;
  margin-left: 28rpx;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    font-weight: $ab-font-medium;
  }
}

// Timeline Item
.timeline-item {
  display: flex;
  position: relative;
  min-height: 100rpx;

  &__line {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 40rpx;
    flex-shrink: 0;
  }

  &__dot {
    width: 16rpx;
    height: 16rpx;
    border-radius: 50%;
    margin-top: 16rpx;
    flex-shrink: 0;

    &--success {
      background-color: $ab-success;
      box-shadow: 0 0 0 4rpx $ab-success-bg;
    }

    &--danger {
      background-color: $ab-danger;
      box-shadow: 0 0 0 4rpx $ab-danger-bg;
    }

    &--info {
      background-color: $ab-info;
      box-shadow: 0 0 0 4rpx $ab-info-bg;
      animation: dot-pulse 2s ease-in-out infinite;
    }

    &--warning {
      background-color: $ab-warning;
      box-shadow: 0 0 0 4rpx $ab-warning-bg;
    }

    &--neutral {
      background-color: $ab-text-tertiary;
      box-shadow: 0 0 0 4rpx $ab-divider;
    }
  }

  &__connector {
    width: 2rpx;
    flex: 1;
    background-color: $ab-border;
    margin-top: 4rpx;
  }

  &:last-child .timeline-item__connector {
    display: none;
  }

  &__content {
    flex: 1;
    padding: $ab-space-xs $ab-space-sm $ab-space-md $ab-space-xs;
    min-width: 0;
  }

  &__row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8rpx;
    margin-bottom: 6rpx;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    font-family: monospace;
    margin-right: 4rpx;
  }

  &__title {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
    line-height: 1.4;
    margin-bottom: 4rpx;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__progress {
    font-size: $ab-text-xs;
    color: $ab-info;
    font-weight: $ab-font-medium;
    margin-left: 4rpx;
  }

  &__error {
    font-size: $ab-text-xs;
    color: $ab-danger;
    margin-top: 4rpx;
    line-height: 1.4;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__actions {
    display: flex;
    gap: $ab-space-sm;
    margin-top: $ab-space-xs;
  }

  &__btn {
    padding: 6rpx $ab-space-md;
    border-radius: $ab-radius-full;
    border: 2rpx solid $ab-border;

    &--pause {
      border-color: $ab-warning;
    }

    &--resume {
      border-color: $ab-success;
    }
  }

  &__btn-text {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
  }
}

@keyframes dot-pulse {
  0%, 100% {
    box-shadow: 0 0 0 4rpx $ab-info-bg;
  }
  50% {
    box-shadow: 0 0 0 8rpx rgba($ab-info, 0.15);
  }
}

// Filter Popup
.filter-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 100;
}

.filter-popup {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg $ab-radius-lg 0 0;
  z-index: 101;
  transform: translateY(100%);
  transition: transform 0.3s ease;

  &--show {
    transform: translateY(0);
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: $ab-space-md $ab-space-lg;
    border-bottom: 2rpx solid $ab-border;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__reset {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__body {
    max-height: 60vh;
    padding: $ab-space-md $ab-space-lg;
  }

  &__footer {
    padding: $ab-space-md $ab-space-lg;
    padding-bottom: calc(#{$ab-space-lg} + env(safe-area-inset-bottom));
    border-top: 2rpx solid $ab-border;
  }

  &__confirm {
    background-color: $ab-primary;
    border-radius: $ab-radius-md;
    padding: $ab-space-sm 0;
    text-align: center;
  }

  &__confirm-text {
    font-size: $ab-text-md;
    color: #FFFFFF;
    font-weight: $ab-font-semibold;
  }
}

.filter-section {
  margin-bottom: $ab-space-md;

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    font-weight: $ab-font-medium;
    margin-bottom: $ab-space-xs;
    display: block;
  }

  &__options {
    display: flex;
    flex-wrap: wrap;
    gap: $ab-space-xs;
  }
}

.filter-chip {
  padding: 10rpx $ab-space-md;
  border-radius: $ab-radius-full;
  border: 2rpx solid $ab-border;
  background-color: $ab-surface;

  &--active {
    background-color: $ab-primary-bg;
    border-color: $ab-primary;
  }

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;

    .filter-chip--active & {
      color: $ab-primary;
      font-weight: $ab-font-medium;
    }
  }
}

.filter-select {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-sm $ab-space-md;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;

  &__text {
    font-size: $ab-text-base;
    color: $ab-text;
  }

  &__arrow {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }
}
</style>
