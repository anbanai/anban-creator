<template>
  <view class="ab-progress">
    <view class="ab-progress__track" :style="{ height, borderRadius: height }">
      <view
        class="ab-progress__fill"
        :style="fillStyle"
      />
    </view>
    <text v-if="showText" class="ab-progress__text">{{ clampedPercent }}%</text>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  percent: number
  color?: string
  height?: string
  showText?: boolean
}>(), {
  color: '',
  height: '12rpx',
  showText: false,
})

const clampedPercent = computed(() => Math.min(100, Math.max(0, props.percent)))

const fillStyle = computed(() => ({
  width: `${clampedPercent.value}%`,
  backgroundColor: props.color || undefined,
}))
</script>

<style lang="scss" scoped>
.ab-progress {
  display: flex;
  align-items: center;
  width: 100%;
  gap: $ab-space-sm;

  &__track {
    flex: 1;
    background-color: $ab-divider;
    border-radius: $ab-radius-full;
    overflow: hidden;
    min-height: 12rpx;
  }

  &__fill {
    height: 100%;
    background-color: $ab-primary;
    border-radius: $ab-radius-full;
    transition: width 0.3s ease;
    min-width: 0;
  }

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    white-space: nowrap;
    min-width: 64rpx;
    text-align: right;
  }
}
</style>
