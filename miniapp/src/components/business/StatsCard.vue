<template>
  <view class="stats-card">
    <text class="stats-card__value">{{ value }}</text>
    <text class="stats-card__label">{{ label }}</text>
    <text v-if="desc" class="stats-card__desc">{{ desc }}</text>
  </view>
</template>

<script setup lang="ts">
defineProps<{
  value: string | number
  label: string
  desc?: string
}>()
</script>

<style lang="scss" scoped>
.stats-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md $ab-space-sm;
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  width: 100%;
  box-sizing: border-box;

  &__value {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    line-height: 1.2;
  }

  &__label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: $ab-space-xs;
  }

  &__desc {
    font-size: 20rpx;
    color: $ab-text-tertiary;
    margin-top: 4rpx;
    text-align: center;
    line-height: 1.4;
  }
}
</style>
