<template>
  <view class="ab-loading">
    <view class="ab-loading__spinner" :class="`ab-loading__spinner--${size}`" />
    <text v-if="text" class="ab-loading__text">{{ text }}</text>
  </view>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  size?: 'sm' | 'md'
  text?: string
}>(), {
  size: 'md',
  text: '',
})
</script>

<style lang="scss" scoped>
.ab-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: $ab-space-lg;

  &__spinner {
    border: 4rpx solid rgba($ab-primary, 0.2);
    border-top-color: $ab-primary;
    border-radius: 50%;
    animation: ab-loading-spin 0.7s linear infinite;
    box-sizing: border-box;

    &--sm {
      width: 40rpx;
      height: 40rpx;
      border-width: 3rpx;
    }

    &--md {
      width: 64rpx;
      height: 64rpx;
      border-width: 5rpx;
    }
  }

  &__text {
    margin-top: $ab-space-sm;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }
}

@keyframes ab-loading-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
