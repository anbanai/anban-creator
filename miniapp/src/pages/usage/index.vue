<template>
  <view class="usage-page">
    <view class="date-tabs">
      <view
        v-for="tab in dateRangeTabs"
        :key="tab.key"
        class="date-tabs__item"
        :class="{ active: activeDateRange === tab.key }"
        @tap="onDateRangeChange(tab.key)"
      >
        {{ tab.label }}
      </view>
    </view>

    <view class="summary">
      <text class="summary__label">任务数</text>
      <text class="summary__value">{{ stats.total_tasks.toLocaleString() }}</text>
      <text class="summary__range">{{ activeRangeLabel }}</text>
    </view>

    <view class="section-title">按类型分组</view>
    <AbLoading v-if="loading" text="加载中..." />
    <AbEmpty v-else-if="typeEntries.length === 0" title="暂无任务" description="当前时间范围内没有创作任务" />
    <view v-else class="type-list">
      <view v-for="entry in typeEntries" :key="entry.key" class="type-row">
        <PlatformAvatar :platform="entry.key" :size="36" />
        <text class="type-row__label">{{ entry.label }}</text>
        <text class="type-row__count">{{ entry.count.toLocaleString() }} 个</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { usageApi } from '@/api/usage'
import { contentTypeLabel } from '@/utils/labels'
import type { UsageStats } from '@/types'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

type RangeKey = '7d' | '30d' | '90d' | 'month'
const dateRangeTabs: { key: RangeKey; label: string }[] = [
  { key: '7d', label: '近7天' },
  { key: '30d', label: '近30天' },
  { key: '90d', label: '近90天' },
  { key: 'month', label: '本月' },
]
const activeDateRange = ref<RangeKey>('30d')
const activeRangeLabel = computed(() => dateRangeTabs.find((tab) => tab.key === activeDateRange.value)?.label || '')
const loading = ref(false)
const stats = reactive<UsageStats>({ total_tasks: 0, by_type: {} })

function formatDate(d: Date) {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function range() {
  const now = new Date()
  const from = new Date(now)
  if (activeDateRange.value === 'month') from.setDate(1)
  else from.setDate(now.getDate() - Number(activeDateRange.value.slice(0, -1)) + 1)
  return { from: formatDate(from), to: formatDate(now) }
}

const typeEntries = computed(() =>
  Object.entries(stats.by_type || {})
    .map(([key, entry]) => ({ key, label: contentTypeLabel[key] || key, count: entry.count }))
    .sort((a, b) => b.count - a.count),
)

async function load() {
  loading.value = true
  try {
    Object.assign(stats, await usageApi.stats(range()))
  } catch (err: any) {
    uni.showToast({ title: err?.message || '加载失败', icon: 'none' })
  } finally {
    loading.value = false
  }
}

function onDateRangeChange(key: RangeKey) {
  activeDateRange.value = key
  void load()
}

onMounted(load)
</script>

<style lang="scss" scoped>
.usage-page { min-height: 100vh; padding: 28rpx; background: $ab-background; color: $ab-text; }
.date-tabs { display: flex; gap: 8rpx; padding: 8rpx; border-radius: $ab-radius-md; background: $ab-surface; }
.date-tabs__item { flex: 1; padding: 16rpx 8rpx; border-radius: $ab-radius-sm; text-align: center; color: $ab-text-secondary; font-size: $ab-text-sm; }
.date-tabs__item.active { background: $ab-primary; color: #fff; }
.summary { display: flex; flex-direction: column; gap: 8rpx; margin-top: 24rpx; padding: 32rpx; border-radius: $ab-radius-lg; background: $ab-surface; }
.summary__label, .summary__range { color: $ab-text-secondary; font-size: $ab-text-sm; }
.summary__value { font-size: 64rpx; font-weight: 700; }
.section-title { margin: 36rpx 0 16rpx; font-weight: 600; }
.type-list { overflow: hidden; border-radius: $ab-radius-md; background: $ab-surface; }
.type-row { display: flex; align-items: center; gap: 20rpx; padding: 24rpx; border-bottom: 1rpx solid $ab-divider; }
.type-row__label { flex: 1; }
.type-row__count { color: $ab-text-secondary; font-variant-numeric: tabular-nums; }
</style>
