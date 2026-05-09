<template>
  <view
    class="ab-switch"
    :class="{ 'ab-switch--active': modelValue, 'ab-switch--disabled': disabled }"
    @tap="toggle"
  >
    <view class="ab-switch__track">
      <view class="ab-switch__thumb" />
    </view>
  </view>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  if (!props.disabled) {
    emit('update:modelValue', !props.modelValue)
  }
}
</script>

<style lang="scss" scoped>
.ab-switch {
  display: inline-flex;
  align-items: center;
  cursor: pointer;

  &--disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  &__track {
    width: 88rpx;
    height: 48rpx;
    background-color: #D1D5DB;
    border-radius: $ab-radius-full;
    position: relative;
    transition: background-color 0.3s ease;
    box-sizing: border-box;
  }

  &--active &__track {
    background-color: $ab-primary;
  }

  &__thumb {
    position: absolute;
    top: 4rpx;
    left: 4rpx;
    width: 40rpx;
    height: 40rpx;
    background-color: #FFFFFF;
    border-radius: 50%;
    box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.15);
    transition: transform 0.3s ease;
  }

  &--active &__thumb {
    transform: translateX(40rpx);
  }
}
</style>
