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
          <view class="date-tabs__more" @tap="openRangePicker">
            <text class="date-tabs__more-label">▼</text>
          </view>
        </view>
      </scroll-view>
    </view>

    <!-- Content -->
    <view v-if="loading" class="usage-loading">
      <AbLoading text="加载中..." />
    </view>

    <scroll-view
      v-else
      class="usage-scroll"
      scroll-y
      :refresher-enabled="true"
      :refresher-triggered="refreshing"
      @refresherrefresh="onPullRefresh"
    >
      <!-- Cost hero card -->
      <view class="hero">
        <text class="hero__label">总费用</text>
        <view class="hero__row">
          <text class="hero__value">${{ stats.total_cost_usd.toFixed(2) }}</text>
          <AbBadge v-if="typeEntries.length" variant="info" size="sm">
            {{ stats.total_tasks }} 个任务
          </AbBadge>
        </view>
        <text class="hero__range">{{ activeRangeLabel }} · {{ rangeDesc }}</text>
      </view>

      <!-- Summary Stats Grid (4 cards) -->
      <view class="stats-grid">
        <view class="stat-card">
          <text class="stat-card__label">输入 Token</text>
          <text class="stat-card__value">{{ formatToken(stats.total_input_tokens) }}</text>
          <text class="stat-card__desc">发送到模型</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">输出 Token</text>
          <text class="stat-card__value">{{ formatToken(stats.total_output_tokens) }}</text>
          <text class="stat-card__desc">模型生成</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">缓存读取</text>
          <text class="stat-card__value">{{ formatToken(stats.total_cache_read_tokens) }}</text>
          <text class="stat-card__desc">命中缓存</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">缓存创建</text>
          <text class="stat-card__value">{{ formatToken(stats.total_cache_creation_tokens) }}</text>
          <text class="stat-card__desc">写入缓存</text>
        </view>
      </view>

      <!-- Read vs Creation split -->
      <view v-if="hasTokenData" class="split">
        <view class="split__header">
          <text class="split__title">读取 / 创作 分布</text>
          <text class="split__hint">按 Token 数量估算</text>
        </view>
        <view class="split__bar">
          <view
            class="split__bar-seg split__bar-seg--creation"
            :style="{ width: split.creationPct + '%' }"
          />
          <view
            class="split__bar-seg split__bar-seg--read"
            :style="{ width: split.readPct + '%' }"
          />
        </view>
        <view class="split__legend">
          <view class="split__legend-item">
            <view class="split__dot split__dot--creation" />
            <text class="split__legend-label">创作生成</text>
            <text class="split__legend-value">{{ formatToken(split.creation) }}</text>
            <text class="split__legend-pct">{{ split.creationPct }}%</text>
          </view>
          <view class="split__legend-item">
            <view class="split__dot split__dot--read" />
            <text class="split__legend-label">读取缓存</text>
            <text class="split__legend-value">{{ formatToken(split.read) }}</text>
            <text class="split__legend-pct">{{ split.readPct }}%</text>
          </view>
        </view>
      </view>

      <!-- Cost donut + ranking -->
      <view v-if="typeEntries.length" class="charts">
        <!-- Donut -->
        <view class="donut-card">
          <text class="chart-title">费用占比</text>
          <view class="donut-wrap">
            <view class="donut" :style="donutStyle">
              <view class="donut__hole">
                <text class="donut__total-label">总费用</text>
                <text class="donut__total-value">${{ stats.total_cost_usd.toFixed(2) }}</text>
              </view>
            </view>
          </view>
          <view class="donut-legend">
            <view
              v-for="(seg, idx) in donutSegments"
              :key="seg.key"
              class="donut-legend__item"
            >
              <view class="donut-legend__dot" :style="{ backgroundColor: palette[idx % palette.length] }" />
              <text class="donut-legend__name">{{ seg.label }}</text>
              <text class="donut-legend__pct">{{ seg.pct }}%</text>
            </view>
          </view>
        </view>
      </view>

      <!-- Per-type Breakdown list -->
      <view v-if="typeEntries.length" class="breakdown">
        <text class="breakdown__title">按类型分布</text>
        <view class="breakdown__list">
          <view
            v-for="entry in typeEntries"
            :key="entry.key"
            class="breakdown-row"
          >
            <view class="breakdown-row__header">
              <view class="breakdown-row__name-wrap">
                <PlatformAvatar :platform="entry.key" :size="36" />
                <view class="breakdown-row__name-col">
                  <text class="breakdown-row__name">{{ entry.label }}</text>
                  <text class="breakdown-row__tokens">
                    {{ formatToken(entry.data.input_tokens + entry.data.output_tokens) }} tok · {{ formatToken(entry.data.cache_read_tokens) }} 缓存
                  </text>
                </view>
              </view>
              <view class="breakdown-row__metrics">
                <text class="breakdown-row__count">{{ entry.data.count }}次</text>
                <text class="breakdown-row__cost">${{ entry.data.cost_usd.toFixed(2) }}</text>
              </view>
            </view>
            <!-- Progress bar (proportional to cost share) -->
            <view class="breakdown-row__bar">
              <view
                class="breakdown-row__bar-fill"
                :style="{ width: entry.sharePct + '%' }"
              />
            </view>
            <view class="breakdown-row__share">
              <text class="breakdown-row__share-text">占总费用 {{ entry.sharePct }}%</text>
            </view>
          </view>
        </view>
      </view>

      <!-- No data -->
      <view v-else class="usage-empty">
        <AbEmpty title="暂无用量数据" description="选定范围内没有 AI 使用记录" />
      </view>

      <view class="usage-bottom-spacer" />
    </scroll-view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { usageApi } from '@/api/usage'
import type { UsageStats, TypeStatEntry } from '@/types'
import { contentTypeLabel } from '@/utils/labels'
import { getMonthRange } from '@/utils/format'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

// ---- Date range ----
type RangeKey = '7d' | '30d' | '90d' | 'month' | 'today' | 'all'
const dateRangeTabs: { key: RangeKey; label: string }[] = [
  { key: 'today', label: '今天' },
  { key: '7d', label: '近7天' },
  { key: '30d', label: '近30天' },
  { key: '90d', label: '近90天' },
  { key: 'month', label: '本月' },
]
const activeDateRange = ref<RangeKey>('30d')

// Action sheet options (extra ranges not in the tab strip)
const extraRanges: { key: RangeKey; label: string }[] = [
  { key: 'all', label: '全部' },
]

const activeRangeLabel = computed(
  () => [...dateRangeTabs, ...extraRanges].find((t) => t.key === activeDateRange.value)?.label || '',
)
const rangeDesc = computed(() => {
  const r = getDateRange()
  if (activeDateRange.value === 'all') return '历史全部数据'
  if (activeDateRange.value === 'today') return r.from
  return `${r.from} ~ ${r.to}`
})

function getDateRange(): { from: string; to: string } {
  const now = new Date()
  const to = formatDateYMD(now)
  switch (activeDateRange.value) {
    case 'today':
      return { from: to, to }
    case '7d':
      return { from: shiftDays(now, 6), to }
    case '30d':
      return { from: shiftDays(now, 29), to }
    case '90d':
      return { from: shiftDays(now, 89), to }
    case 'month':
      return getMonthRange(now)
    case 'all':
      // Backend treats empty `from`/`to` as no-bound; we send a far-past date.
      return { from: '2000-01-01', to }
  }
}

function shiftDays(base: Date, days: number): string {
  const d = new Date(base)
  d.setDate(d.getDate() - days)
  return formatDateYMD(d)
}

function formatDateYMD(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function openRangePicker() {
  const items = extraRanges.map((r) => r.label)
  uni.showActionSheet({
    itemList: items.length ? items : ['全部'],
    success: (res) => {
      const picked = extraRanges[res.tapIndex]
      if (picked) onDateRangeChange(picked.key)
    },
  })
}

// ---- State ----
const loading = ref(false)
const refreshing = ref(false)

const defaultStats: UsageStats = {
  total_tasks: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_read_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cost_usd: 0,
  by_type: {},
}
const stats = reactive<UsageStats>({ ...defaultStats })

// ---- Read vs Creation split ----
// Derived client-side from token buckets returned by the backend:
//   Creation = input + output (fresh generation work sent/consumed by the model)
//   Read     = cache_read + cache_creation (cached retrieval, prompt-cache side)
// No new backend field is required; we only re-aggregate existing data.
const hasTokenData = computed(
  () =>
    stats.total_input_tokens +
      stats.total_output_tokens +
      stats.total_cache_read_tokens +
      stats.total_cache_creation_tokens >
    0,
)

const split = computed(() => {
  const creation = stats.total_input_tokens + stats.total_output_tokens
  const read = stats.total_cache_read_tokens + stats.total_cache_creation_tokens
  const total = creation + read
  if (total <= 0) {
    return { creation: 0, read: 0, creationPct: 0, readPct: 0 }
  }
  const creationPct = Math.round((creation / total) * 100)
  const readPct = 100 - creationPct
  return { creation, read, creationPct, readPct }
})

// ---- Per-type breakdown ----
interface TypeEntry {
  key: string
  label: string
  data: TypeStatEntry
  sharePct: number
}

const typeEntries = computed<TypeEntry[]>(() => {
  const byType = stats.by_type || {}
  const entries = Object.entries(byType) as [string, TypeStatEntry][]
  if (!entries.length) return []

  const totalCost = entries.reduce((sum, [, d]) => sum + d.cost_usd, 0) || 1

  return entries
    .map(([key, data]) => ({
      key,
      label: contentTypeLabel[key] || key,
      data,
      sharePct: Math.round((data.cost_usd / totalCost) * 100),
    }))
    .sort((a, b) => b.data.cost_usd - a.data.cost_usd)
})

// ---- Donut ----
// 8-color palette tuned for mp-weixin conic-gradient rendering.
const palette = [
  '#4F46E5', // primary
  '#07C160', // wechat green
  '#FF2442', // seednote red
  '#2563EB', // info
  '#D97706', // warning
  '#059669', // success
  '#9333EA', // purple
  '#6B7280', // neutral
]

interface DonutSegment {
  key: string
  label: string
  pct: number // 0..100
}

const donutSegments = computed<DonutSegment[]>(() =>
  typeEntries.value.map((e) => ({
    key: e.key,
    label: e.label,
    pct: e.sharePct,
  })),
)

// Build a conic-gradient string from segments.
const donutStyle = computed(() => {
  const segs = donutSegments.value
  if (!segs.length) {
    return { background: `$ab-divider` } as Record<string, string>
  }
  let acc = 0
  const stops: string[] = []
  segs.forEach((s, idx) => {
    const color = palette[idx % palette.length]
    const start = acc
    acc += s.pct
    stops.push(`${color} ${start}% ${acc}%`)
  })
  return {
    background: `conic-gradient(${stops.join(', ')})`,
  } as Record<string, string>
})

// ---- Format helpers ----
function formatToken(n: number): string {
  if (n == null || isNaN(n)) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return Math.floor(n).toLocaleString('en-US')
}

function formatNumberWithCommas(n: number): string {
  if (n == null) return '0'
  return Math.floor(n).toLocaleString('en-US')
}
// Kept for backward compatibility if referenced elsewhere; not currently used in template.
void formatNumberWithCommas

// ---- Fetch ----
async function fetchStats(showRefresh = false) {
  if (showRefresh) {
    refreshing.value = true
  } else {
    loading.value = true
  }

  try {
    const range = getDateRange()
    const res = await usageApi.stats({ from: range.from, to: range.to })

    Object.assign(stats, {
      total_tasks: res.total_tasks || 0,
      total_input_tokens: res.total_input_tokens || 0,
      total_output_tokens: res.total_output_tokens || 0,
      total_cache_read_tokens: res.total_cache_read_tokens || 0,
      total_cache_creation_tokens: res.total_cache_creation_tokens || 0,
      total_cost_usd: res.total_cost_usd || 0,
      by_type: res.by_type || {},
    })
  } catch (err: any) {
    uni.showToast({ title: err?.message || '加载失败', icon: 'none' })
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

function onDateRangeChange(key: string) {
  activeDateRange.value = key as RangeKey
  fetchStats()
}

function onPullRefresh() {
  fetchStats(true)
}

onMounted(() => {
  fetchStats()
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
  background-color: $ab-surface;
  border-bottom: 2rpx solid $ab-border;
  padding: 0 $ab-space-sm;
  flex-shrink: 0;

  &__scroll {
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

  &__more {
    padding: $ab-space-sm $ab-space-md;
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
  }

  &__more-label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Loading
.usage-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 120rpx 0;
}

// Scroll
.usage-scroll {
  flex: 1;
  height: 0;
}

.usage-bottom-spacer {
  height: $ab-space-xl;
}

// ---- Hero cost card ----
.hero {
  margin: $ab-space-md;
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  padding: $ab-space-lg $ab-space-md;
  display: flex;
  flex-direction: column;
  border-left: 8rpx solid $ab-primary;

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-xs;
  }

  &__row {
    display: flex;
    align-items: baseline;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-xs;
  }

  &__value {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    line-height: 1.1;
  }

  &__range {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Summary Stats Grid (2x2)
.stats-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;
  padding: 0 $ab-space-md;
}

.stat-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  padding: $ab-space-md;
  display: flex;
  flex-direction: column;

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-xs;
  }

  &__value {
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    line-height: 1.2;
    word-break: break-all;
  }

  &__desc {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: $ab-space-xs;
  }
}

// ---- Read vs Creation split ----
.split {
  margin: $ab-space-md;
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  padding: $ab-space-md;

  &__header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
  }

  &__title {
    font-size: $ab-text-base;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__bar {
    display: flex;
    width: 100%;
    height: 20rpx;
    border-radius: 10rpx;
    overflow: hidden;
    background-color: $ab-divider;
    margin-bottom: $ab-space-sm;
  }

  &__bar-seg {
    height: 100%;
    transition: width 0.4s ease;

    &--creation {
      background-color: $ab-primary;
    }

    &--read {
      background-color: $ab-info;
    }
  }

  &__legend {
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
  }

  &__legend-item {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
  }

  &__dot {
    width: 16rpx;
    height: 16rpx;
    border-radius: 50%;
    flex-shrink: 0;

    &--creation {
      background-color: $ab-primary;
    }

    &--read {
      background-color: $ab-info;
    }
  }

  &__legend-label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    flex: 1;
  }

  &__legend-value {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__legend-pct {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
    min-width: 72rpx;
    text-align: right;
  }
}

// ---- Charts row ----
.charts {
  padding: 0 $ab-space-md;
  margin-top: $ab-space-sm;
}

.chart-title {
  font-size: $ab-text-sm;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  margin-bottom: $ab-space-sm;
}

.donut-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  padding: $ab-space-md;
}

.donut-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  margin: $ab-space-sm 0;
}

.donut {
  width: 220rpx;
  height: 220rpx;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;

  &__hole {
    width: 150rpx;
    height: 150rpx;
    border-radius: 50%;
    background-color: $ab-surface;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
  }

  &__total-label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__total-value {
    font-size: $ab-text-base;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    margin-top: 4rpx;
  }
}

.donut-legend {
  display: flex;
  flex-wrap: wrap;
  gap: $ab-space-sm $ab-space-md;
  margin-top: $ab-space-sm;

  &__item {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    min-width: 45%;
  }

  &__dot {
    width: 14rpx;
    height: 14rpx;
    border-radius: 50%;
    flex-shrink: 0;
  }

  &__name {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    flex: 1;
  }

  &__pct {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }
}

// Breakdown Section
.breakdown {
  padding: 0 $ab-space-md;
  margin-top: $ab-space-md;

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    margin-bottom: $ab-space-sm;
    padding-top: $ab-space-xs;
  }

  &__list {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    box-shadow: $ab-shadow-sm;
    overflow: hidden;
  }
}

.breakdown-row {
  padding: $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-xs;
  }

  &__name-wrap {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    flex: 1;
    min-width: 0;
  }

  &__name-col {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  &__name {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__tokens {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 2rpx;
  }

  &__metrics {
    display: flex;
    align-items: center;
    gap: $ab-space-md;
    flex-shrink: 0;
  }

  &__count {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__cost {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-semibold;
  }

  &__bar {
    height: 12rpx;
    background-color: $ab-divider;
    border-radius: 6rpx;
    overflow: hidden;
    margin-top: $ab-space-xs;
  }

  &__bar-fill {
    height: 100%;
    border-radius: 6rpx;
    background-color: $ab-primary;
    transition: width 0.5s ease;
  }

  &__share {
    margin-top: $ab-space-xs;
  }

  &__share-text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Empty state within scroll
.usage-empty {
  padding: 60rpx 0;
}
</style>
