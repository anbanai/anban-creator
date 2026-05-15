<template>
  <view class="page task-create">
    <!-- Channel selector -->
    <view class="task-create__section">
      <text class="field-label">选择账号 <text class="field-required">*</text></text>
      <ChannelSelector
        v-model="form.channel_id"
        placeholder="搜索账号..."
        @change="onChannelChange"
      />
      <text v-if="errors.channel" class="field-error">{{ errors.channel }}</text>
    </view>

    <!-- Content type (locked from channel) -->
    <view class="task-create__section" v-if="selectedChannel">
      <text class="field-label">内容类型</text>
      <view class="locked-field">
        <PlatformAvatar :platform="selectedChannel.platform" :size="32" />
        <text class="locked-field__text">{{ contentTypeLabel[selectedChannel.platform] || '未知' }}</text>
        <text class="locked-field__hint">（跟随账号平台）</text>
      </view>
    </view>

    <!-- Prompt -->
    <view class="task-create__section">
      <text class="field-label">创作要求 <text class="field-required">*</text></text>
      <AbTextarea
        v-model="form.prompt"
        placeholder="描述你想要的内容，&#10;如主题、风格、关键词..."
        :rows="4"
        :maxlength="2000"
        :error="errors.prompt"
      />
      <!-- Inspiration hint -->
      <view class="inspiration-hint" @tap="applyInspiration">
        <text class="inspiration-hint__icon">💡</text>
        <text class="inspiration-hint__text">
          试试: {{ currentInspiration }}
        </text>
      </view>
    </view>

    <!-- Quantity selector -->
    <view class="task-create__section">
      <text class="field-label">生成数量</text>
      <view class="quantity-group">
        <view
          v-for="q in TASK_QUANTITIES"
          :key="q"
          class="quantity-btn"
          :class="{ 'quantity-btn--active': form.quantity === q }"
          @tap="form.quantity = q"
        >
          <text>{{ q }}</text>
        </view>
      </view>
    </view>

    <!-- Image ratio selector -->
    <view class="task-create__section">
      <text class="field-label">图片比例</text>
      <view class="ratio-group">
        <view
          v-for="ratio in IMAGE_RATIOS"
          :key="ratio.value"
          class="ratio-btn"
          :class="{ 'ratio-btn--active': form.image_ratio === ratio.value }"
          @tap="form.image_ratio = ratio.value"
        >
          <text>{{ ratio.label }}</text>
        </view>
      </view>
    </view>

    <!-- Advanced options (collapsible) -->
    <view class="task-create__section">
      <view class="collapsible-header" @tap="showAdvanced = !showAdvanced">
        <text class="field-label" style="margin-bottom: 0;">高级选项</text>
        <text class="collapsible-arrow">{{ showAdvanced ? '收起' : '展开' }}</text>
      </view>
      <view v-if="showAdvanced" class="advanced-options">
        <!-- Generate video (seednote/xls only) -->
        <view v-if="isImagePlatform" class="switch-row">
          <text class="field-label" style="margin-bottom: 0;">生成视频</text>
          <AbSwitch v-model="form.generate_video" />
        </view>
      </view>
    </view>

    <!-- Credit info -->
    <view class="task-create__section">
      <view class="credit-info">
        <view class="credit-info__row">
          <text class="credit-info__label">预计消耗</text>
          <text class="credit-info__value credit-info__value--cost">
            约 {{ estimatedCost }} 积分
          </text>
        </view>
        <view class="credit-info__row">
          <text class="credit-info__label">当前余额</text>
          <text class="credit-info__value" :class="{ 'credit-info__value--low': balance < estimatedCost }">
            {{ balance.toLocaleString() }} 积分
          </text>
        </view>
      </view>
      <text v-if="balance > 0 && balance < estimatedCost" class="field-error">
        积分不足，请先充值
      </text>
    </view>

    <!-- Fixed bottom button -->
    <view class="task-create__bottom">
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="submitting"
        :disabled="!canSubmit"
        @click="onSubmit"
      >
        开始创作
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import type { Channel, CreditPricing } from '@/types'
import { tasksApi } from '@/api/tasks'
import { creditsApi } from '@/api/credits'
import { TASK_QUANTITIES, IMAGE_RATIOS } from '@/utils/constants'
import { contentTypeLabel } from '@/utils/labels'
import AbButton from '@/components/common/AbButton.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import ChannelSelector from '@/components/business/ChannelSelector.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

const inspirations = [
  '写一篇关于春季护肤的笔记，风格温暖自然',
  '分享5个提升效率的办公好物，搭配实拍图',
  '做一期测评：对比3款热门面霜的真实体验',
  '分享一周穿搭灵感，适合通勤和约会',
]

const selectedChannel = ref<Channel | null>(null)
const balance = ref(0)
const pricing = ref<CreditPricing | null>(null)
const submitting = ref(false)
const showAdvanced = ref(false)
const inspirationIndex = ref(0)

const form = reactive({
  channel_id: '',
  prompt: '',
  quantity: 1,
  image_ratio: '3:4',
  generate_video: false,
})

const errors = reactive<Record<string, string>>({})

const currentInspiration = computed(() => inspirations[inspirationIndex.value % inspirations.length])

const isImagePlatform = computed(() => {
  if (!selectedChannel.value) return false
  return ['seednote', 'xls'].includes(selectedChannel.value.platform)
})

const estimatedCost = computed(() => {
  if (!pricing.value?.task_costs) return 0
  if (!selectedChannel.value) return 0
  const costPerTask = pricing.value.task_costs[selectedChannel.value.platform] || 500
  return costPerTask * form.quantity
})

const canSubmit = computed(() => {
  return !!form.channel_id && !!form.prompt.trim() && balance.value >= estimatedCost.value && !submitting.value
})

function onChannelChange(channel: Channel) {
  selectedChannel.value = channel

  // Set image ratio to channel default
  if (channel.image_ratio) {
    form.image_ratio = channel.image_ratio
  } else {
    const defaults: Record<string, string> = {
      article: '16:9',
      seednote: '3:4',
      xls: '3:4',
    }
    form.image_ratio = defaults[channel.platform] || '3:4'
  }

  // Reset video for non-image platforms
  if (!isImagePlatform.value) {
    form.generate_video = false
  }

  // Clear error
  delete errors.channel
}

function applyInspiration() {
  form.prompt = currentInspiration.value
  inspirationIndex.value = (inspirationIndex.value + 1) % inspirations.length
  delete errors.prompt
}

function validate(): boolean {
  Object.keys(errors).forEach((k) => delete errors[k])

  if (!form.channel_id) {
    errors.channel = '请选择账号'
    return false
  }

  if (!form.prompt.trim()) {
    errors.prompt = '请输入创作要求'
    return false
  }

  if (balance.value < estimatedCost.value) {
    uni.showToast({ title: '积分不足，请先充值', icon: 'none' })
    return false
  }

  return true
}

async function onSubmit() {
  if (!validate()) return

  if (!selectedChannel.value) return

  submitting.value = true
  try {
    const task = await tasksApi.create({
      type: selectedChannel.value.platform as any,
      channel_id: form.channel_id,
      prompt: form.prompt.trim(),
      quantity: form.quantity,
      image_ratio: form.image_ratio,
      generate_video: form.generate_video || undefined,
    })

    uni.showToast({ title: '任务已创建', icon: 'success' })
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

async function loadBalance() {
  try {
    const res = await creditsApi.balance()
    balance.value = res.balance ?? 0
  } catch {
    balance.value = 0
  }
}

async function loadPricing() {
  try {
    pricing.value = await creditsApi.pricing()
  } catch {
    // Pricing is non-critical
  }
}

onMounted(() => {
  loadBalance()
  loadPricing()
})
</script>

<style lang="scss" scoped>
.task-create {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 180rpx;

  &__section {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    margin-bottom: $ab-space-sm;
    box-shadow: $ab-shadow-sm;
  }

  &__bottom {
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

.field-error {
  font-size: $ab-text-xs;
  color: $ab-danger;
  display: block;
  margin-top: $ab-space-xs;
  line-height: 1.4;
}

.locked-field {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-background;
  border-radius: $ab-radius-sm;

  &__text {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Inspiration hint
.inspiration-hint {
  display: flex;
  align-items: center;
  gap: $ab-space-xs;
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-primary-bg;
  border-radius: $ab-radius-sm;
  margin-top: $ab-space-xs;
  cursor: pointer;

  &__icon {
    font-size: $ab-text-base;
  }

  &__text {
    font-size: $ab-text-xs;
    color: $ab-primary;
    line-height: 1.4;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
}

// Quantity / ratio button groups
.quantity-group,
.ratio-group {
  display: flex;
  gap: $ab-space-sm;
  flex-wrap: wrap;
}

.quantity-btn,
.ratio-btn {
  min-width: 80rpx;
  height: 72rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 $ab-space-lg;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  font-size: $ab-text-base;
  color: $ab-text-secondary;
  background-color: $ab-surface;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    color: $ab-primary;
    background-color: $ab-primary-bg;
    font-weight: $ab-font-medium;
  }
}

// Collapsible
.collapsible-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
}

.collapsible-arrow {
  font-size: $ab-text-sm;
  color: $ab-text-tertiary;
}

.advanced-options {
  margin-top: $ab-space-md;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

// Credit info
.credit-info {
  border-top: 2rpx solid $ab-divider;
  padding-top: $ab-space-sm;

  &__row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: $ab-space-xs 0;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__value {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;

    &--cost {
      color: $ab-warning;
    }

    &--low {
      color: $ab-danger;
    }
  }
}
</style>
