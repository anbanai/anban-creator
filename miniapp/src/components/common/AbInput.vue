<template>
  <view class="ab-input-wrapper">
    <input
      class="ab-input"
      :class="{ 'ab-input--error': error, 'ab-input--disabled': disabled }"
      :type="type === 'number' ? 'digit' : type"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :password="type === 'password'"
      placeholder-class="ab-input__placeholder"
      @input="onInput"
    />
    <text v-if="error" class="ab-input__error">{{ error }}</text>
  </view>
</template>

<script setup lang="ts">
defineProps<{
  modelValue: string
  placeholder?: string
  type?: 'text' | 'password' | 'number'
  disabled?: boolean
  error?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

function onInput(e: any) {
  emit('update:modelValue', e.detail.value)
}
</script>

<style lang="scss" scoped>
.ab-input-wrapper {
  width: 100%;
}

.ab-input {
  width: 100%;
  height: 80rpx;
  padding: $ab-space-sm $ab-space-md;
  font-size: $ab-text-base;
  color: $ab-text;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  box-sizing: border-box;
  transition: border-color 0.2s ease;
  outline: none;

  &::after {
    border: none;
  }

  &:focus {
    border-color: $ab-primary;
  }

  &--error {
    border-color: $ab-danger;
  }

  &--disabled {
    background-color: $ab-background;
    color: $ab-text-tertiary;
    cursor: not-allowed;
  }

  &__placeholder {
    color: $ab-text-tertiary;
    font-size: $ab-text-base;
  }

  &__error {
    display: block;
    margin-top: $ab-space-xs;
    font-size: $ab-text-xs;
    color: $ab-danger;
    line-height: 1.4;
  }
}
</style>
