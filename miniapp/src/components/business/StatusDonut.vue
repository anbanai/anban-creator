<template>
  <view class="status-donut">
    <view class="status-donut__ring" :style="ringStyle">
      <view class="status-donut__hole">
        <text class="status-donut__total">{{ total }}</text>
        <text class="status-donut__total-label">总样本</text>
      </view>
    </view>
    <view class="status-donut__legend">
      <view v-for="(item, i) in legend" :key="item.name" class="status-donut__legend-item">
        <view class="status-donut__legend-dot" :style="{ backgroundColor: item.color }" />
        <text class="status-donut__legend-name">{{ item.name }}</text>
        <text class="status-donut__legend-value">{{ item.value }}</text>
        <text class="status-donut__legend-pct">{{ item.pct }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  completed: number
  failed: number
  cancelled: number
}>()

// Fixed palette matching the studio chart-1..4 cadence (success / danger / neutral).
const COLOR_COMPLETED = '#4F46E5' // primary (chart-1)
const COLOR_FAILED = '#DC2626' // danger
const COLOR_CANCELLED = '#9CA3AF' // neutral

const total = computed(() => props.completed + props.failed + props.cancelled)

const ringStyle = computed(() => {
  const t = total.value
  if (t === 0) {
    return { background: `${COLOR_CANCELLED}` }
  }
  const c = (props.completed / t) * 100
  const f = (props.failed / t) * 100
  // cancelled takes the remainder
  const cStop = c
  const fStop = c + f
  // conic-gradient(from 0deg, color1 0% cStop%, color2 cStop% fStop%, color3 fStop% 100%)
  return {
    background: `conic-gradient(${COLOR_COMPLETED} 0% ${cStop}%, ${COLOR_FAILED} ${cStop}% ${fStop}%, ${COLOR_CANCELLED} ${fStop}% 100%)`,
  }
})

const legend = computed(() => {
  const t = total.value
  const pct = (n: number) => (t > 0 ? `${Math.round((n / t) * 100)}%` : '0%')
  return [
    { name: '已完成', value: props.completed, pct: pct(props.completed), color: COLOR_COMPLETED },
    { name: '失败', value: props.failed, pct: pct(props.failed), color: COLOR_FAILED },
    { name: '已取消', value: props.cancelled, pct: pct(props.cancelled), color: COLOR_CANCELLED },
  ]
})
</script>

<style lang="scss" scoped>
.status-donut {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: $ab-space-md;
  padding: $ab-space-sm 0;

  &__ring {
    width: 240rpx;
    height: 240rpx;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
  }

  &__hole {
    width: 160rpx;
    height: 160rpx;
    border-radius: 50%;
    background-color: $ab-surface;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
  }

  &__total {
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    line-height: 1.1;
  }

  &__total-label {
    font-size: 20rpx;
    color: $ab-text-tertiary;
    margin-top: 4rpx;
  }

  &__legend {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
    padding: 0 $ab-space-sm;
    box-sizing: border-box;
  }

  &__legend-item {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
  }

  &__legend-dot {
    width: 16rpx;
    height: 16rpx;
    border-radius: 50%;
    flex-shrink: 0;
  }

  &__legend-name {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    flex: 1;
  }

  &__legend-value {
    font-size: $ab-text-xs;
    color: $ab-text;
    font-weight: $ab-font-medium;
    min-width: 48rpx;
    text-align: right;
  }

  &__legend-pct {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    min-width: 64rpx;
    text-align: right;
  }
}
</style>
