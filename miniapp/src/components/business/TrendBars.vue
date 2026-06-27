<template>
  <view class="trend-bars">
    <view class="trend-bars__plot">
      <view
        v-for="(point, i) in data"
        :key="i"
        class="trend-bars__col"
        @tap="onTap(point)"
      >
        <view class="trend-bars__bar-wrap">
          <view
            class="trend-bars__bar"
            :style="{ height: barHeight(point.count) + '%' }"
          />
        </view>
      </view>
    </view>
    <view class="trend-bars__axis">
      <text class="trend-bars__tick">{{ firstLabel }}</text>
      <text class="trend-bars__tick">{{ midLabel }}</text>
      <text class="trend-bars__tick">{{ lastLabel }}</text>
    </view>
    <view class="trend-bars__summary">
      <text class="trend-bars__summary-label">合计</text>
      <text class="trend-bars__summary-value">{{ total }}</text>
      <text class="trend-bars__summary-sep">·</text>
      <text class="trend-bars__summary-label">峰值</text>
      <text class="trend-bars__summary-value">{{ peak }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface TrendPoint {
  date: string
  count: number
}

const props = defineProps<{
  data: TrendPoint[]
}>()

// Use a small floor so a value of 1 still renders a visible sliver relative
// to a large peak, while 0 renders nothing.
const max = computed(() => {
  const m = props.data.reduce((acc, p) => Math.max(acc, p.count), 0)
  return m > 0 ? m : 1
})

function barHeight(count: number): number {
  if (count <= 0) return 0
  if (max.value === 0) return 0
  return Math.max(6, Math.round((count / max.value) * 100))
}

const total = computed(() => props.data.reduce((acc, p) => acc + p.count, 0))
const peak = computed(() => max.value)

const firstLabel = computed(() => props.data[0]?.date ?? '')
const midLabel = computed(() => {
  const mid = props.data[Math.floor(props.data.length / 2)]
  return mid?.date ?? ''
})
const lastLabel = computed(() => props.data[props.data.length - 1]?.date ?? '')

function onTap(point: TrendPoint) {
  uni.showToast({
    title: `${point.date}：${point.count} 个任务`,
    icon: 'none',
    duration: 1200,
  })
}
</script>

<style lang="scss" scoped>
.trend-bars {
  width: 100%;

  &__plot {
    display: flex;
    align-items: flex-end;
    height: 280rpx;
    gap: 4rpx;
    padding: 0 4rpx;
    box-sizing: border-box;
  }

  &__col {
    flex: 1;
    height: 100%;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    min-width: 0;
  }

  &__bar-wrap {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: flex-end;
  }

  &__bar {
    width: 100%;
    background: linear-gradient(180deg, $ab-primary-light 0%, $ab-primary 100%);
    border-radius: 4rpx 4rpx 0 0;
    min-height: 0;
    transition: height 0.4s ease;
  }

  &__axis {
    display: flex;
    justify-content: space-between;
    margin-top: $ab-space-xs;
    padding: 0 4rpx;
  }

  &__tick {
    font-size: 20rpx;
    color: $ab-text-tertiary;
  }

  &__summary {
    display: flex;
    align-items: center;
    gap: 6rpx;
    margin-top: $ab-space-sm;
    padding-top: $ab-space-sm;
    border-top: 2rpx solid $ab-divider;
  }

  &__summary-label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__summary-value {
    font-size: $ab-text-xs;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__summary-sep {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin: 0 8rpx;
  }
}
</style>
