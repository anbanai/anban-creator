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
      <!-- Summary Cards -->
      <view class="stats-grid">
        <view class="stat-card">
          <text class="stat-card__label">输入 Token</text>
          <text class="stat-card__value">{{ formatNumberWithCommas(stats.total_input_tokens) }}</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">输出 Token</text>
          <text class="stat-card__value">{{ formatNumberWithCommas(stats.total_output_tokens) }}</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">缓存命中</text>
          <text class="stat-card__value">{{ formatNumberWithCommas(stats.total_cache_read_tokens) }}</text>
        </view>
        <view class="stat-card">
          <text class="stat-card__label">总费用</text>
          <text class="stat-card__value stat-card__value--cost">
            ${{ stats.total_cost_usd.toFixed(2) }}
          </text>
        </view>
      </view>

      <!-- Per-type Breakdown -->
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
                <PlatformAvatar :platform="entry.key" :size="32" />
                <text class="breakdown-row__name">{{ entry.label }}</text>
              </view>
              <view class="breakdown-row__metrics">
                <text class="breakdown-row__count">{{ entry.data.count }}次</text>
                <text class="breakdown-row__cost">${{ entry.data.cost_usd.toFixed(2) }}</text>
              </view>
            </view>
            <!-- Progress bar -->
            <view class="breakdown-row__bar">
              <view
                class="breakdown-row__bar-fill"
                :style="{ width: entry.percentage + '%' }"
                :class="`breakdown-row__bar-fill--${entry.barColor}`"
              />
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
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

// Date range
const dateRangeTabs = [
  { key: '7d', label: '近7天' },
  { key: '30d', label: '近30天' },
  { key: '90d', label: '近90天' },
  { key: 'month', label: '本月' },
]
const activeDateRange = ref('7d')

function getDateRange(): { from: string; to: string } {
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

// Type breakdown entries
interface TypeEntry {
  key: string
  label: string
  data: TypeStatEntry
  percentage: number
  barColor: string
}

const typeEntries = computed<TypeEntry[]>(() => {
  const byType = stats.by_type || {}
  const entries = Object.entries(byType) as [string, TypeStatEntry][]

  if (!entries.length) return []

  const maxCost = Math.max(...entries.map(([, d]) => d.cost_usd), 0.01)

  const barColors: Record<string, string> = {
    seednote: 'danger',
    article: 'success',
    xls: 'info',
  }

  return entries
    .map(([key, data]) => ({
      key,
      label: contentTypeLabel[key] || key,
      data,
      percentage: Math.max(Math.round((data.cost_usd / maxCost) * 100), 4),
      barColor: barColors[key] || 'neutral',
    }))
    .sort((a, b) => b.data.cost_usd - a.data.cost_usd)
})

// Format helpers
function formatNumberWithCommas(n: number): string {
  if (n == null) return '0'
  return Math.floor(n).toLocaleString('en-US')
}

// Fetch
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
  activeDateRange.value = key
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

// Summary Stats Grid (2x2)
.stats-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;
  padding: $ab-space-md;
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
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    line-height: 1.2;
    word-break: break-all;

    &--cost {
      color: $ab-primary;
      font-size: $ab-text-xl;
    }
  }
}

// Breakdown Section
.breakdown {
  padding: 0 $ab-space-md;

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
  }

  &__name {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__metrics {
    display: flex;
    align-items: center;
    gap: $ab-space-md;
  }

  &__count {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__cost {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__bar {
    height: 12rpx;
    background-color: $ab-divider;
    border-radius: 6rpx;
    overflow: hidden;
  }

  &__bar-fill {
    height: 100%;
    border-radius: 6rpx;
    transition: width 0.5s ease;

    &--danger {
      background-color: $ab-platform-seednote;
    }

    &--success {
      background-color: $ab-platform-wechat;
    }

    &--info {
      background-color: $ab-info;
    }

    &--neutral {
      background-color: $ab-text-tertiary;
    }
  }
}

// Empty state within scroll
.usage-empty {
  padding: 60rpx 0;
}
</style>
