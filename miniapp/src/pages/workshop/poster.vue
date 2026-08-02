<template>
  <view class="page poster-page">
    <!-- Form fields -->
    <view class="pp-section">
      <text class="field-label">标题 <text class="field-required">*</text></text>
      <AbInput
        v-model="form.title"
        placeholder="输入海报标题"
        :error="errors.title"
      />
    </view>

    <!-- Selling points -->
    <view class="pp-section">
      <text class="field-label">卖点</text>
      <view class="pp-selling-points">
        <view v-for="(point, index) in form.selling_points" :key="index" class="pp-selling-point">
          <AbInput
            :model-value="point"
            placeholder="输入卖点"
            @update:model-value="updateSellingPoint(index, $event)"
          />
          <view class="pp-selling-point__remove" @tap="removeSellingPoint(index)">
            <text class="pp-selling-point__remove-icon">×</text>
          </view>
        </view>
        <view v-if="form.selling_points.length < 5" class="pp-add-point" @tap="addSellingPoint">
          <text class="pp-add-point__icon">+</text>
          <text class="pp-add-point__text">添加卖点</text>
        </view>
      </view>
    </view>

    <!-- Brand, price, style -->
    <view class="pp-section">
      <view class="pp-field-row">
        <view class="pp-field-row__item">
          <text class="field-label">品牌</text>
          <AbInput
            v-model="form.brand"
            placeholder="输入品牌名"
          />
        </view>
        <view class="pp-field-row__item">
          <text class="field-label">价格</text>
          <AbInput
            v-model="form.price"
            type="number"
            placeholder="输入价格"
          />
        </view>
      </view>

      <text class="field-label" style="margin-top: $ab-space-sm;">风格偏好</text>
      <AbSelect
        v-model="form.style_preference"
        :options="styleOptions"
        placeholder="选择风格"
      />
    </view>

    <!-- Generated images -->
    <view v-if="generatedImages.length > 0" class="pp-section">
      <text class="section-title">生成结果</text>
      <view class="pp-image-grid">
        <view
          v-for="(img, i) in generatedImages"
          :key="i"
          class="pp-image-item"
          @tap="previewImage(i)"
        >
          <image :src="img.url" class="pp-image-item__img" mode="aspectFill" />
        </view>
      </view>
      <view class="pp-result-actions">
        <AbButton type="primary" size="sm" :loading="submitting" @click="regenerateVariant">继续变体</AbButton>
        <AbButton type="ghost" size="sm" @click="copyPosterPrompt">复制 Prompt</AbButton>
      </view>
    </view>

    <!-- Generating indicator -->
    <view v-if="isGenerating" class="pp-generating">
      <AbLoading size="sm" text="正在生成海报..." />
    </view>

    <!-- Fixed bottom button -->
    <view class="pp-bottom">
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="submitting"
        :disabled="!canSubmit"
        @click="onGenerate"
      >
        生成海报
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onUnmounted } from 'vue'
import type { PosterTask, PosterImage } from '@/types'
import { postersApi } from '@/api/posters'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSelect from '@/components/common/AbSelect.vue'
import AbLoading from '@/components/common/AbLoading.vue'

const styleOptions = [
  { value: 'minimalist', label: '极简风格' },
  { value: 'luxury', label: '高端奢华' },
  { value: 'cute', label: '可爱甜美' },
  { value: 'tech', label: '科技感' },
  { value: 'vintage', label: '复古风' },
  { value: 'fresh', label: '清新自然' },
]

const submitting = ref(false)
const currentTask = ref<PosterTask | null>(null)
const generatedImages = ref<PosterImage[]>([])
const pollTimer = ref<ReturnType<typeof setInterval> | null>(null)

const form = reactive({
  title: '',
  selling_points: [''],
  brand: '',
  price: '',
  style_preference: '',
})

const errors = reactive<Record<string, string>>({})

const isGenerating = computed(() => {
  return currentTask.value &&
    (currentTask.value.status === 'drafting' || currentTask.value.status === 'generating')
})

const canSubmit = computed(() => {
  return !!form.title.trim() && !submitting.value && !isGenerating.value
})

function addSellingPoint() {
  if (form.selling_points.length < 5) {
    form.selling_points.push('')
  }
}

function removeSellingPoint(index: number) {
  if (form.selling_points.length > 1) {
    form.selling_points.splice(index, 1)
  }
}

function updateSellingPoint(index: number, value: string) {
  form.selling_points[index] = value
}

function previewImage(index: number) {
  const urls = generatedImages.value.map(img => img.url)
  uni.previewImage({
    current: urls[index],
    urls,
  })
}

function buildPosterPrompt(): string {
  return [
    `海报标题：${form.title.trim()}`,
    form.brand.trim() ? `品牌：${form.brand.trim()}` : '',
    form.price.trim() ? `价格：${form.price.trim()}` : '',
    form.style_preference ? `风格：${styleOptions.find((item) => item.value === form.style_preference)?.label || form.style_preference}` : '',
    form.selling_points.filter((point) => point.trim()).length
      ? `卖点：${form.selling_points.filter((point) => point.trim()).join('；')}`
      : '',
  ].filter(Boolean).join('\n')
}

function copyPosterPrompt() {
  const prompt = buildPosterPrompt()
  if (!prompt) return
  uni.setClipboardData({
    data: prompt,
    success: () => uni.showToast({ title: '海报 Prompt 已复制', icon: 'none' }),
  })
}

async function regenerateVariant() {
  if (!canSubmit.value) return
  await onGenerate()
}

function validate(): boolean {
  Object.keys(errors).forEach(k => delete errors[k])

  if (!form.title.trim()) {
    errors.title = '请输入海报标题'
    return false
  }

  return true
}

async function onGenerate() {
  if (!validate()) return

  submitting.value = true
  try {
    const task = await postersApi.create({
      input_content: {
        title: form.title.trim(),
        selling_points: form.selling_points.filter(s => s.trim()),
        brand: form.brand.trim(),
        price: form.price.trim() || undefined,
      },
      style_preference: form.style_preference,
    })

    currentTask.value = task
    generatedImages.value = []
    uni.showToast({ title: '海报生成中', icon: 'success' })
    startPolling(task)
  } catch (err: any) {
    const msg = err?.message || '生成失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

function startPolling(task: PosterTask) {
  stopPolling()
  if (task.status === 'completed' || task.status === 'failed') {
    if (task.status === 'completed' && task.images) {
      generatedImages.value = task.images
    }
    return
  }

  pollTimer.value = setInterval(async () => {
    if (!currentTask.value) {
      stopPolling()
      return
    }
    try {
      const updated = await postersApi.get(currentTask.value.id)
      currentTask.value = updated

      if (updated.status === 'completed') {
        generatedImages.value = updated.images || []
        stopPolling()
        uni.showToast({ title: '海报生成完成', icon: 'success' })
      } else if (updated.status === 'failed') {
        stopPolling()
        uni.showToast({ title: updated.error_message || '海报生成失败', icon: 'none' })
      }
    } catch (err) {
      console.error('Poll error:', err)
    }
  }, 3000)
}

function stopPolling() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

onUnmounted(stopPolling)
</script>

<style lang="scss" scoped>
.poster-page {
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

.section-title {
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  display: block;
  margin-bottom: $ab-space-sm;
}

.pp-section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

// Selling points
.pp-selling-points {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
}

.pp-selling-point {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;

  :deep(.ab-input-wrapper) {
    flex: 1;
  }

  &__remove {
    width: 56rpx;
    height: 56rpx;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    margin-top: 12rpx;
  }

  &__remove-icon {
    font-size: 40rpx;
    color: $ab-text-tertiary;
    line-height: 1;
  }
}

.pp-add-point {
  display: flex;
  align-items: center;
  gap: $ab-space-xs;
  padding: $ab-space-sm $ab-space-md;
  border: 2rpx dashed $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-background;

  &__icon {
    font-size: $ab-text-lg;
    color: $ab-primary;
    line-height: 1;
  }

  &__text {
    font-size: $ab-text-sm;
    color: $ab-primary;
  }
}

// Field row
.pp-field-row {
  display: flex;
  gap: $ab-space-sm;

  &__item {
    flex: 1;
    min-width: 0;
  }
}

// Image grid
.pp-image-grid {
  display: flex;
  flex-wrap: wrap;
  gap: $ab-space-sm;
}

.pp-image-item {
  width: calc((100% - #{$ab-space-sm}) / 2);
  aspect-ratio: 3 / 4;
  border-radius: $ab-radius-sm;
  overflow: hidden;

  &__img {
    width: 100%;
    height: 100%;
    background-color: $ab-divider;
  }
}

.pp-result-actions {
  display: flex;
  flex-wrap: wrap;
  gap: $ab-space-sm;
  margin-top: $ab-space-md;
}

// Generating indicator
.pp-generating {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

// Bottom
.pp-bottom {
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
