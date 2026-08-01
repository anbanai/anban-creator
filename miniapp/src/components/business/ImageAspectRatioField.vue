<template>
  <view class="ratio-field">
    <view
      v-for="option in options"
      :key="option.value || 'auto'"
      class="ratio-option"
      :class="{
        'ratio-option--active': modelValue === option.value,
        'ratio-option--disabled': !isSupported(option.value),
      }"
      @tap="select(option.value)"
    >
      <text class="ratio-option__label">{{ option.label }}</text>
      <text class="ratio-option__hint">{{ option.hint }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: string
  supportedSizes?: string[]
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()

const options = [
  { value: '', label: '智能适配', hint: '按每张产物选择' },
  { value: '3:4', label: '3:4', hint: '竖版' },
  { value: '1:1', label: '1:1', hint: '方形' },
  { value: '4:3', label: '4:3', hint: '横版' },
  { value: '16:9', label: '16:9', hint: '宽屏' },
  { value: '3:2', label: '3:2', hint: '横版' },
  { value: '2:3', label: '2:3', hint: '竖版' },
  { value: '9:16', label: '9:16', hint: '竖屏' },
  { value: '21:9', label: '21:9', hint: '超宽' },
]

function isSupported(value: string) {
  return value === '' || !props.supportedSizes || props.supportedSizes.includes(value)
}

function select(value: string) {
  if (isSupported(value)) emit('update:modelValue', value)
}
</script>

<style lang="scss" scoped>
.ratio-field { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: $ab-space-sm; }
.ratio-option { min-width: 0; padding: $ab-space-sm; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; background: $ab-surface; text-align: center; }
.ratio-option--active { border-color: $ab-primary; background: $ab-primary-bg; }
.ratio-option--disabled { opacity: .42; }
.ratio-option__label, .ratio-option__hint { display: block; }
.ratio-option__label { color: $ab-text; font-size: $ab-text-sm; font-weight: $ab-font-medium; }
.ratio-option__hint { margin-top: 4rpx; color: $ab-text-tertiary; font-size: $ab-text-xs; }
</style>
