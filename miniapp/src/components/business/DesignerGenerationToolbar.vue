<template>
  <view class="designer-toolbar">
    <view class="designer-toolbar__trigger" @tap="open = true">
      <AbIcon name="image" size="28rpx" />
      <text class="designer-toolbar__summary">{{ summary }}</text>
      <text class="designer-toolbar__arrow">›</text>
    </view>

    <view v-if="open" class="designer-sheet__mask" @tap="open = false" />
    <view class="designer-sheet" :class="{ 'designer-sheet--open': open }">
      <view class="designer-sheet__header">
        <text class="designer-sheet__title">生成设置</text>
        <text class="designer-sheet__close" @tap="open = false">完成</text>
      </view>
      <scroll-view scroll-y class="designer-sheet__body">
        <text class="designer-sheet__label">图像能力</text>
        <view
          v-for="capability in capabilities"
          :key="capability.id"
          class="designer-sheet__capability"
          :class="{ 'designer-sheet__capability--active': capabilityKey === capability.id }"
          @tap="selectCapability(capability.id)"
        >
          <text class="designer-sheet__capability-name">{{ capability.name }}</text>
          <text v-if="capability.description" class="designer-sheet__hint">{{ capability.description }}</text>
          <text class="designer-sheet__price">每张 {{ capability.credits.toLocaleString() }} 积分</text>
        </view>

        <template v-if="selectedCapability">
          <text class="designer-sheet__label designer-sheet__label--spaced">固定规格</text>
          <view class="designer-sheet__choices">
            <view
              v-for="preset in selectedCapability.designerFeatures.sizePresets"
              :key="preset"
              class="designer-sheet__choice"
              :class="{ 'designer-sheet__choice--active': settings.size === preset }"
              @tap="patchSettings({ size: preset })"
            >
              <text>{{ presetLabel(preset) }}</text>
            </view>
          </view>

          <template v-if="selectedCapability.designerFeatures.qualityLevels.length">
            <text class="designer-sheet__label designer-sheet__label--spaced">质量</text>
            <view class="designer-sheet__choices">
              <view
                v-for="quality in selectedCapability.designerFeatures.qualityLevels"
                :key="quality"
                class="designer-sheet__choice"
                :class="{ 'designer-sheet__choice--active': settings.quality === quality }"
                @tap="patchSettings({ quality })"
              >
                <text>{{ qualityLabel(quality) }}</text>
              </view>
            </view>
          </template>

          <text class="designer-sheet__label designer-sheet__label--spaced">格式</text>
          <view class="designer-sheet__choices">
            <view
              v-for="format in selectedCapability.designerFeatures.outputFormats"
              :key="format"
              class="designer-sheet__choice"
              :class="{ 'designer-sheet__choice--active': settings.outputFormat === format }"
              @tap="patchSettings({ outputFormat: format })"
            >
              <text>{{ format.toUpperCase() }}</text>
            </view>
          </view>

          <template v-if="selectedCapability.designerFeatures.maxBatch > 1">
            <text class="designer-sheet__label designer-sheet__label--spaced">图片数量</text>
            <view class="designer-sheet__choices">
              <view
                v-for="count in selectedCapability.designerFeatures.maxBatch"
                :key="count"
                class="designer-sheet__choice"
                :class="{ 'designer-sheet__choice--active': settings.n === count }"
                @tap="patchSettings({ n: count })"
              >
                <text>{{ count }}</text>
              </view>
            </view>
          </template>
        </template>
      </scroll-view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { DesignerCapability, DesignerSettings } from '@/types'
import AbIcon from '@/components/common/AbIcon.vue'

const props = defineProps<{
  capabilities: DesignerCapability[]
  capabilityKey: string
  settings: DesignerSettings
}>()
const emit = defineEmits<{
  (event: 'update:capabilityKey', value: string): void
  (event: 'update:settings', value: DesignerSettings): void
}>()
const open = ref(false)

const selectedCapability = computed(() => props.capabilities.find((item) => item.id === props.capabilityKey))
const summary = computed(() => [
  selectedCapability.value?.name || '图像能力',
  presetLabel(props.settings.size),
  props.settings.quality ? qualityLabel(props.settings.quality) : '',
].filter(Boolean).join(' · '))

function presetLabel(preset: string): string {
  const parts = preset.split(':')
  if (parts.length === 3) return `${parts[0]}:${parts[1]} · ${parts[2].toUpperCase()}`
  return preset
}

function qualityLabel(quality: string): string {
  return ({ auto: '自动', low: '低', medium: '中', high: '高' } as Record<string, string>)[quality] || quality
}

function selectCapability(value: string) {
  emit('update:capabilityKey', value)
}

function patchSettings(patch: Partial<DesignerSettings>) {
  emit('update:settings', { ...props.settings, ...patch })
}
</script>

<style lang="scss" scoped>
.designer-toolbar { margin-top: $ab-space-xs; }
.designer-toolbar__trigger { display: inline-flex; align-items: center; max-width: 100%; min-height: 60rpx; gap: 10rpx; padding: 0 $ab-space-sm; border-radius: $ab-radius-sm; color: $ab-text-secondary; }
.designer-toolbar__summary { min-width: 0; overflow: hidden; color: $ab-text-secondary; font-size: $ab-text-sm; text-overflow: ellipsis; white-space: nowrap; }
.designer-toolbar__arrow { color: $ab-text-tertiary; font-size: $ab-text-lg; }
.designer-sheet__mask { position: fixed; inset: 0; z-index: 80; background: rgba(0, 0, 0, 0.42); }
.designer-sheet { position: fixed; right: 0; bottom: 0; left: 0; z-index: 81; padding: $ab-space-md $ab-space-md calc(#{$ab-space-md} + env(safe-area-inset-bottom)); border-radius: 16rpx 16rpx 0 0; background: $ab-surface; transform: translateY(110%); transition: transform 180ms ease; }
.designer-sheet--open { transform: translateY(0); }
.designer-sheet__header { display: flex; align-items: center; justify-content: space-between; margin-bottom: $ab-space-md; }
.designer-sheet__title { color: $ab-text; font-size: $ab-text-lg; font-weight: $ab-font-semibold; }
.designer-sheet__close { color: $ab-primary; font-size: $ab-text-sm; }
.designer-sheet__body { max-height: 70vh; }
.designer-sheet__label { display: block; margin-bottom: $ab-space-sm; color: $ab-text; font-size: $ab-text-sm; font-weight: $ab-font-medium; }
.designer-sheet__label--spaced { margin-top: $ab-space-lg; }
.designer-sheet__capability { margin-bottom: $ab-space-sm; padding: $ab-space-sm $ab-space-md; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; }
.designer-sheet__capability--active { border-color: $ab-primary; background: $ab-primary-bg; }
.designer-sheet__capability-name, .designer-sheet__hint, .designer-sheet__price { display: block; }
.designer-sheet__capability-name { color: $ab-text; font-size: $ab-text-base; font-weight: $ab-font-medium; }
.designer-sheet__hint { margin-top: 6rpx; color: $ab-text-secondary; font-size: $ab-text-sm; line-height: 1.4; }
.designer-sheet__price { margin-top: 6rpx; color: $ab-text; font-size: $ab-text-sm; font-weight: $ab-font-medium; }
.designer-sheet__choices { display: flex; flex-wrap: wrap; gap: $ab-space-sm; }
.designer-sheet__choice { min-width: 112rpx; padding: 14rpx $ab-space-sm; border: 2rpx solid $ab-border; border-radius: $ab-radius-sm; color: $ab-text-secondary; font-size: $ab-text-sm; text-align: center; }
.designer-sheet__choice--active { border-color: $ab-primary; background: $ab-primary-bg; color: $ab-primary; }
</style>
