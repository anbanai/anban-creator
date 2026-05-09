<template>
  <button
    class="ab-button"
    :class="[
      `ab-button--${type}`,
      `ab-button--${size}`,
      {
        'ab-button--block': block,
        'ab-button--disabled': disabled || loading,
        'ab-button--loading': loading,
      },
    ]"
    :disabled="disabled || loading"
    hover-class="ab-button--hover"
    @click="handleClick"
  >
    <view v-if="loading" class="ab-button__spinner" />
    <slot />
  </button>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  type?: 'primary' | 'danger' | 'ghost'
  size?: 'sm' | 'md' | 'lg'
  block?: boolean
  disabled?: boolean
  loading?: boolean
}>(), {
  type: 'primary',
  size: 'md',
  block: false,
  disabled: false,
  loading: false,
})

const emit = defineEmits<{
  (e: 'click'): void
}>()

function handleClick() {
  if (!props.disabled && !props.loading) {
    emit('click')
  }
}
</script>

<style lang="scss" scoped>
.ab-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: $ab-radius-sm;
  font-weight: $ab-font-medium;
  font-size: $ab-text-base;
  line-height: 1.5;
  text-align: center;
  white-space: nowrap;
  transition: opacity 0.2s ease;
  position: relative;
  box-sizing: border-box;

  &::after {
    border: none;
  }

  // Types
  &--primary {
    background-color: $ab-primary;
    color: #FFFFFF;
  }

  &--danger {
    background-color: $ab-danger;
    color: #FFFFFF;
  }

  &--ghost {
    background-color: transparent;
    color: $ab-primary;
    border: 2rpx solid $ab-primary;
  }

  // Sizes
  &--sm {
    padding: 8rpx $ab-space-md;
    font-size: $ab-text-sm;
  }

  &--md {
    padding: 16rpx $ab-space-lg;
    font-size: $ab-text-base;
  }

  &--lg {
    padding: 20rpx $ab-space-xl;
    font-size: $ab-text-md;
  }

  // Block
  &--block {
    display: flex;
    width: 100%;
  }

  // States
  &--disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  &--hover:not(&--disabled) {
    opacity: 0.85;
  }

  // Loading
  &--loading {
    pointer-events: none;
  }

  &__spinner {
    width: 32rpx;
    height: 32rpx;
    border: 4rpx solid rgba(255, 255, 255, 0.3);
    border-top-color: #FFFFFF;
    border-radius: 50%;
    animation: ab-spin 0.6s linear infinite;
    margin-right: $ab-space-xs;
    flex-shrink: 0;
  }

  &--ghost &__spinner {
    border-color: rgba($ab-primary, 0.3);
    border-top-color: $ab-primary;
  }
}

@keyframes ab-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
