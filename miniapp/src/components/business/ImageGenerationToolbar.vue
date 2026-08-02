<template>
  <view class="generation-toolbar">
    <view class="generation-toolbar__trigger" @tap="open = true">
      <AbIcon name="image" size="28rpx" />
      <text class="generation-toolbar__summary">{{ summary }}</text>
      <text class="generation-toolbar__arrow">›</text>
    </view>

    <view v-if="open" class="generation-sheet__mask" @tap="open = false" />
    <view class="generation-sheet" :class="{ 'generation-sheet--open': open }">
      <view class="generation-sheet__header">
        <text class="generation-sheet__title">图像设置</text>
        <text class="generation-sheet__close" @tap="open = false">完成</text>
      </view>
      <scroll-view scroll-y class="generation-sheet__body">
        <text class="generation-sheet__label">图片比例</text>
        <view class="generation-sheet__ratios">
          <view
            v-for="value in ratioOptions"
            :key="value"
            class="generation-sheet__ratio"
            :class="{ 'generation-sheet__ratio--active': ratio === value }"
            @tap="selectRatio(value)"
          >
            <text>{{ value === 'auto' ? '智能适配' : value }}</text>
          </view>
        </view>

        <text class="generation-sheet__label generation-sheet__label--spaced">图像能力</text>
        <view
          v-for="option in sortedCapabilities"
          :key="option.key"
          class="generation-sheet__capability"
          :class="{
            'generation-sheet__capability--active': capabilityKey === option.key,
            'generation-sheet__capability--disabled': !available(option),
          }"
          @tap="selectCapability(option)"
        >
          <view class="generation-sheet__capability-head">
            <text class="generation-sheet__capability-name">{{ option.display_name }}</text>
            <text v-if="option.min_tier && option.min_tier !== 'free'" class="generation-sheet__tier">
              {{ option.min_tier === 'enterprise' ? '企业版' : 'Pro 版' }}
            </text>
          </view>
          <text v-if="option.description" class="generation-sheet__description">{{ option.description }}</text>
          <text v-if="available(option) && typeof option.price_credits === 'number'" class="generation-sheet__price">
            每张 {{ option.price_credits.toLocaleString() }} 积分
          </text>
          <text v-else class="generation-sheet__price">价格暂不可用</text>
        </view>
      </scroll-view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ImageCapabilityOption } from '@/types'
import AbIcon from '@/components/common/AbIcon.vue'

const props = withDefaults(defineProps<{
  ratio: string
  ratios: string[]
  capabilityKey: string
  capabilities: ImageCapabilityOption[]
  loading?: boolean
}>(), { loading: false })

const emit = defineEmits<{
  (event: 'update:ratio', value: string): void
  (event: 'update:capabilityKey', value: string): void
}>()
const open = ref(false)

const ratioOptions = computed(() => ['auto', ...props.ratios])
const sortedCapabilities = computed(() => [...props.capabilities].sort((left, right) =>
  (left.sort_order ?? Number.MAX_SAFE_INTEGER) - (right.sort_order ?? Number.MAX_SAFE_INTEGER),
))
const selectedCapability = computed(() => props.capabilities.find((item) => item.key === props.capabilityKey))
const summary = computed(() => {
  if (props.loading) return '加载图像设置...'
  const ratio = props.ratio === 'auto' ? '智能适配' : props.ratio
  return `${ratio} · ${selectedCapability.value?.display_name || '图像能力'}`
})

function available(option: ImageCapabilityOption): boolean {
  return option.enabled === true && option.price_available === true
}

function selectRatio(value: string) {
  emit('update:ratio', value)
}

function selectCapability(option: ImageCapabilityOption) {
  if (available(option)) emit('update:capabilityKey', option.key)
}
</script>

<style lang="scss" scoped>
.generation-toolbar { margin-top: $ab-space-xs; }
.generation-toolbar__trigger { display: inline-flex; align-items: center; max-width: 100%; min-height: 60rpx; gap: 10rpx; padding: 0 $ab-space-sm; border-radius: $ab-radius-sm; color: $ab-text-secondary; }
.generation-toolbar__summary { min-width: 0; overflow: hidden; color: $ab-text-secondary; font-size: $ab-text-sm; text-overflow: ellipsis; white-space: nowrap; }
.generation-toolbar__arrow { color: $ab-text-tertiary; font-size: $ab-text-lg; }
.generation-sheet__mask { position: fixed; inset: 0; z-index: 80; background: rgba(0, 0, 0, 0.42); }
.generation-sheet { position: fixed; right: 0; bottom: 0; left: 0; z-index: 81; padding: $ab-space-md $ab-space-md calc(#{$ab-space-md} + env(safe-area-inset-bottom)); border-radius: 16rpx 16rpx 0 0; background: $ab-surface; transform: translateY(110%); transition: transform 180ms ease; }
.generation-sheet--open { transform: translateY(0); }
.generation-sheet__header { display: flex; align-items: center; justify-content: space-between; margin-bottom: $ab-space-md; }
.generation-sheet__title { color: $ab-text; font-size: $ab-text-lg; font-weight: $ab-font-semibold; }
.generation-sheet__close { color: $ab-primary; font-size: $ab-text-sm; }
.generation-sheet__body { max-height: 68vh; }
.generation-sheet__label { display: block; margin-bottom: $ab-space-sm; color: $ab-text; font-size: $ab-text-sm; font-weight: $ab-font-medium; }
.generation-sheet__label--spaced { margin-top: $ab-space-lg; }
.generation-sheet__ratios { display: flex; flex-wrap: wrap; gap: $ab-space-sm; }
.generation-sheet__ratio { min-width: 116rpx; padding: 14rpx $ab-space-sm; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; color: $ab-text-secondary; font-size: $ab-text-sm; text-align: center; }
.generation-sheet__ratio--active { border-color: $ab-primary; background: $ab-primary-bg; color: $ab-primary; }
.generation-sheet__capability { margin-bottom: $ab-space-sm; padding: $ab-space-sm $ab-space-md; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; }
.generation-sheet__capability--active { border-color: $ab-primary; background: $ab-primary-bg; }
.generation-sheet__capability--disabled { opacity: .5; }
.generation-sheet__capability-head { display: flex; align-items: center; justify-content: space-between; gap: $ab-space-sm; }
.generation-sheet__capability-name { color: $ab-text; font-size: $ab-text-base; font-weight: $ab-font-medium; }
.generation-sheet__tier { color: $ab-primary; font-size: $ab-text-xs; }
.generation-sheet__description, .generation-sheet__price { display: block; margin-top: 6rpx; color: $ab-text-secondary; font-size: $ab-text-sm; line-height: 1.45; }
.generation-sheet__price { color: $ab-text; font-weight: $ab-font-medium; }
</style>
