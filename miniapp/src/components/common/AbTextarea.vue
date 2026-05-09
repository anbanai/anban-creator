<template>
  <view class="ab-textarea-wrapper">
    <textarea
      class="ab-textarea"
      :class="{ 'ab-textarea--error': error, 'ab-textarea--disabled': disabled }"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :maxlength="maxlength"
      :auto-height="rows === 0"
      :style="rowsStyle"
      placeholder-class="ab-textarea__placeholder"
      @input="onInput"
    />
    <view class="ab-textarea__footer">
      <text v-if="error" class="ab-textarea__error">{{ error }}</text>
      <text v-if="maxlength" class="ab-textarea__count">{{ currentLength }}/{{ maxlength }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  maxlength?: number
  disabled?: boolean
  error?: string
  rows?: number
}>(), {
  placeholder: '',
  maxlength: 0,
  disabled: false,
  error: '',
  rows: 3,
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const currentLength = computed(() => (props.modelValue || '').length)

const rowsStyle = computed(() => {
  if (props.rows === 0) return {}
  return {
    height: `${props.rows * 48 + 32}rpx`,
  }
})

function onInput(e: any) {
  emit('update:modelValue', e.detail.value)
}
</script>

<style lang="scss" scoped>
.ab-textarea-wrapper {
  width: 100%;
}

.ab-textarea {
  width: 100%;
  padding: $ab-space-sm $ab-space-md;
  font-size: $ab-text-base;
  color: $ab-text;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  box-sizing: border-box;
  transition: border-color 0.2s ease;
  outline: none;
  line-height: 1.6;

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

  &__footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-top: $ab-space-xs;
    min-height: 32rpx;
  }

  &__error {
    font-size: $ab-text-xs;
    color: $ab-danger;
    line-height: 1.4;
  }

  &__count {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-left: auto;
  }
}
</style>
