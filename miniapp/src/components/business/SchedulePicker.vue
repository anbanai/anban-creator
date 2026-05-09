<template>
  <view class="schedule-picker">
    <view
      v-for="preset in presets"
      :key="preset.label"
      :class="['schedule-picker__option', { active: modelValue === preset.cronExpr }]"
      @tap="select(preset.cronExpr)"
    >
      <text class="schedule-picker__label">{{ preset.label }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { SCHEDULE_PRESETS } from '@/utils/constants'

defineProps<{
  modelValue?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const presets = SCHEDULE_PRESETS

function select(cronExpr: string) {
  emit('update:modelValue', cronExpr)
}
</script>

<style lang="scss" scoped>
.schedule-picker {
  display: flex;
  flex-wrap: wrap;
  gap: $ab-space-sm;

  &__option {
    padding: $ab-space-xs $ab-space-md;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    background-color: $ab-surface;

    &.active {
      border-color: $ab-primary;
      background-color: $ab-primary-bg;
    }
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;
  }

  .active &__label {
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}
</style>
