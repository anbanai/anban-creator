<template>
  <text class="ab-badge" :class="[`ab-badge--${variant}`, `ab-badge--${size}`]">
    <slot />
  </text>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  variant?: 'success' | 'danger' | 'warning' | 'info' | 'neutral'
  size?: 'sm' | 'md'
}>(), {
  variant: 'neutral',
  size: 'md',
})
</script>

<style lang="scss" scoped>
.ab-badge {
  display: inline-flex;
  align-items: center;
  border-radius: $ab-radius-full;
  font-weight: $ab-font-medium;
  white-space: nowrap;
  box-sizing: border-box;
  line-height: 1.4;

  &--sm {
    padding: 4rpx 16rpx;
    font-size: $ab-text-xs;
  }

  &--md {
    padding: 6rpx 20rpx;
    font-size: $ab-text-sm;
  }

  // Variants
  &--success {
    background-color: $ab-success-bg;
    color: $ab-success;
  }

  &--danger {
    background-color: $ab-danger-bg;
    color: $ab-danger;
  }

  &--warning {
    background-color: $ab-warning-bg;
    color: $ab-warning;
  }

  &--info {
    background-color: $ab-info-bg;
    color: $ab-info;
  }

  &--neutral {
    background-color: #F3F4F6;
    color: $ab-text-secondary;
  }
}
</style>
