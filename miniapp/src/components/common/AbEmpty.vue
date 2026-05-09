<template>
  <view class="ab-empty">
    <text class="ab-empty__icon">📭</text>
    <text v-if="title" class="ab-empty__title">{{ title }}</text>
    <text v-if="description" class="ab-empty__desc">{{ description }}</text>
    <view v-if="actionText" class="ab-empty__action" @tap="$emit('action')">
      <text class="ab-empty__action-text">{{ actionText }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
defineProps<{
  title?: string
  description?: string
  actionText?: string
}>()

defineEmits<{
  (e: 'action'): void
}>()
</script>

<style lang="scss" scoped>
.ab-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: $ab-space-xl $ab-space-lg;

  &__icon {
    font-size: 120rpx;
    margin-bottom: $ab-space-md;
    line-height: 1;
  }

  &__title {
    font-size: $ab-text-lg;
    color: $ab-text-secondary;
    font-weight: $ab-font-medium;
    margin-bottom: $ab-space-xs;
  }

  &__desc {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    text-align: center;
    line-height: 1.5;
    margin-bottom: $ab-space-md;
  }

  &__action {
    margin-top: $ab-space-xs;
  }

  &__action-text {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
    padding: 12rpx $ab-space-lg;
    border: 2rpx solid $ab-primary;
    border-radius: $ab-radius-full;
  }
}
</style>
