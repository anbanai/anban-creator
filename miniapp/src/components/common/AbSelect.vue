<template>
  <picker
    class="ab-select"
    :value="selectedIndex"
    :range="optionLabels"
    range-key="label"
    :disabled="disabled"
    @change="onChange"
  >
    <view class="ab-select__display" :class="{ 'ab-select__display--disabled': disabled }">
      <text class="ab-select__text" :class="{ 'ab-select__text--placeholder': !selectedLabel }">
        {{ selectedLabel || placeholder || '请选择' }}
      </text>
      <text class="ab-select__arrow">›</text>
    </view>
  </picker>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  options: Array<{ value: string; label: string }>
  placeholder?: string
  disabled?: boolean
}>(), {
  placeholder: '请选择',
  disabled: false,
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const selectedIndex = computed(() => {
  const idx = props.options.findIndex((o) => o.value === props.modelValue)
  return idx >= 0 ? idx : 0
})

const optionLabels = computed(() => props.options.map((o) => ({ label: o.label })))

const selectedLabel = computed(() => {
  const found = props.options.find((o) => o.value === props.modelValue)
  return found ? found.label : ''
})

function onChange(e: any) {
  const index = Number(e.detail.value)
  if (index >= 0 && index < props.options.length) {
    emit('update:modelValue', props.options[index].value)
  }
}
</script>

<style lang="scss" scoped>
.ab-select {
  width: 100%;

  &__display {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 80rpx;
    padding: $ab-space-sm $ab-space-md;
    background-color: $ab-surface;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    box-sizing: border-box;

    &--disabled {
      background-color: $ab-background;
      opacity: 0.6;
    }
  }

  &__text {
    font-size: $ab-text-base;
    color: $ab-text;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;

    &--placeholder {
      color: $ab-text-tertiary;
    }
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
    margin-left: $ab-space-xs;
    flex-shrink: 0;
  }
}
</style>
