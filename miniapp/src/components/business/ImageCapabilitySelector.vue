<template>
  <view class="capability-selector">
    <view v-if="loading" class="capability-selector__loading">正在加载图像能力...</view>
    <template v-else>
      <view v-if="retiredValue" class="capability-option capability-option--retired">
        <text class="capability-option__name">已停用图像能力（请重新选择）</text>
      </view>
      <view
        v-for="option in sortedOptions"
        :key="option.key"
        class="capability-option"
        :class="{
          'capability-option--active': modelValue === option.key,
          'capability-option--disabled': option.enabled !== true || option.price_available !== true,
        }"
        @tap="select(option.key)"
      >
        <view class="capability-option__header">
          <text class="capability-option__name">{{ option.display_name }}</text>
          <text v-if="option.min_tier && option.min_tier !== 'free'" class="capability-option__tier">
            {{ option.min_tier === 'enterprise' ? '企业版' : 'Pro 版' }}
          </text>
        </view>
        <text v-if="option.description" class="capability-option__description">{{ option.description }}</text>
        <text v-if="option.price_available && typeof option.price_credits === 'number'" class="capability-option__price">
          每张 {{ option.price_credits.toLocaleString() }} 积分
        </text>
        <text v-else class="capability-option__price">价格暂不可用</text>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ImageCapabilityOption } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: string
  options: ImageCapabilityOption[]
  loading?: boolean
}>(), { loading: false })

const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()

const sortedOptions = computed(() => [...props.options].sort((left, right) => {
  const leftOrder = left.sort_order || Number.MAX_SAFE_INTEGER
  const rightOrder = right.sort_order || Number.MAX_SAFE_INTEGER
  return leftOrder - rightOrder || left.display_name.localeCompare(right.display_name)
}))
const retiredValue = computed(() =>
  props.modelValue && !props.options.some((option) => option.key === props.modelValue),
)

function select(key: string) {
  const option = props.options.find((candidate) => candidate.key === key)
  if (!option || option.enabled !== true || option.price_available !== true) return
  emit('update:modelValue', key)
}
</script>

<style lang="scss" scoped>
.capability-selector { display: flex; flex-direction: column; gap: $ab-space-sm; }
.capability-selector__loading { color: $ab-text-tertiary; font-size: $ab-text-sm; }
.capability-option { padding: $ab-space-sm $ab-space-md; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; background: $ab-surface; }
.capability-option--active { border-color: $ab-primary; background: $ab-primary-bg; }
.capability-option--disabled { opacity: 0.55; }
.capability-option--retired { border-color: $ab-danger; background: $ab-danger-bg; }
.capability-option__header { display: flex; align-items: center; justify-content: space-between; gap: $ab-space-sm; }
.capability-option__name { color: $ab-text; font-size: $ab-text-base; font-weight: $ab-font-medium; }
.capability-option__tier { color: $ab-primary; font-size: $ab-text-xs; }
.capability-option__description, .capability-option__price { display: block; margin-top: 8rpx; color: $ab-text-secondary; font-size: $ab-text-sm; line-height: 1.45; }
.capability-option__price { color: $ab-text; font-weight: $ab-font-medium; }
</style>
