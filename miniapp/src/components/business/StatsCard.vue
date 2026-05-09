<template>
  <view class="stats-card">
    <text class="stats-card__value">{{ value }}</text>
    <text class="stats-card__label">{{ label }}</text>
  </view>
</template>

<script setup lang="ts">
defineProps<{
  value: string | number
  label: string
}>()
</script>

<style lang="scss" scoped>
.stats-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: $ab-space-sm;

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
}
</style>
