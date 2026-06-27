<template>
  <view class="designer-page">
    <view class="hero-card">
      <view>
        <text class="hero-card__title">设计师</text>
        <text class="hero-card__subtitle">AI 图片生成、参考图创作与历史追踪</text>
      </view>
      <view class="credit-pill">
        <text>{{ creditsBalance.toLocaleString() }}</text>
        <text>积分</text>
      </view>
    </view>

    <view class="section">
      <text class="field-label">模型</text>
      <AbLoading v-if="providersLoading" size="sm" text="加载模型..." />
      <scroll-view v-else scroll-x class="provider-scroll">
        <view class="provider-list">
          <view
            v-for="provider in enabledProviders"
            :key="provider.id"
            class="provider-card"
            :class="{ 'provider-card--active': selectedProviderId === provider.id }"
            @tap="selectProvider(provider.id)"
          >
            <text class="provider-card__name">{{ provider.name }}</text>
            <text class="provider-card__model">{{ provider.model }}</text>
            <text class="provider-card__credits">{{ provider.credits }} 积分/次</text>
          </view>
        </view>
      </scroll-view>
      <AbEmpty v-if="!providersLoading && enabledProviders.length === 0" title="暂无可用图片模型" />
    </view>

    <view class="section">
      <text class="field-label">提示词</text>
      <AbTextarea
        v-model="prompt"
        placeholder="描述你要生成的画面、风格、构图和用途"
        :rows="5"
        :maxlength="1200"
      />
    </view>

    <view v-if="capabilities" class="section">
      <view class="field-row">
        <view class="field-row__item">
          <text class="field-label">尺寸</text>
          <AbSelect v-model="settings.size" :options="sizeOptions" />
        </view>
        <view class="field-row__item">
          <text class="field-label">数量</text>
          <AbSelect v-model="countValue" :options="countOptions" :disabled="!capabilities.batch" />
        </view>
      </view>

      <view class="field-row">
        <view class="field-row__item">
          <text class="field-label">质量</text>
          <AbSelect v-model="settings.quality" :options="qualityOptions" />
        </view>
        <view class="field-row__item">
          <text class="field-label">格式</text>
          <AbSelect v-model="settings.outputFormat" :options="formatOptions" />
        </view>
      </view>

      <view v-if="capabilities.watermark" class="switch-row">
        <text class="field-label">生成水印</text>
        <AbSwitch v-model="settings.watermark" />
      </view>
    </view>

    <view class="section">
      <view class="section-header">
        <text class="field-label">参考图</text>
        <text class="section-header__hint">{{ referenceFiles.length }}/{{ capabilities?.maxRefImages ?? 0 }}</text>
      </view>
      <scroll-view v-if="referenceFiles.length" scroll-x class="reference-scroll">
        <view class="reference-list">
          <view v-for="(file, index) in referenceFiles" :key="file.path" class="reference-item">
            <image :src="file.path" class="reference-item__image" mode="aspectFill" />
            <text class="reference-item__remove" @tap="removeReference(index)">×</text>
          </view>
        </view>
      </scroll-view>
      <AbButton
        type="ghost"
        size="md"
        block
        :disabled="!canAddReference"
        @click="chooseReference"
      >
        选择参考图
      </AbButton>
    </view>

    <view v-if="generatedImages.length" class="section">
      <text class="section-title">生成结果</text>
      <view class="image-grid">
        <image
          v-for="(img, index) in generatedImages"
          :key="img.url"
          :src="img.url"
          class="image-grid__item"
          mode="aspectFill"
          @tap="previewGenerated(index)"
        />
      </view>
      <view v-if="canInpaint" class="edit-grid">
        <AbButton
          v-for="(img, index) in generatedImages"
          :key="`edit-${img.url}`"
          type="ghost"
          size="sm"
          @click="startEdit(index)"
        >
          编辑第 {{ index + 1 }} 张
        </AbButton>
      </view>
    </view>

    <view v-if="editingImage" class="section">
      <view class="section-header">
        <text class="section-title">局部编辑</text>
        <text class="section-header__hint" @tap="cancelEdit">取消</text>
      </view>
      <image :src="editingImage.url" class="edit-preview" mode="aspectFit" />
      <text class="field-label">编辑提示词</text>
      <AbTextarea
        v-model="editPrompt"
        placeholder="描述要修改的区域和目标效果"
        :rows="4"
        :maxlength="1200"
      />
      <view class="mask-row">
        <view class="mask-row__body">
          <text class="field-label">蒙版图</text>
          <text class="mask-row__hint">上传黑白/透明蒙版，白色或不透明区域会被编辑</text>
        </view>
        <AbButton type="ghost" size="sm" @click="chooseMask">
          {{ maskFile ? '更换' : '选择' }}
        </AbButton>
      </view>
      <image v-if="maskFile" :src="maskFile.path" class="mask-preview" mode="aspectFit" />
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="isGenerating"
        :disabled="!canSubmitEdit"
        @click="generateEdit"
      >
        提交编辑
      </AbButton>
    </view>

    <view v-if="history.length" class="section">
      <text class="section-title">历史记录</text>
      <view
        v-for="item in history"
        :key="item.id"
        class="history-row"
        @tap="selectHistory(item)"
      >
        <view class="history-row__body">
          <text class="history-row__prompt">{{ item.prompt }}</text>
          <text class="history-row__meta">{{ item.provider }} · {{ item.model }}</text>
        </view>
        <AbBadge :variant="statusVariant(item.status)" size="sm">{{ statusLabel(item.status) }}</AbBadge>
      </view>
    </view>

    <view class="bottom-bar">
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="isGenerating"
        :disabled="!canGenerate"
        @click="generate"
      >
        {{ isGenerating ? '生成中...' : '生成图片' }}
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import type { DesignerProvider, GenerateImage, ImageGeneration, ImageGenerationResult } from '@/types'
import { getModelCapabilities } from '@/types/designer'
import { designerApi } from '@/api/designer'
import { creditsApi } from '@/api/credits'
import { TOKEN_KEY } from '@/utils/constants'
import AbBadge from '@/components/common/AbBadge.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbSelect from '@/components/common/AbSelect.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'

interface LocalReference {
  path: string
  name: string
}

const POLL_INTERVAL = 2500
const MAX_POLLS = 144

const providers = ref<DesignerProvider[]>([])
const providersLoading = ref(false)
const selectedProviderId = ref('')
const prompt = ref('')
const referenceFiles = ref<LocalReference[]>([])
const generatedImages = ref<GenerateImage[]>([])
const history = ref<ImageGeneration[]>([])
const creditsBalance = ref(0)
const isGenerating = ref(false)
const editingImage = ref<GenerateImage | null>(null)
const editPrompt = ref('')
const maskFile = ref<LocalReference | null>(null)

const settings = reactive({
  size: 'auto',
  n: 1,
  quality: 'auto',
  outputFormat: 'png',
  watermark: false,
})

const enabledProviders = computed(() => providers.value.filter((provider) => provider.enabled))
const selectedProvider = computed(() => enabledProviders.value.find((provider) => provider.id === selectedProviderId.value) || enabledProviders.value[0])
const capabilities = computed(() => getModelCapabilities(selectedProvider.value?.provider || '', selectedProvider.value?.model))
const canInpaint = computed(() => capabilities.value?.inpainting === true)

const sizeOptions = computed(() => {
  const presets = capabilities.value?.sizePresets?.length ? capabilities.value.sizePresets : ['auto', '1:1', '3:4', '4:3', '16:9']
  return presets.map((value) => ({ value, label: value === 'auto' ? '自动' : value }))
})

const qualityOptions = computed(() => {
  const levels = capabilities.value?.qualityLevels?.length ? capabilities.value.qualityLevels : ['auto']
  return levels.map((value) => ({ value, label: value }))
})

const formatOptions = computed(() => {
  const formats = capabilities.value?.outputFormats?.length ? capabilities.value.outputFormats : ['png']
  return formats.map((value) => ({ value, label: value.toUpperCase() }))
})

const countOptions = computed(() => {
  const max = capabilities.value?.batch ? capabilities.value.maxBatch : 1
  return Array.from({ length: Math.min(max, 4) }, (_, i) => String(i + 1)).map((value) => ({ value, label: value }))
})

const countValue = computed({
  get: () => String(settings.n),
  set: (value: string) => {
    settings.n = Number(value) || 1
  },
})

const canAddReference = computed(() => {
  const max = capabilities.value?.maxRefImages ?? 0
  return max > 0 && referenceFiles.value.length < max && !isGenerating.value
})

const canGenerate = computed(() => !!prompt.value.trim() && !!selectedProvider.value && !isGenerating.value)
const canSubmitEdit = computed(() => !!editingImage.value && !!editPrompt.value.trim() && !!maskFile.value && !!selectedProvider.value && !isGenerating.value)

function resultsToImages(results?: ImageGenerationResult[]): GenerateImage[] {
  if (!results) return []
  return results
    .filter((item) => item.image_url || item.image_path)
    .map((item, index) => ({
      url: item.image_url || item.image_path!,
      width: item.width,
      height: item.height,
      index,
    }))
}

function selectProvider(id: string) {
  selectedProviderId.value = id
  const caps = capabilities.value
  settings.size = caps?.sizePresets?.includes('auto') ? 'auto' : caps?.sizePresets?.[0] || '1:1'
  settings.n = caps?.batch ? Math.min(settings.n, caps.maxBatch) : 1
  settings.quality = caps?.qualityLevels?.[0] || 'auto'
  settings.outputFormat = caps?.outputFormats?.[0] || 'png'
  settings.watermark = false
}

function chooseReference() {
  if (!canAddReference.value) return
  const remaining = (capabilities.value?.maxRefImages ?? 0) - referenceFiles.value.length
  uni.chooseImage({
    count: Math.max(1, Math.min(remaining, 9)),
    sizeType: ['compressed'],
    sourceType: ['album', 'camera'],
    success(res) {
      const paths = Array.isArray(res.tempFilePaths)
        ? res.tempFilePaths
        : res.tempFilePaths
          ? [res.tempFilePaths]
          : []
      const files = paths.map((path: string, index: number) => ({
        path,
        name: `reference-${Date.now()}-${index}`,
      }))
      referenceFiles.value = [...referenceFiles.value, ...files].slice(0, capabilities.value?.maxRefImages ?? 0)
    },
  })
}

function removeReference(index: number) {
  referenceFiles.value.splice(index, 1)
}

function previewGenerated(index: number) {
  const urls = generatedImages.value.map((item) => item.url)
  uni.previewImage({ current: urls[index], urls })
}

function startEdit(index: number) {
  if (!canInpaint.value) return
  editingImage.value = generatedImages.value[index]
  editPrompt.value = prompt.value
  maskFile.value = null
}

function cancelEdit() {
  editingImage.value = null
  editPrompt.value = ''
  maskFile.value = null
}

function chooseMask() {
  uni.chooseImage({
    count: 1,
    sizeType: ['compressed'],
    sourceType: ['album', 'camera'],
    success(res) {
      const path = Array.isArray(res.tempFilePaths) ? res.tempFilePaths[0] : res.tempFilePaths
      if (path) {
        maskFile.value = {
          path,
          name: `mask-${Date.now()}`,
        }
      }
    },
  })
}

function toUploadablePath(url: string): Promise<string> {
  if (!/^https?:\/\//.test(url) && !url.startsWith('/api/')) {
    return Promise.resolve(url)
  }
  return new Promise((resolve, reject) => {
    const token = uni.getStorageSync(TOKEN_KEY)
    uni.downloadFile({
      url,
      header: token ? { Authorization: `Bearer ${token}` } : undefined,
      success(res) {
        if (res.statusCode === 200) {
          resolve(res.tempFilePath)
          return
        }
        reject(new Error('源图片下载失败'))
      },
      fail(err) {
        reject(new Error(err.errMsg || '源图片下载失败'))
      },
    })
  })
}

function selectHistory(item: ImageGeneration) {
  generatedImages.value = resultsToImages(item.results)
  prompt.value = item.prompt
}

function statusLabel(status: string) {
  const map: Record<string, string> = {
    generating: '生成中',
    completed: '已完成',
    failed: '失败',
  }
  return map[status] || status
}

function statusVariant(status: string): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'generating') return 'info'
  return 'neutral'
}

async function generate() {
  if (!canGenerate.value || !selectedProvider.value) return
  isGenerating.value = true
  generatedImages.value = []

  try {
    const referenceIds = []
    for (const file of referenceFiles.value) {
      const uploaded = await designerApi.uploadReference(file.path)
      referenceIds.push(uploaded.file_id)
    }

    const response = await designerApi.generate({
      project_id: '',
      prompt: prompt.value.trim(),
      provider: selectedProvider.value.provider,
      provider_id: selectedProvider.value.id,
      model: selectedProvider.value.model,
      size: settings.size,
      n: settings.n > 1 ? settings.n : undefined,
      quality: settings.quality !== 'auto' ? settings.quality : undefined,
      output_format: settings.outputFormat,
      reference_file_ids: referenceIds.length ? referenceIds : undefined,
      watermark: settings.watermark || undefined,
    })

    await pollGeneration(response.generation_id)
    await loadHistory()
    await loadCredits()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '生成失败', icon: 'none' })
  } finally {
    isGenerating.value = false
  }
}

async function generateEdit() {
  if (!canSubmitEdit.value || !selectedProvider.value || !editingImage.value || !maskFile.value) return
  isGenerating.value = true

  try {
    const sourcePath = await toUploadablePath(editingImage.value.url)
    const [source, mask] = await Promise.all([
      designerApi.uploadReference(sourcePath, 'source'),
      designerApi.uploadReference(maskFile.value.path, 'mask'),
    ])

    const response = await designerApi.generate({
      project_id: '',
      prompt: editPrompt.value.trim(),
      provider: selectedProvider.value.provider,
      provider_id: selectedProvider.value.id,
      model: selectedProvider.value.model,
      size: settings.size,
      n: 1,
      quality: settings.quality !== 'auto' ? settings.quality : undefined,
      output_format: settings.outputFormat,
      reference_file_ids: [source.file_id],
      mask_file_id: mask.file_id,
      watermark: settings.watermark || undefined,
    })

    await pollGeneration(response.generation_id)
    cancelEdit()
    await loadHistory()
    await loadCredits()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '编辑失败', icon: 'none' })
  } finally {
    isGenerating.value = false
  }
}

async function pollGeneration(generationId: string) {
  for (let i = 0; i < MAX_POLLS; i++) {
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL))
    const generation = await designerApi.getGeneration(generationId)
    if (generation.status === 'completed') {
      generatedImages.value = resultsToImages(generation.results)
      uni.showToast({ title: '图片生成完成', icon: 'success' })
      return
    }
    if (generation.status === 'failed') {
      throw new Error(generation.error || '图片生成失败')
    }
  }
  throw new Error('生成超时，请稍后查看历史记录')
}

async function loadProviders() {
  providersLoading.value = true
  try {
    providers.value = await designerApi.getProviders()
    if (!selectedProviderId.value && enabledProviders.value.length) {
      selectProvider(enabledProviders.value[0].id)
    }
  } catch (err) {
    console.error('load designer providers failed', err)
  } finally {
    providersLoading.value = false
  }
}

async function loadHistory() {
  try {
    const res = await designerApi.getHistory({ page_size: 20 })
    history.value = res.items || []
  } catch (err) {
    console.error('load designer history failed', err)
  }
}

async function loadCredits() {
  try {
    const res = await creditsApi.balance()
    creditsBalance.value = res.balance
  } catch {
    creditsBalance.value = 0
  }
}

onMounted(() => {
  loadProviders()
  loadHistory()
  loadCredits()
})
</script>

<style lang="scss" scoped>
.designer-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 160rpx;
  box-sizing: border-box;
}

.hero-card,
.section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.hero-card {
  display: flex;
  justify-content: space-between;
  gap: $ab-space-md;

  &__title {
    display: block;
    font-size: $ab-text-xl;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__subtitle {
    display: block;
    margin-top: 6rpx;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }
}

.credit-pill {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  color: $ab-primary;
  font-size: $ab-text-sm;
  font-weight: $ab-font-semibold;
}

.field-label,
.section-title {
  display: block;
  font-size: $ab-text-base;
  font-weight: $ab-font-medium;
  color: $ab-text;
  margin-bottom: $ab-space-xs;
}

.provider-scroll,
.reference-scroll {
  white-space: nowrap;
}

.provider-list,
.reference-list {
  display: flex;
  gap: $ab-space-sm;
}

.provider-card {
  display: inline-flex;
  flex-direction: column;
  width: 260rpx;
  padding: $ab-space-sm;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  gap: 6rpx;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__name {
    font-size: $ab-text-base;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__model,
  &__credits {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.field-row {
  display: flex;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-sm;

  &__item {
    flex: 1;
    min-width: 0;
  }
}

.switch-row,
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-sm;
}

.section-header__hint {
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
}

.reference-item {
  position: relative;
  width: 144rpx;
  height: 144rpx;
  flex-shrink: 0;

  &__image {
    width: 144rpx;
    height: 144rpx;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
  }

  &__remove {
    position: absolute;
    top: 4rpx;
    right: 4rpx;
    width: 36rpx;
    height: 36rpx;
    border-radius: 50%;
    background-color: rgba(0, 0, 0, 0.55);
    color: #fff;
    text-align: center;
    line-height: 36rpx;
    font-size: $ab-text-sm;
  }
}

.image-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: $ab-space-sm;

  &__item {
    width: 100%;
    aspect-ratio: 1;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
  }
}

.history-row {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__prompt,
  &__meta {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__prompt {
    font-size: $ab-text-sm;
    color: $ab-text;
  }

  &__meta {
    margin-top: 4rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.bottom-bar {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 20;
  padding: $ab-space-sm $ab-space-md calc($ab-space-sm + env(safe-area-inset-bottom));
  background-color: $ab-surface;
  border-top: 2rpx solid $ab-border;
}
</style>
