<template>
  <view class="page task-create">
    <!-- Project selector -->
    <view class="task-create__section">
      <text class="field-label">选择账号 <text class="field-required">*</text></text>
      <ProjectSelector
        ref="projectSelectorRef"
        v-model="form.project_id"
        placeholder="搜索账号..."
        :platform-filter="requestedType || undefined"
        empty-action-text="创建账号"
        @change="onProjectChange"
      />
      <text v-if="errors.project" class="field-error">{{ errors.project }}</text>
    </view>

    <!-- Content type (locked from project) -->
    <view class="task-create__section" v-if="selectedProject">
      <text class="field-label">内容类型</text>
      <view class="locked-field">
        <PlatformAvatar :platform="selectedProject.platform" :size="32" />
        <text class="locked-field__text">{{ contentTypeLabel[selectedProject.platform] || '未知' }}</text>
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
      <ImageGenerationToolbar
        v-model:ratio="form.image_ratio"
        v-model:capability-key="form.image_capability_key"
        :ratios="allowedImageRatios"
        :capabilities="imageCapabilities"
        :loading="imageCapabilitiesLoading"
      />
      <view class="inspiration-hint" @tap="applyInspiration">
        <text class="inspiration-hint__icon">灵</text>
        <text class="inspiration-hint__text">试试: {{ currentInspiration }}</text>
      </view>
    </view>

    <view class="task-create__advanced-toggle" @tap="advancedOpen = !advancedOpen">
      <view class="task-create__advanced-toggle-main">
        <text class="task-create__advanced-toggle-title">高级控制</text>
        <text class="task-create__advanced-toggle-desc">视觉、人设和参考图</text>
      </view>
      <text class="task-create__advanced-toggle-arrow">{{ advancedOpen ? '收起' : '展开' }}</text>
    </view>

    <!-- Visual style -->
    <view class="task-create__section" v-if="advancedOpen && !isEcommerce">
      <text class="field-label">视觉风格</text>
      <AbTextarea
        v-model="form.visual_style"
        placeholder="描述画面风格，如：清新自然、暖色调、生活化场景..."
        :rows="3"
        :maxlength="1024"
      />
      <text class="field-hint">留空则使用账号默认视觉风格</text>
    </view>

    <!-- Writing style (article only) -->
    <view class="task-create__section" v-if="advancedOpen && isArticle">
      <text class="field-label">写作风格</text>
      <AbTextarea
        v-model="form.writer_key"
        placeholder="描述文章的写作风格、语气、结构..."
        :rows="3"
        :maxlength="1024"
      />
    </view>

    <!-- Theme (article only) -->
    <view class="task-create__section" v-if="advancedOpen && isArticle">
      <text class="field-label">排版主题</text>
      <view class="picker-row" @tap="pickTheme">
        <text v-if="selectedThemeName" class="picker-row__value">{{ selectedThemeName }}</text>
        <text v-else class="picker-row__placeholder">可选，选择公众号排版主题</text>
        <text v-if="form.theme" class="picker-row__clear" @tap.stop="form.theme = ''">清除</text>
        <text v-else class="picker-row__arrow">›</text>
      </view>
    </view>

    <!-- Persona (article / seednote) -->
    <view class="task-create__section" v-if="advancedOpen && (isArticle || isSeednote)">
      <text class="field-label">作者人设</text>
      <view class="field-spacer">
        <text class="field-sublabel">作者署名</text>
        <AbInput v-model="form.byline" placeholder="显示在文章/笔记的作者名" />
      </view>
      <view class="field-spacer">
        <text class="field-sublabel">写作风格模仿</text>
        <AbTextarea
          v-model="form.writing_voice"
          placeholder="模仿某位作者/博主的文风，描述其语言习惯、句式特点..."
          :rows="2"
          :maxlength="1024"
        />
      </view>
      <view class="field-spacer">
        <text class="field-sublabel">人设头像 URL</text>
        <AbInput v-model="form.persona_avatar" placeholder="可选，作者头像图片地址" />
      </view>
    </view>

    <!-- Seednote image composition (seednote only) -->
    <view class="task-create__section" v-if="isSeednote">
      <text class="field-label">图片组合</text>
      <view class="switch-row">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">生成内容图</text>
          <text class="field-hint">除封面外，额外生成正文配图</text>
        </view>
        <AbSwitch v-model="form.has_content_image" />
      </view>
      <view class="switch-row field-spacer">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">生成尾图</text>
          <text class="field-hint">在笔记末尾生成引导/总结图</text>
        </view>
        <AbSwitch v-model="form.has_tail_image" />
      </view>
    </view>

    <!-- Article image composition (公众号 article): cover + content images each
         independently toggleable — unlike seednote, the article cover is NOT
         mandatory. Both default on → legacy behavior. -->
    <view class="task-create__section" v-if="isArticle">
      <text class="field-label">图片组合</text>
      <view class="switch-row">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">生成封面图</text>
          <text class="field-hint">公众号头图（900×383），关闭则发布草稿不设封面</text>
        </view>
        <AbSwitch v-model="form.article_with_cover" />
      </view>
      <view class="switch-row field-spacer">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">生成正文配图</text>
          <text class="field-hint">按排版节奏插入的章节插图</text>
        </view>
        <AbSwitch v-model="form.article_with_content_images" />
      </view>
    </view>

    <!-- E-commerce package (ecommerce only): product photos + delivery modules -->
    <view class="task-create__section" v-if="isEcommerce">
      <text class="field-label">电商素材包</text>
      <text class="field-hint">上传产品图并选择交付模块。创建按固定任务 SKU 计价。</text>

      <!-- Product photos (required) -->
      <view class="field-spacer">
        <text class="field-sublabel">产品图（必填）<text class="field-sublabel-count">{{ form.product_photos.length }}/16</text></text>
        <view class="product-grid">
          <view
            v-for="(url, i) in form.product_photos"
            :key="i"
            class="product-thumb"
          >
            <image class="product-thumb__img" :src="url" mode="aspectFill" />
            <view class="product-thumb__remove" @tap.stop="removeProductPhoto(i)">×</view>
          </view>
          <view
            v-if="form.product_photos.length < 16"
            class="product-add"
            :class="{ 'product-add--disabled': uploadingPhoto }"
            @tap="chooseProductPhoto"
          >
            <text class="product-add__icon">＋</text>
            <text class="product-add__text">{{ uploadingPhoto ? '上传中' : '添加' }}</text>
          </view>
        </view>
      </view>

      <!-- Delivery modules (at least one required) -->
      <view class="field-spacer">
        <text class="field-sublabel">交付模块（至少选一项）</text>
        <view class="module-list">
          <view
            v-for="mod in ecommerceModuleCatalog"
            :key="mod.key"
            class="module-row"
            :class="{ 'module-row--active': moduleQty(mod.key) >= 1 }"
          >
            <view class="module-row__main" @tap="toggleModule(mod.key, moduleQty(mod.key) < 1)">
              <view class="module-row__head">
                <text class="module-row__label">{{ mod.label }}</text>
                <text class="module-row__ratio">{{ mod.ratio }}</text>
              </view>
              <text class="module-row__hint">{{ mod.hint }}</text>
            </view>
            <view class="module-row__controls">
              <view v-if="moduleQty(mod.key) >= 1" class="qty-stepper">
                <text class="qty-stepper__btn" @tap.stop="setModuleQty(mod.key, moduleQty(mod.key) - mod.qtyStep)">−</text>
                <text class="qty-stepper__value">{{ moduleQty(mod.key) }}{{ mod.qtyLabel }}</text>
                <text class="qty-stepper__btn" @tap.stop="setModuleQty(mod.key, moduleQty(mod.key) + mod.qtyStep)">＋</text>
              </view>
              <AbSwitch
                :model-value="moduleQty(mod.key) >= 1"
                @update:model-value="(on: boolean) => toggleModule(mod.key, on)"
              />
            </view>
          </view>
        </view>
        <text v-if="enabledModuleCount === 0" class="field-error">请至少选择一个交付模块</text>
      </view>

      <view class="field-spacer">
        <text class="field-sublabel">目标平台</text>
        <view class="picker-row" @tap="pickTargetPlatform">
          <text v-if="form.target_platform" class="picker-row__value">{{ targetPlatformLabel }}</text>
          <text v-else class="picker-row__placeholder">选择投放平台</text>
          <text class="picker-row__arrow">›</text>
        </view>
      </view>
      <view class="field-spacer">
        <text class="field-sublabel">核心卖点（可选）</text>
        <AbTextarea
          v-model="form.selling_points"
          placeholder="产品核心卖点（材质 / 功能 / 使用场景 / 价格优势等）。留空则由 AI 从产品图分析提炼"
          :rows="3"
          :maxlength="2000"
        />
      </view>
      <view class="field-spacer">
        <text class="field-sublabel">语言</text>
        <view class="picker-row" @tap="pickLanguage">
          <text v-if="form.language" class="picker-row__value">{{ languageLabel }}</text>
          <text v-else class="picker-row__placeholder">默认中文</text>
          <text class="picker-row__arrow">›</text>
        </view>
      </view>
    </view>

    <!-- Quantity selector (hidden for ecommerce) -->
    <view class="task-create__section" v-if="!isEcommerce">
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

    <!-- Image generation options -->
    <view class="task-create__section" v-if="advancedOpen">
      <view class="switch-row">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">跳过参考图</text>
          <text class="field-hint">不使用账号默认视觉参考图生成图片</text>
        </view>
        <AbSwitch v-model="form.skip_reference_image" />
      </view>

      <view class="field-spacer">
        <text class="field-label">本次参考图片</text>
        <image
          v-if="referencePreviewUrl"
          :src="referencePreviewUrl"
          class="reference-preview"
          mode="aspectFill"
        />
        <view class="reference-actions">
          <AbButton type="ghost" size="sm" :loading="referenceUploading" @click="chooseReferenceImage">
            {{ referencePreviewUrl ? '更换图片' : '上传图片' }}
          </AbButton>
          <AbButton v-if="referencePreviewUrl" type="danger" size="sm" @click="clearReferenceImage">
            清除
          </AbButton>
        </view>
      </view>

      <view class="switch-row field-spacer">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">图片水印</text>
          <text class="field-hint">生成支持水印的图片时添加平台水印</text>
        </view>
        <AbSwitch v-model="form.watermark" />
      </view>
    </view>

    <!-- Agent execution profile -->
    <view class="task-create__section">
      <text class="field-label">执行配置 <text class="field-required">*</text></text>
      <ExecutionProfileSelector
        v-model="form.execution_profile"
        :profiles="executionProfiles"
        :loading="executionProfilesLoading"
        :disabled="submitting"
      />
      <text v-if="executionProfilesError" class="field-error">{{ executionProfilesError }}</text>
    </view>

    <!-- Credit info -->
    <view class="task-create__section">
      <view class="credit-info">
        <view class="credit-info__row">
          <text class="credit-info__label">基础任务费</text>
          <text class="credit-info__value credit-info__value--cost">
            {{ priceAvailable ? `${estimatedCost.toLocaleString()} 积分` : '暂不可用' }}
          </text>
        </view>
        <view class="credit-info__row">
          <text class="credit-info__label">当前余额</text>
          <text class="credit-info__value" :class="{ 'credit-info__value--low': balance < creationCost }">
            {{ balance.toLocaleString() }} 积分
          </text>
        </view>
      </view>
      <text class="field-hint">创建时锁定固定任务 SKU；任务内成功交付的图片、视频等增值操作按固定 SKU 结算，允许产生欠费。</text>
      <text v-if="!priceAvailable" class="field-error">固定价格目录暂不可用，请稍后重试</text>
      <text v-if="debt > 0" class="field-error">当前有欠费，请先充值补齐</text>
      <text v-if="balance > 0 && balance < creationCost" class="field-error">积分不足，请先充值</text>
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
        {{ !isEcommerce && form.quantity > 1 ? `创建 ${form.quantity} 个任务` : '开始创作' }}
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { onLoad, onShow } from '@dcloudio/uni-app'
import type {
  AgentExecutionProfileCapability,
  AgentExecutionProfileID,
  BillingCatalog,
  Project,
  ResourceEntry,
  ReferenceImageSelection,
  ImageCapabilityOption,
  PlatformConfig,
} from '@/types'
import { agentProfilesApi } from '@/api/agent-profiles'
import { tasksApi } from '@/api/tasks'
import { resourcesApi } from '@/api/resources'
import { billingApi } from '@/api/billing'
import { projectsApi } from '@/api/projects'
import { imageCapabilitiesApi } from '@/api/image-capabilities'
import {
  resolveExecutionProfileSelection,
  taskPriceForExecutionProfile,
} from '@/utils/execution-profiles'
import { TASK_QUANTITIES } from '@/utils/constants'
import {
  contentTypeLabel,
  ecommerceModuleCatalog,
  ecommerceTargetPlatformOptions,
  ecommerceLanguageOptions,
} from '@/utils/labels'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import ProjectSelector from '@/components/business/ProjectSelector.vue'
import ImageGenerationToolbar from '@/components/business/ImageGenerationToolbar.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'
import ExecutionProfileSelector from '@/components/business/ExecutionProfileSelector.vue'

const inspirations = [
  '写一篇关于春季护肤的笔记，风格温暖自然',
  '分享5个提升效率的办公好物，搭配实拍图',
  '做一期测评：对比3款热门面霜的真实体验',
  '分享一周穿搭灵感，适合通勤和约会',
]

const TARGET_PLATFORM_LABELS = ecommerceTargetPlatformOptions.map((o) => o.label)
const LANGUAGE_LABELS = ecommerceLanguageOptions.map((o) => o.label)
const selectedProject = ref<Project | null>(null)
const balance = ref(0)
const debt = ref(0)
const catalog = ref<BillingCatalog | null>(null)
const executionProfiles = ref<AgentExecutionProfileCapability[]>([])
const executionProfilesLoading = ref(false)
const executionProfilesError = ref('')
const executionProfilePrefilled = ref(false)
const submitting = ref(false)
const inspirationIndex = ref(0)
const requestedType = ref('')
const prefillProjectId = ref('')
const prefillPrompt = ref('')
const advancedOpen = ref(false)
const projectSelectorRef = ref<{ refresh?: () => void } | null>(null)
const referenceUploading = ref(false)
const referencePreviewUrl = ref('')

// Loaded on platform change
const platformThemes = ref<ResourceEntry[]>([])
const imageCapabilities = ref<ImageCapabilityOption[]>([])
const imageCapabilitiesLoading = ref(false)
const defaultImageCapability = ref('')
const platformConfigs = ref<PlatformConfig[]>([])

const form = reactive({
  project_id: '',
  execution_profile: '' as AgentExecutionProfileID | '',
  prompt: '',
  quantity: 1,
  image_ratio: 'auto',
  image_capability_key: '',
  visual_style: '',
  writer_key: '',
  theme: '',
  byline: '',
  writing_voice: '',
  persona_avatar: '',
  has_content_image: false,
  has_tail_image: false,
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true → legacy "always generate both" (zero regression).
  article_with_cover: true,
  article_with_content_images: true,
  // ecommerce
  target_platform: '',
  selling_points: '',
  language: '',
  product_photos: [] as string[],
  selected_modules: {} as Record<string, number>,
  // image options
  skip_reference_image: false,
  reference_image: null as ReferenceImageSelection | null,
  watermark: false,
})

const errors = reactive<Record<string, string>>({})

const platform = computed(() => selectedProject.value?.platform || '')
const selectedCapability = computed(() => imageCapabilities.value.find((item) => item.key === form.image_capability_key))
const imageCapabilityUnavailable = computed(() =>
  !!form.image_capability_key && (
    !selectedCapability.value
    || selectedCapability.value.enabled !== true
    || selectedCapability.value.price_available !== true
  ),
)
const allowedImageRatios = computed(() => platformConfigs.value.find((item) => item.id === platform.value)?.supported_image_ratios || [])
const isArticle = computed(() => platform.value === 'article')
const isSeednote = computed(() => platform.value === 'seednote')
const isEcommerce = computed(() => platform.value === 'ecommerce')

// ---- E-commerce: product photos + delivery modules ----
const uploadingPhoto = ref(false)

function chooseReferenceImage() {
  if (referenceUploading.value) return
  uni.chooseImage({
    count: 1,
    success: async (chosen) => {
      const filePath = chosen.tempFilePaths?.[0]
      if (!filePath) return
      referenceUploading.value = true
      try {
        const uploaded = await projectsApi.uploadImage(filePath, 'task_reference')
        form.reference_image = { upload_session_id: uploaded.upload_session_id }
        referencePreviewUrl.value = uploaded.preview_url
        uni.showToast({ title: '上传成功', icon: 'success' })
      } catch (err: any) {
        uni.showToast({ title: err?.message || '上传失败', icon: 'none' })
      } finally {
        referenceUploading.value = false
      }
    },
  })
}

function clearReferenceImage() {
  form.reference_image = null
  referencePreviewUrl.value = ''
}

function moduleQty(key: string): number {
  return form.selected_modules[key] ?? 0
}

function setModuleQty(key: string, qty: number) {
  const mod = ecommerceModuleCatalog.find((m) => m.key === key)
  if (!mod) return
  const clamped = Math.max(mod.minQty, Math.min(mod.maxQty, qty))
  // Use a fresh object so Vue reactivity picks up the nested change.
  form.selected_modules = { ...form.selected_modules, [key]: clamped }
}

function toggleModule(key: string, on: boolean) {
  const mod = ecommerceModuleCatalog.find((m) => m.key === key)
  if (!mod) return
  setModuleQty(key, on ? mod.defaultQty : 0)
}

const enabledModuleCount = computed(
  () => Object.values(form.selected_modules).filter((q) => q >= 1).length,
)

const targetPlatformLabel = computed(
  () => ecommerceTargetPlatformOptions.find((o) => o.value === form.target_platform)?.label || '',
)
const languageLabel = computed(
  () => ecommerceLanguageOptions.find((o) => o.value === form.language)?.label || '',
)

async function chooseProductPhoto() {
  if (form.product_photos.length >= 16) {
    uni.showToast({ title: '最多上传 16 张产品图', icon: 'none' })
    return
  }
  try {
    const pick = await uni.chooseImage({ count: 16 - form.product_photos.length, sourceType: ['album', 'camera'] })
    const paths = pick.tempFilePaths || []
    if (paths.length === 0) return
    uploadingPhoto.value = true
    uni.showLoading({ title: '上传中...' })
    for (const p of paths) {
      const res = await projectsApi.uploadImage(p, 'ecommerce_product_photo')
      if (res.url) form.product_photos.push(res.url)
    }
  } catch (err: any) {
    uni.showToast({ title: err?.message || '上传失败', icon: 'none' })
  } finally {
    uploadingPhoto.value = false
    uni.hideLoading()
  }
}

function removeProductPhoto(index: number) {
  form.product_photos.splice(index, 1)
}

const currentInspiration = computed(() => inspirations[inspirationIndex.value % inspirations.length])

const selectedThemeName = computed(() => {
  const t = platformThemes.value.find((x) => x.name === form.theme)
  return t?.display_name || t?.name || form.theme || ''
})

const resolvedTaskPrice = computed(() => {
  if (!selectedProject.value || !form.execution_profile) return undefined
  return taskPriceForExecutionProfile(
    catalog.value,
    selectedProject.value.platform,
    form.execution_profile,
  )
})

const priceAvailable = computed(() => resolvedTaskPrice.value !== undefined)
const selectedExecutionProfileAvailable = computed(() => {
  return executionProfiles.value.some((profile) =>
    profile.id === form.execution_profile && profile.available,
  )
})

const estimatedCost = computed(() => {
  const costPerTask = resolvedTaskPrice.value ?? 0
  const billableQuantity = isEcommerce.value ? 1 : form.quantity
  return costPerTask * billableQuantity
})

const creationCost = computed(() => estimatedCost.value)

const canSubmit = computed(() => {
  if (!form.project_id || !form.prompt.trim() || !form.execution_profile) return false
  if (!selectedExecutionProfileAvailable.value) return false
  if (imageCapabilityUnavailable.value) return false
  if (!priceAvailable.value || debt.value > 0 || balance.value < creationCost.value) return false
  return !submitting.value && !referenceUploading.value
})

function onProjectChange(project: Project) {
  selectedProject.value = project
  form.project_id = project.id

  form.image_ratio = project.image_ratio || 'auto'
  form.image_capability_key = project.ecommerce_defaults?.image_capability_key || defaultImageCapability.value

  delete errors.project
  ensureExecutionProfileSelection()
  loadPlatformResources()
}

function ensureExecutionProfileSelection() {
  if (!selectedProject.value) return
  form.execution_profile = resolveExecutionProfileSelection(
    form.execution_profile,
    executionProfilePrefilled.value,
    executionProfiles.value,
    catalog.value,
    selectedProject.value.platform,
  )
}

async function applyProjectById(projectId: string) {
  if (!projectId) return
  prefillProjectId.value = projectId
  form.project_id = projectId
  try {
    const projects = await projectsApi.list({ status: 'active' })
    const project = projects.find((item) => item.id === projectId)
    if (project) {
      onProjectChange(project)
    }
  } catch (err) {
    console.error('Failed to prefill project:', err)
  }
}

async function loadPlatformResources() {
  if (!selectedProject.value) return
  const plat = selectedProject.value.platform
  // Themes (article)
  if (plat === 'article') {
    try {
      const res = await resourcesApi.list('themes', plat)
      platformThemes.value = res.items || []
    } catch {
      platformThemes.value = []
    }
  } else {
    platformThemes.value = []
  }
}

function pickTheme() {
  if (platformThemes.value.length === 0) {
    uni.showToast({ title: '暂无可用主题', icon: 'none' })
    return
  }
  const labels = platformThemes.value.map((t) => t.display_name || t.name)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const theme = platformThemes.value[res.tapIndex]
      if (theme) form.theme = theme.name
    },
  })
}

function pickTargetPlatform() {
  uni.showActionSheet({
    itemList: TARGET_PLATFORM_LABELS,
    success: (res) => {
      const opt = ecommerceTargetPlatformOptions[res.tapIndex]
      if (opt) form.target_platform = opt.value
    },
  })
}

function pickLanguage() {
  uni.showActionSheet({
    itemList: LANGUAGE_LABELS,
    success: (res) => {
      const opt = ecommerceLanguageOptions[res.tapIndex]
      if (opt) form.language = opt.value
    },
  })
}

function applyInspiration() {
  form.prompt = currentInspiration.value
  inspirationIndex.value = (inspirationIndex.value + 1) % inspirations.length
  delete errors.prompt
}

function validate(): boolean {
  Object.keys(errors).forEach((k) => delete errors[k])

  if (!form.project_id) {
    errors.project = '请选择账号'
    return false
  }
  if (!form.prompt.trim()) {
    errors.prompt = '请输入创作要求'
    return false
  }
  if (!form.execution_profile) {
    uni.showToast({ title: '请选择可用的执行配置', icon: 'none' })
    return false
  }
  if (imageCapabilityUnavailable.value) {
    errors.image_capability_key = '该图像能力已停用，请重新选择'
    uni.showToast({ title: errors.image_capability_key, icon: 'none' })
    return false
  }
  // Ecommerce requires product photos + at least one delivery module
  // (matches studio's createTaskSchema superRefine).
  if (isEcommerce.value) {
    if (form.product_photos.length === 0) {
      uni.showToast({ title: '请至少上传一张产品图', icon: 'none' })
      return false
    }
    if (enabledModuleCount.value === 0) {
      uni.showToast({ title: '请至少选择一个交付模块', icon: 'none' })
      return false
    }
  }
  if (debt.value > 0) {
    uni.showToast({ title: '请先充值补齐欠费', icon: 'none' })
    return false
  }
  if (!priceAvailable.value) {
    uni.showToast({ title: '固定价格目录不可用', icon: 'none' })
    return false
  }
  if (balance.value < creationCost.value) {
    uni.showToast({ title: '积分不足，请先充值', icon: 'none' })
    return false
  }
  return true
}

async function onSubmit() {
  if (!canSubmit.value || referenceUploading.value) return
  if (!validate()) return
  if (!selectedProject.value) return
  const executionProfile = form.execution_profile
  if (!executionProfile) return

  submitting.value = true
  try {
    const task = await tasksApi.create({
      type: selectedProject.value.platform as any,
      execution_profile: executionProfile,
      project_id: form.project_id,
      prompt: form.prompt.trim(),
      quantity: isEcommerce.value ? undefined : form.quantity,
      image_ratio: form.image_ratio,
      image_capability_key: form.image_capability_key || undefined,
      visual_style: form.visual_style.trim() || undefined,
      writer_key: isArticle.value && form.writer_key.trim() ? form.writer_key.trim() : undefined,
      theme: isArticle.value && form.theme ? form.theme : undefined,
      byline: form.byline.trim() || undefined,
      writing_voice: form.writing_voice.trim() || undefined,
      persona_avatar: form.persona_avatar.trim() || undefined,
      has_content_image: isSeednote.value && form.has_content_image ? true : undefined,
      has_tail_image: isSeednote.value && form.has_tail_image ? true : undefined,
      // Article image toggles: send actual boolean (incl. false when toggled off);
      // non-article omits. Both default true → server honors the explicit false.
      article_with_cover: isArticle.value ? form.article_with_cover : undefined,
      article_with_content_images: isArticle.value ? form.article_with_content_images : undefined,
      // ecommerce basic fields
      target_platform: isEcommerce.value && form.target_platform ? form.target_platform : undefined,
      selling_points: isEcommerce.value && form.selling_points.trim() ? form.selling_points.trim() : undefined,
      language: isEcommerce.value && form.language ? form.language : undefined,
      product_photos: isEcommerce.value && form.product_photos.length ? form.product_photos : undefined,
      selected_modules:
        isEcommerce.value && enabledModuleCount.value > 0 ? form.selected_modules : undefined,
      skip_reference_image: form.skip_reference_image || undefined,
      reference_image: form.reference_image || undefined,
      watermark: form.watermark || undefined,
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
    const res = await billingApi.wallet()
    balance.value = res.balance ?? 0
    debt.value = res.debt ?? 0
  } catch {
    balance.value = 0
    debt.value = 0
  }
}

async function loadExecutionConfiguration() {
  executionProfilesLoading.value = true
  executionProfilesError.value = ''
  const [profilesResult, catalogResult] = await Promise.allSettled([
    agentProfilesApi.list(),
    billingApi.catalog(),
  ])
  executionProfiles.value = profilesResult.status === 'fulfilled' ? profilesResult.value : []
  catalog.value = catalogResult.status === 'fulfilled' ? catalogResult.value : null
  if (profilesResult.status === 'rejected') {
    executionProfilesError.value = '执行配置加载失败，请稍后重试'
  } else if (catalogResult.status === 'rejected') {
    executionProfilesError.value = '价格目录加载失败，请稍后重试'
  }
  executionProfilesLoading.value = false
  ensureExecutionProfileSelection()
}

async function loadImageCapabilities() {
  imageCapabilitiesLoading.value = true
  try {
    const response = await imageCapabilitiesApi.list()
    imageCapabilities.value = response.items || []
    defaultImageCapability.value = response.default_capability || response.items?.[0]?.key || ''
    if (!form.image_capability_key) form.image_capability_key = defaultImageCapability.value
  } catch {
    imageCapabilities.value = []
  } finally {
    imageCapabilitiesLoading.value = false
  }
}

async function loadPlatformConfigs() {
  try {
    platformConfigs.value = await projectsApi.platformConfigs()
  } catch {
    platformConfigs.value = []
  }
}

function safeDecodeQuery(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

onLoad((query) => {
  if (query?.execution_profile) {
    form.execution_profile = String(query.execution_profile) as AgentExecutionProfileID
    executionProfilePrefilled.value = true
  }
  if (query?.type) {
    requestedType.value = String(query.type)
  }
  if (query?.prompt) {
    prefillPrompt.value = safeDecodeQuery(String(query.prompt))
    form.prompt = prefillPrompt.value
  }
  if (query?.project_id) {
    void applyProjectById(String(query.project_id))
  }
})

onShow(() => {
  projectSelectorRef.value?.refresh?.()
})

onMounted(() => {
  loadBalance()
  loadExecutionConfiguration()
  loadImageCapabilities()
  loadPlatformConfigs()
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

.field-sublabel {
  font-size: $ab-text-sm;
  color: $ab-text-secondary;
  display: block;
  margin-bottom: $ab-space-xs;
}

.field-required {
  color: $ab-danger;
}

.task-create__advanced-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-md;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;

  &-main {
    flex: 1;
    min-width: 0;
  }

  &-title {
    display: block;
    font-size: $ab-text-base;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &-desc {
    display: block;
    margin-top: 4rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    line-height: 1.4;
  }

  &-arrow {
    flex-shrink: 0;
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
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

// Picker row (template / theme / platform)
.picker-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;
  min-height: 80rpx;

  &__value {
    font-size: $ab-text-base;
    color: $ab-text;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__placeholder {
    font-size: $ab-text-base;
    color: $ab-text-tertiary;
    flex: 1;
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
    margin-left: $ab-space-sm;
  }

  &__clear {
    font-size: $ab-text-xs;
    color: $ab-primary;
    margin-left: $ab-space-sm;
    padding: $ab-space-xs $ab-space-sm;
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

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-md;

  &__text {
    flex: 1;
    min-width: 0;
  }

  &__label {
    margin-bottom: 4rpx;
  }
}

.field-spacer {
  margin-top: $ab-space-md;
}

.field-hint {
  display: block;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  line-height: 1.4;
  margin-top: $ab-space-xs;
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

// ---- E-commerce: product photos + delivery modules ----
.field-sublabel-count {
  margin-left: $ab-space-xs;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  font-weight: $ab-font-normal;
}

.product-grid {
  display: flex;
  flex-wrap: wrap;
  gap: $ab-space-sm;
  margin-top: $ab-space-xs;
}

.product-thumb {
  position: relative;
  width: 160rpx;
  height: 160rpx;
  border-radius: $ab-radius-sm;
  overflow: hidden;
  background-color: $ab-divider;

  &__img {
    width: 100%;
    height: 100%;
  }

  &__remove {
    position: absolute;
    top: 4rpx;
    right: 4rpx;
    width: 36rpx;
    height: 36rpx;
    line-height: 32rpx;
    text-align: center;
    border-radius: 50%;
    background-color: rgba(0, 0, 0, 0.55);
    color: #ffffff;
    font-size: $ab-text-lg;
  }
}

.product-add {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  width: 160rpx;
  height: 160rpx;
  border: 2rpx dashed $ab-border;
  border-radius: $ab-radius-sm;
  color: $ab-text-tertiary;
  gap: $ab-space-xs;

  &__icon {
    font-size: 44rpx;
    line-height: 1;
  }

  &__text {
    font-size: $ab-text-xs;
  }

  &--disabled {
    opacity: 0.5;
    pointer-events: none;
  }
}

.reference-preview {
  display: block;
  width: 160rpx;
  height: 160rpx;
  margin-top: $ab-space-xs;
  border-radius: $ab-radius-sm;
}

.reference-actions {
  display: flex;
  gap: $ab-space-sm;
  margin-top: $ab-space-sm;
}

.module-list {
  display: flex;
  flex-direction: column;
  margin-top: $ab-space-xs;
}

.module-row {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &--active {
    // subtle emphasis when enabled
  }

  &__main {
    flex: 1;
    min-width: 0;
  }

  &__head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: $ab-space-xs;
  }

  &__label {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__ratio {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    background-color: $ab-divider;
    border-radius: $ab-radius-full;
    padding: 2rpx 12rpx;
  }

  &__price {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__hint {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 4rpx;
  }

  &__controls {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    flex-shrink: 0;
  }
}

.qty-stepper {
  display: flex;
  align-items: center;
  gap: $ab-space-xs;

  &__btn {
    width: 48rpx;
    height: 48rpx;
    line-height: 44rpx;
    text-align: center;
    border: 2rpx solid $ab-border;
    border-radius: $ab-radius-sm;
    font-size: $ab-text-md;
    color: $ab-text;
  }

  &__value {
    min-width: 64rpx;
    text-align: center;
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }
}
</style>
