<template>
  <view class="ratio-control">
    <view class="ratio-control__trigger" @tap="open = true">
      <AbIcon name="image" size="28rpx" />
      <text class="ratio-control__value">{{ selectedLabel }}</text>
      <text class="ratio-control__arrow">›</text>
    </view>

    <view v-if="open" class="ratio-sheet__mask" @tap="open = false" />
    <view class="ratio-sheet" :class="{ 'ratio-sheet--open': open }">
      <view class="ratio-sheet__header">
        <text class="ratio-sheet__title">图片比例</text>
        <text class="ratio-sheet__close" @tap="open = false">关闭</text>
      </view>
      <view class="ratio-sheet__options">
        <view
          v-for="option in options"
          :key="option.value"
          class="ratio-sheet__option"
          :class="{ 'ratio-sheet__option--active': modelValue === option.value }"
          @tap="select(option.value)"
        >
          <text class="ratio-sheet__option-label">{{ option.label }}</text>
          <text class="ratio-sheet__option-hint">{{ option.hint }}</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import AbIcon from '@/components/common/AbIcon.vue'

const props = defineProps<{
  modelValue: string
  ratios: string[]
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()
const open = ref(false)

const options = computed(() => [
  { value: 'auto', label: '智能适配', hint: '按内容选择' },
  ...props.ratios.map((ratio) => ({ value: ratio, label: ratio, hint: ratioHint(ratio) })),
])
const selectedLabel = computed(() => props.modelValue === 'auto' ? '智能适配' : props.modelValue)

function ratioHint(ratio: string): string {
  const [width, height] = ratio.split(':').map(Number)
  if (width === height) return '方形'
  return width > height ? '横版' : '竖版'
}

function select(value: string) {
  emit('update:modelValue', value)
  open.value = false
}
</script>

<style lang="scss" scoped>
.ratio-control__trigger {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  min-height: 64rpx;
  gap: 10rpx;
  padding: 0 $ab-space-sm;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  background: $ab-surface;
}

.ratio-control__value { color: $ab-text; font-size: $ab-text-sm; }
.ratio-control__arrow { color: $ab-text-tertiary; font-size: $ab-text-lg; }

.ratio-sheet__mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgba(0, 0, 0, 0.42);
}

.ratio-sheet {
  position: fixed;
  right: 0;
  bottom: 0;
  left: 0;
  z-index: 81;
  padding: $ab-space-md $ab-space-md calc(#{$ab-space-lg} + env(safe-area-inset-bottom));
  border-radius: 16rpx 16rpx 0 0;
  background: $ab-surface;
  transform: translateY(110%);
  transition: transform 180ms ease;
}

.ratio-sheet--open { transform: translateY(0); }
.ratio-sheet__header { display: flex; align-items: center; justify-content: space-between; margin-bottom: $ab-space-md; }
.ratio-sheet__title { color: $ab-text; font-size: $ab-text-lg; font-weight: $ab-font-semibold; }
.ratio-sheet__close { color: $ab-primary; font-size: $ab-text-sm; }
.ratio-sheet__options { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: $ab-space-sm; }
.ratio-sheet__option { min-width: 0; padding: $ab-space-sm; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; text-align: center; }
.ratio-sheet__option--active { border-color: $ab-primary; background: $ab-primary-bg; }
.ratio-sheet__option-label, .ratio-sheet__option-hint { display: block; }
.ratio-sheet__option-label { color: $ab-text; font-size: $ab-text-sm; font-weight: $ab-font-medium; }
.ratio-sheet__option-hint { margin-top: 4rpx; color: $ab-text-tertiary; font-size: $ab-text-xs; }
</style>
