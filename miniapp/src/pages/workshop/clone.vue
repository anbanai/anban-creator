<template>
  <view class="page clone-page">
    <!-- Source selection -->
    <view class="cp-section">
      <text class="field-label">内容来源</text>
      <view class="cp-source-tabs">
        <view
          class="cp-source-tab"
          :class="{ 'cp-source-tab--active': sourceType === 'url' }"
          @tap="sourceType = 'url'"
        >
          <text class="cp-source-tab__icon">🔗</text>
          <text class="cp-source-tab__text">粘贴链接</text>
        </view>
        <view
          class="cp-source-tab"
          :class="{ 'cp-source-tab--active': sourceType === 'template' }"
          @tap="goToTemplates"
        >
          <text class="cp-source-tab__icon">📑</text>
          <text class="cp-source-tab__text">从模板选</text>
        </view>
      </view>
    </view>

    <!-- URL input (shown when sourceType === 'url') -->
    <view v-if="sourceType === 'url'" class="cp-section">
      <text class="field-label">粘贴种草笔记链接</text>
      <AbInput
        v-model="sourceUrl"
        placeholder="https://xhslink.com/..."
        :error="errors.sourceUrl"
      />
    </view>

    <!-- Selected template indicator (shown when sourceType === 'template') -->
    <view v-if="sourceType === 'template' && selectedTemplate" class="cp-section">
      <view class="cp-selected-template">
        <image
          :src="selectedTemplate.thumbnail_url"
          class="cp-selected-template__thumb"
          mode="aspectFill"
        />
        <view class="cp-selected-template__info">
          <text class="cp-selected-template__name">{{ selectedTemplate.name }}</text>
          <text class="cp-selected-template__change" @tap="goToTemplates">更换模板</text>
        </view>
      </view>
    </view>
    <view v-if="sourceType === 'template' && !selectedTemplate" class="cp-section">
      <view class="cp-no-template" @tap="goToTemplates">
        <text class="cp-no-template__text">请先选择一个模板</text>
        <text class="cp-no-template__arrow">›</text>
      </view>
    </view>

    <!-- Clone depth selection -->
    <view class="cp-section">
      <text class="field-label">复刻深度</text>
      <view class="cp-depth-list">
        <view
          v-for="option in depthOptions"
          :key="option.value"
          class="cp-depth-card"
          :class="{ 'cp-depth-card--active': cloneDepth === option.value }"
          @tap="cloneDepth = option.value"
        >
          <view class="cp-depth-card__header">
            <text class="cp-depth-card__icon">{{ option.icon }}</text>
            <text class="cp-depth-card__title">{{ option.label }}</text>
            <view v-if="option.recommended" class="cp-depth-card__badge">
              <AbBadge variant="warning" size="sm">推荐</AbBadge>
            </view>
          </view>
          <text class="cp-depth-card__desc">{{ option.description }}</text>
        </view>
      </view>
    </view>

    <!-- Target account selector -->
    <view class="cp-section">
      <text class="field-label">目标账号 <text class="field-required">*</text></text>
      <ChannelSelector
        v-model="form.channel_id"
        placeholder="选择要发布的账号..."
        @change="onChannelChange"
      />
      <text v-if="errors.channel" class="field-error">{{ errors.channel }}</text>
    </view>

    <!-- Additional prompt (optional) -->
    <view class="cp-section">
      <text class="field-label">额外要求 <text class="field-optional">（可选）</text></text>
      <AbTextarea
        v-model="form.extra_prompt"
        placeholder="补充你想要的风格、关键词或特殊要求..."
        :rows="3"
        :maxlength="500"
      />
    </view>

    <!-- Fixed bottom button -->
    <view class="cp-bottom">
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="submitting"
        :disabled="!canSubmit"
        @click="onClone"
      >
        开始复刻
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import type { Channel, Template } from '@/types'
import { tasksApi } from '@/api/tasks'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import ChannelSelector from '@/components/business/ChannelSelector.vue'

const depthOptions = [
  {
    value: 'style',
    icon: '🎨',
    label: '风格复刻',
    description: '学习写作风格和排版',
    recommended: false,
  },
  {
    value: 'medium',
    icon: '📝',
    label: '中度复刻',
    description: '风格+结构+关键词',
    recommended: true,
  },
  {
    value: 'deep',
    icon: '🔄',
    label: '深度复刻',
    description: '全面学习并创新',
    recommended: false,
  },
]

const sourceType = ref<'url' | 'template'>('url')
const sourceUrl = ref('')
const selectedTemplate = ref<Template | null>(null)
const cloneDepth = ref('medium')
const submitting = ref(false)
const selectedChannel = ref<Channel | null>(null)

const form = reactive({
  channel_id: '',
  extra_prompt: '',
})

const errors = reactive<Record<string, string>>({})

const canSubmit = computed(() => {
  const hasSource = sourceType.value === 'url'
    ? !!sourceUrl.value.trim()
    : !!selectedTemplate.value
  return hasSource && !!form.channel_id && !submitting.value
})

function goToTemplates() {
  // Navigate to templates page with return flag
  uni.navigateTo({
    url: '/pages/templates/index?mode=clone',
  })
}

function onChannelChange(channel: Channel) {
  selectedChannel.value = channel
  delete errors.channel
}

function validate(): boolean {
  Object.keys(errors).forEach(k => delete errors[k])

  if (sourceType.value === 'url' && !sourceUrl.value.trim()) {
    errors.sourceUrl = '请输入种草笔记链接'
    return false
  }

  if (sourceType.value === 'template' && !selectedTemplate.value) {
    uni.showToast({ title: '请先选择模板', icon: 'none' })
    return false
  }

  if (!form.channel_id) {
    errors.channel = '请选择目标账号'
    return false
  }

  return true
}

async function onClone() {
  if (!validate()) return
  if (!selectedChannel.value) return

  submitting.value = true
  try {
    // Build the clone prompt from depth + source info
    const depthLabel = depthOptions.find(d => d.value === cloneDepth.value)?.label || '中度复刻'
    const depthDesc = depthOptions.find(d => d.value === cloneDepth.value)?.description || ''

    let prompt = `【爆款复刻任务】\n复刻深度: ${depthLabel}（${depthDesc}）\n`

    if (sourceType.value === 'url' && sourceUrl.value.trim()) {
      prompt += `来源链接: ${sourceUrl.value.trim()}\n`
    } else if (selectedTemplate.value) {
      prompt += `来源模板: ${selectedTemplate.value.name}\n`
    }

    if (form.extra_prompt.trim()) {
      prompt += `额外要求: ${form.extra_prompt.trim()}\n`
    }

    prompt += `\n请参考来源内容的风格和结构，为目标账号创作新的内容。`

    const task = await tasksApi.create({
      type: selectedChannel.value.platform as any,
      channel_id: form.channel_id,
      prompt: prompt.trim(),
    })

    uni.showToast({ title: '复刻任务已创建', icon: 'success' })
    setTimeout(() => {
      uni.redirectTo({ url: `/pages/tasks/detail?id=${task.id}` })
    }, 500)
  } catch (err: any) {
    const msg = err?.message || '创建失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  // Check if returning from template selection with a selected template
  const eventChannel = (uni as typeof uni & {
    getOpenerEventChannel?: () => { on: (event: string, callback: (data: { template: Template }) => void) => void }
  }).getOpenerEventChannel?.()
  if (eventChannel) {
    eventChannel.on('selectTemplate', (data: { template: Template }) => {
      if (data?.template) {
        selectedTemplate.value = data.template
        sourceType.value = 'template'
      }
    })
  }
})
</script>

<style lang="scss" scoped>
.clone-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 180rpx;
}

.field-label {
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
  display: block;
  margin-bottom: $ab-space-xs;
}

.field-required {
  color: $ab-danger;
}

.field-optional {
  color: $ab-text-tertiary;
  font-weight: $ab-font-normal;
}

.field-error {
  font-size: $ab-text-xs;
  color: $ab-danger;
  display: block;
  margin-top: $ab-space-xs;
  line-height: 1.4;
}

.cp-section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

// Source tabs
.cp-source-tabs {
  display: flex;
  gap: $ab-space-sm;
}

.cp-source-tab {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8rpx;
  padding: $ab-space-md $ab-space-sm;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__icon {
    font-size: 48rpx;
    line-height: 1;
  }

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    font-weight: $ab-font-medium;
  }

  &--active &__text {
    color: $ab-primary;
  }
}

// Selected template
.cp-selected-template {
  display: flex;
  gap: $ab-space-sm;
  align-items: center;

  &__thumb {
    width: 96rpx;
    height: 128rpx;
    border-radius: $ab-radius-sm;
    flex-shrink: 0;
    background-color: $ab-divider;
  }

  &__info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8rpx;
  }

  &__name {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__change {
    font-size: $ab-text-sm;
    color: $ab-primary;
  }
}

.cp-no-template {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-background;
  border-radius: $ab-radius-sm;
  cursor: pointer;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}

// Depth cards
.cp-depth-list {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
}

.cp-depth-card {
  padding: $ab-space-md;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__header {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: 8rpx;
  }

  &__icon {
    font-size: 40rpx;
    line-height: 1;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    flex: 1;
  }

  &__badge {
    flex-shrink: 0;
  }

  &__desc {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.4;
    padding-left: 56rpx;
  }
}

// Bottom
.cp-bottom {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-md $ab-space-lg;
  padding-bottom: calc(#{$ab-space-md} + env(safe-area-inset-bottom));
  background-color: $ab-surface;
  box-shadow: 0 -4rpx 12rpx rgba(0, 0, 0, 0.06);
  z-index: 20;
}
</style>
