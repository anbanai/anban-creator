<template>
  <view class="plan-create">
    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <view v-if="!pageLoading" class="form">
      <!-- Project selector -->
      <view class="form-section">
        <text class="form-section__label">选择账号 <text class="form-section__required">*</text></text>
        <ProjectSelector
          v-model="form.projectId"
          placeholder="选择账号..."
          @change="onProjectChange"
        />
        <text v-if="errors.projectId" class="form-section__error">{{ errors.projectId }}</text>
      </view>

      <!-- Content type (locked from project) -->
      <view class="form-section" v-if="selectedProject">
        <text class="form-section__label">内容类型</text>
        <view class="locked-field">
          <PlatformAvatar :platform="selectedProject.platform" :size="32" />
          <text class="locked-field__text">{{ contentTypeLabel[selectedProject.platform] || '未知' }}</text>
          <text class="locked-field__hint">（跟随账号平台）</text>
        </view>
      </view>

      <!-- Plans only support seednote / article (matches studio planSchema).
           E-commerce / 小绿书 are one-shot, not scheduled. -->
      <view class="form-section" v-if="planUnsupported">
        <view class="notice-bar">
          <text class="notice-bar__text">计划仅支持「公众号 / 种草笔记」账号。电商出图、小绿书为一次性生成，请改用任务创建。</text>
        </view>
      </view>

      <view class="form-section">
        <text class="form-section__label">执行配置 <text class="form-section__required">*</text></text>
        <ExecutionProfileSelector
          v-model="form.executionProfile"
          :profiles="executionProfiles"
          :loading="executionProfilesLoading"
          :disabled="submitting"
        />
        <text v-if="resolvedPlanPrice !== undefined" class="form-section__hint">
          每次任务准入费 {{ resolvedPlanPrice.toLocaleString() }} 积分
        </text>
        <text v-if="executionProfilesError" class="form-section__error">{{ executionProfilesError }}</text>
      </view>

      <!-- Schedule frequency -->
      <view class="form-section">
        <text class="form-section__label">执行频率 <text class="form-section__required">*</text></text>
        <SchedulePicker v-model="form.cronExpr" />
      </view>

      <!-- Custom time picker -->
      <view class="form-section">
        <text class="form-section__label">执行时间 <text class="form-section__required">*</text></text>
        <picker
          mode="time"
          :value="selectedTime"
          @change="onTimeChange"
        >
          <view class="time-picker">
            <text class="time-picker__value">{{ selectedTime || '选择时间' }}</text>
            <text class="time-picker__arrow">›</text>
          </view>
        </picker>
      </view>

      <!-- Weekday multi-select (visible for weekly presets) -->
      <view v-if="showWeekdaySelect" class="form-section">
        <text class="form-section__label">星期选择</text>
        <view class="weekday-grid">
          <view
            v-for="(day, index) in weekdays"
            :key="index"
            :class="['weekday-item', { active: form.weekdays.includes(index) }]"
            @tap="toggleWeekday(index)"
          >
            <text class="weekday-item__label">{{ day }}</text>
          </view>
        </view>
      </view>

      <!-- Optional prompt -->
      <view class="form-section">
        <text class="form-section__label">创作提示（可选）</text>
        <AbTextarea
          v-model="form.prompt"
          placeholder="留空则根据账号信息自动生成，&#10;或填写每次执行的额外要求..."
          :maxlength="5120"
          :rows="3"
        />
        <ImageGenerationToolbar
          v-model:ratio="form.imageRatio"
          v-model:capability-key="form.imageCapabilityKey"
          :ratios="allowedImageRatios"
          :capabilities="imageCapabilities"
          :loading="imageCapabilitiesLoading"
        />
      </view>

      <!-- Visual style -->
      <view class="form-section" v-if="!isEcommerce">
        <text class="form-section__label">视觉风格</text>
        <AbTextarea
          v-model="form.visual_style"
          placeholder="描述画面风格，如：清新自然、暖色调、生活化场景..."
          :rows="3"
          :maxlength="1024"
        />
        <text class="form-section__hint">留空则使用账号默认视觉风格</text>
      </view>

      <!-- Writing style (article only) -->
      <view class="form-section" v-if="isArticle">
        <text class="form-section__label">写作风格</text>
        <AbTextarea
          v-model="form.writer_key"
          placeholder="描述文章的写作风格、语气、结构..."
          :rows="3"
          :maxlength="100"
        />
      </view>

      <!-- Theme (article only) -->
      <view class="form-section" v-if="isArticle">
        <text class="form-section__label">排版主题</text>
        <view class="picker-row" @tap="pickTheme">
          <text v-if="selectedThemeName" class="picker-row__value">{{ selectedThemeName }}</text>
          <text v-else class="picker-row__placeholder">可选，选择公众号排版主题</text>
          <text v-if="form.theme" class="picker-row__clear" @tap.stop="form.theme = ''">清除</text>
          <text v-else class="picker-row__arrow">›</text>
        </view>
      </view>

      <!-- Persona (article / seednote) -->
      <view class="form-section" v-if="isArticle || isSeednote">
        <text class="form-section__label">作者人设</text>
        <view class="field-spacer">
          <text class="form-section__sublabel">作者署名</text>
          <AbInput v-model="form.byline" placeholder="显示在文章/笔记的作者名" />
        </view>
        <view class="field-spacer">
          <text class="form-section__sublabel">写作风格模仿</text>
          <AbTextarea
            v-model="form.writing_voice"
            placeholder="模仿某位作者/博主的文风，描述其语言习惯、句式特点..."
            :rows="2"
            :maxlength="1024"
          />
        </view>
        <view class="field-spacer">
          <text class="form-section__sublabel">人设头像 URL</text>
          <AbInput v-model="form.persona_avatar" placeholder="可选，作者头像图片地址" />
        </view>
      </view>

      <!-- Seednote image composition (seednote only) -->
      <view class="form-section" v-if="isSeednote">
        <text class="form-section__label">图片组合</text>
        <view class="switch-row">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">生成内容图</text>
            <text class="form-section__hint">除封面外，额外生成正文配图</text>
          </view>
          <AbSwitch v-model="form.hasContentImage" />
        </view>
        <view class="switch-row field-spacer">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">生成尾图</text>
            <text class="form-section__hint">在笔记末尾生成引导/总结图</text>
          </view>
          <AbSwitch v-model="form.hasTailImage" />
        </view>
      </view>

      <!-- Article image composition (公众号 article): cover + content images each
           independently toggleable — unlike seednote, the article cover is NOT
           mandatory. Both default on → legacy behavior. -->
      <view class="form-section" v-if="isArticle">
        <text class="form-section__label">图片组合</text>
        <view class="switch-row">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">生成封面图</text>
            <text class="form-section__hint">公众号头图（900×383），关闭则发布草稿不设封面</text>
          </view>
          <AbSwitch v-model="form.articleWithCover" />
        </view>
        <view class="switch-row field-spacer">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">生成正文配图</text>
            <text class="form-section__hint">按排版节奏插入的章节插图</text>
          </view>
          <AbSwitch v-model="form.articleWithContentImages" />
        </view>
      </view>

      <!-- Image generation options -->
      <view class="form-section">
        <view class="switch-row">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">跳过参考图</text>
            <text class="form-section__hint">执行计划时不使用账号默认视觉参考图</text>
          </view>
          <AbSwitch v-model="form.skipReferenceImage" />
        </view>

        <view class="field-spacer">
          <text class="form-section__label">参考图片</text>
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
            <text class="form-section__label switch-row__label">图片水印</text>
            <text class="form-section__hint">生成支持水印的图片时添加平台水印</text>
          </view>
          <AbSwitch v-model="form.watermark" />
        </view>
      </view>
    </view>

    <!-- Fixed bottom button -->
    <view class="bottom-bar">
      <AbButton
        type="primary"
        block
        :disabled="!canSubmit"
        :loading="submitting"
        @click="handleSubmit"
      >
        {{ isEditing ? '保存修改' : '创建计划' }}
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { onLoad } from '@dcloudio/uni-app'
import { agentProfilesApi } from '@/api/agent-profiles'
import { billingApi } from '@/api/billing'
import { plansApi } from '@/api/plans'
import { projectsApi } from '@/api/projects'
import { resourcesApi } from '@/api/resources'
import { imageCapabilitiesApi } from '@/api/image-capabilities'
import { contentTypeLabel } from '@/utils/labels'
import {
  resolveExecutionProfileSelection,
  taskPriceForExecutionProfile,
} from '@/utils/execution-profiles'
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
import ProjectSelector from '@/components/business/ProjectSelector.vue'
import ImageGenerationToolbar from '@/components/business/ImageGenerationToolbar.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'
import SchedulePicker from '@/components/business/SchedulePicker.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import ExecutionProfileSelector from '@/components/business/ExecutionProfileSelector.vue'

// --- Form state ---
// Mirrors studio planSchema (plan Zod schema). Fields use camelCase internally
// and are mapped to snake_case on submit (CreatePlanRequest payload).
const form = reactive({
  projectId: '',
  executionProfile: '' as AgentExecutionProfileID | '',
  cronExpr: '0 9 * * 1,3,5',
  prompt: '',
  weekdays: [] as number[],
  imageCapabilityKey: '',
  imageRatio: 'auto',
  visual_style: '',
  writer_key: '',
  theme: '',
  byline: '',
  writing_voice: '',
  persona_avatar: '',
  hasContentImage: true,
  hasTailImage: false,
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true → legacy "always generate both" (zero regression).
  articleWithCover: true,
  articleWithContentImages: true,
  skipReferenceImage: false,
  referenceImage: null as ReferenceImageSelection | null,
  watermark: false,
})

const selectedTime = ref('09:00')
const errors = reactive({
  projectId: '',
  cronExpr: '',
})

const submitting = ref(false)
const pageLoading = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => !!editingId.value)
const referenceUploading = ref(false)
const referencePreviewUrl = ref('')
const executionProfiles = ref<AgentExecutionProfileCapability[]>([])
const executionProfilesLoading = ref(false)
const executionProfilesError = ref('')
const catalog = ref<BillingCatalog | null>(null)

const selectedProject = ref<Project | null>(null)
const imageCapabilities = ref<ImageCapabilityOption[]>([])
const imageCapabilitiesLoading = ref(false)
const defaultImageCapability = ref('')
const platformConfigs = ref<PlatformConfig[]>([])
// Platform-dependent resources, loaded on project change.
const platformThemes = ref<ResourceEntry[]>([])

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
        form.referenceImage = { upload_session_id: uploaded.upload_session_id }
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
  form.referenceImage = null
  referencePreviewUrl.value = ''
}

// Weekday labels (starting from Sunday = 0)
const weekdays = ['日', '一', '二', '三', '四', '五', '六']

// --- Platform flags (mirror task-create pattern) ---
const platform = computed(() => selectedProject.value?.platform || '')
const selectedCapability = computed(() => imageCapabilities.value.find((item) => item.key === form.imageCapabilityKey))
const imageCapabilityUnavailable = computed(() =>
  !!form.imageCapabilityKey && (
    !selectedCapability.value
    || selectedCapability.value.enabled !== true
    || selectedCapability.value.price_available !== true
  ),
)
const allowedImageRatios = computed(() => platformConfigs.value.find((item) => item.id === platform.value)?.supported_image_ratios || [])
const isArticle = computed(() => platform.value === 'article')
const isSeednote = computed(() => platform.value === 'seednote')
const isEcommerce = computed(() => platform.value === 'ecommerce')
// Plans only support seednote / article (matches studio's planSchema enum).
const planUnsupported = computed(() => !!selectedProject.value && !isArticle.value && !isSeednote.value)
const resolvedPlanPrice = computed(() => {
  if (!selectedProject.value || !form.executionProfile) return undefined
  return taskPriceForExecutionProfile(
    catalog.value,
    selectedProject.value.platform,
    form.executionProfile,
  )
})
const selectedExecutionProfileAvailable = computed(() => {
  return executionProfiles.value.some((profile) =>
    profile.id === form.executionProfile && profile.available,
  )
})
const canSubmit = computed(() => {
  if (!form.projectId || !selectedProject.value || planUnsupported.value) return false
  if (!form.cronExpr.trim()) return false
  if (!form.executionProfile || !selectedExecutionProfileAvailable.value) return false
  if (imageCapabilityUnavailable.value) return false
  if (resolvedPlanPrice.value === undefined) return false
  return !submitting.value && !referenceUploading.value && !executionProfilesLoading.value
})

const selectedThemeName = computed(() => {
  const t = platformThemes.value.find((x) => x.name === form.theme)
  return t?.display_name || t?.name || form.theme || ''
})

// Show weekday selector when cron uses day-of-week field
const showWeekdaySelect = computed(() => {
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length < 5) return false
  const dow = parts[4]
  return dow !== '*' && dow !== '?'
})

// --- Time picker ---
function onTimeChange(e: any) {
  const time = e.detail.value as string
  selectedTime.value = time
  updateCronWithTime(time)
}

function updateCronWithTime(time: string) {
  const [hour, minute] = time.split(':')
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length >= 5) {
    parts[0] = minute
    parts[1] = hour
    form.cronExpr = parts.join(' ')
  }
}

// --- Weekday toggle ---
function toggleWeekday(index: number) {
  const idx = form.weekdays.indexOf(index)
  if (idx >= 0) {
    form.weekdays.splice(idx, 1)
  } else {
    form.weekdays.push(index)
    form.weekdays.sort((a, b) => a - b)
  }
  updateCronWithWeekdays()
}

function updateCronWithWeekdays() {
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length < 5) return
  if (form.weekdays.length === 0) {
    parts[4] = '*'
  } else {
    parts[4] = form.weekdays.join(',')
  }
  form.cronExpr = parts.join(' ')
}

// --- Project change ---
function onProjectChange(project: Project) {
  selectedProject.value = project
  form.projectId = project.id
  form.imageRatio = project.image_ratio || 'auto'
  form.imageCapabilityKey = project.ecommerce_defaults?.image_capability_key || defaultImageCapability.value
  errors.projectId = ''
  ensureExecutionProfileSelection()
  loadPlatformResources()
}

function ensureExecutionProfileSelection() {
  if (!selectedProject.value) return
  form.executionProfile = resolveExecutionProfileSelection(
    form.executionProfile,
    isEditing.value,
    executionProfiles.value,
    catalog.value,
    selectedProject.value.platform,
  )
}

// Loads platform resources for the selected project.
async function loadPlatformResources() {
  if (!selectedProject.value) return
  const plat = selectedProject.value.platform
  // Themes (article only)
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

// --- Theme picker (article) ---
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

// --- Validation ---
function validate(): boolean {
  errors.projectId = ''
  errors.cronExpr = ''

  if (!form.projectId) {
    errors.projectId = '请选择账号'
    uni.showToast({ title: '请选择账号', icon: 'none' })
    return false
  }

  if (imageCapabilityUnavailable.value) {
    uni.showToast({ title: '该图像能力已停用，请重新选择', icon: 'none' })
    return false
  }

  // Plans only support seednote / article (matches studio planSchema).
  if (planUnsupported.value) {
    uni.showToast({ title: '该账号类型暂不支持计划，请选公众号/种草笔记', icon: 'none' })
    return false
  }

  if (!form.cronExpr.trim()) {
    errors.cronExpr = '请设置执行频率'
    uni.showToast({ title: '请设置执行频率', icon: 'none' })
    return false
  }

  if (
    !form.executionProfile
    || !selectedExecutionProfileAvailable.value
    || resolvedPlanPrice.value === undefined
  ) {
    uni.showToast({ title: '请选择可用的执行配置', icon: 'none' })
    return false
  }

  return true
}

// --- Submit ---
// Builds a full CreatePlanRequest mirroring studio's onSubmit.
async function handleSubmit() {
  if (!canSubmit.value || referenceUploading.value) return
  if (!validate()) return
  if (!selectedProject.value) return
  const executionProfile = form.executionProfile
  if (!executionProfile) return

  submitting.value = true
  try {
    const cronExpr = form.cronExpr.trim()
    const plat = selectedProject.value.platform as 'seednote' | 'article'

    const payload = {
      type: plat,
      execution_profile: executionProfile,
      cron_expr: cronExpr,
      prompt: form.prompt.trim() || undefined,
      project_id: form.projectId,
      image_capability_key: form.imageCapabilityKey || undefined,
      image_ratio: form.imageRatio,
      visual_style: form.visual_style.trim() || undefined,
      writer_key: isArticle.value && form.writer_key.trim() ? form.writer_key.trim() : undefined,
      theme: isArticle.value && form.theme ? form.theme : undefined,
      byline: form.byline.trim() || undefined,
      writing_voice: form.writing_voice.trim() || undefined,
      persona_avatar: form.persona_avatar.trim() || undefined,
      watermark: form.watermark || undefined,
      has_content_image: isSeednote.value ? form.hasContentImage : undefined,
      has_tail_image: isSeednote.value ? form.hasTailImage : undefined,
      // Article image toggles: send actual boolean (incl. false when toggled off);
      // non-article omits. Both default true → server honors the explicit false.
      article_with_cover: isArticle.value ? form.articleWithCover : undefined,
      article_with_content_images: isArticle.value ? form.articleWithContentImages : undefined,
      skip_reference_image: form.skipReferenceImage || undefined,
      reference_image: form.referenceImage,
    }

    if (isEditing.value && editingId.value) {
      await plansApi.update(editingId.value, payload)
      uni.showToast({ title: '修改成功', icon: 'none' })
    } else {
      await plansApi.create(payload)
      uni.showToast({ title: '创建成功', icon: 'none' })
    }

    setTimeout(() => {
      uni.navigateBack()
    }, 500)
  } catch (err: any) {
    const msg = err?.message || (isEditing.value ? '修改失败' : '创建失败')
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

// --- Edit mode: load existing plan ---
async function loadPlan(planId: string) {
  pageLoading.value = true
  try {
    const plan = await plansApi.get(planId)
    form.projectId = plan.project_id || ''
    form.executionProfile = plan.execution_profile
    form.cronExpr = plan.cron_expr || '0 9 * * 1,3,5'
    form.prompt = plan.prompt || ''
    form.imageCapabilityKey = plan.image_capability_key || ''
    form.imageRatio = plan.image_ratio || 'auto'
    form.visual_style = plan.visual_style || ''
    form.writer_key = plan.writer_key || ''
    form.theme = plan.theme || ''
    form.byline = plan.byline || ''
    form.writing_voice = plan.writing_voice || ''
    form.persona_avatar = plan.persona_avatar || ''
    form.hasContentImage = plan.has_content_image ?? true
    form.hasTailImage = plan.has_tail_image ?? false
    form.articleWithCover = plan.article_with_cover ?? true
    form.articleWithContentImages = plan.article_with_content_images ?? true
    form.skipReferenceImage = plan.skip_reference_image || false
    form.referenceImage = plan.reference_image?.asset_id
      ? { asset_id: plan.reference_image.asset_id }
      : null
    referencePreviewUrl.value = plan.reference_image?.download_url || ''
    form.watermark = plan.watermark || false

    // Extract time from cron
    const parts = (plan.cron_expr || '').trim().split(/\s+/)
    if (parts.length >= 2) {
      selectedTime.value = `${parts[1].padStart(2, '0')}:${parts[0].padStart(2, '0')}`
    }

    // Extract weekdays from cron day-of-week field
    if (parts.length >= 5 && parts[4] !== '*' && parts[4] !== '?') {
      const dowParts = parts[4].split(/[,-]/)
      form.weekdays = dowParts
        .map((d) => parseInt(d.trim(), 10))
        .filter((d) => !isNaN(d) && d >= 0 && d <= 6)
    }

    // Resolve project so platform-dependent sections (article persona/theme,
    // seednote image composition) render correctly when editing.
    if (plan.project_id) {
      try {
        // selectedProject drives the isArticle/isSeednote computed flags.
        const list = await projectsApi.list({ status: 'active' })
        const proj = list.find((p) => p.id === plan.project_id) || null
        if (proj) {
          selectedProject.value = proj
          await loadPlatformResources()
        }
      } catch {
        // Non-critical: form still submits with project_id alone.
      }
    }

    uni.setNavigationBarTitle({ title: '编辑计划' })
  } catch (err) {
    console.error('Failed to load plan:', err)
    uni.showToast({ title: '加载计划失败', icon: 'none' })
  } finally {
    pageLoading.value = false
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
    if (!form.imageCapabilityKey) form.imageCapabilityKey = defaultImageCapability.value
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

// --- Page lifecycle ---
onLoad((query) => {
  void loadExecutionConfiguration()
  void loadImageCapabilities()
  void loadPlatformConfigs()
  if (query?.id) {
    editingId.value = query.id
    loadPlan(query.id)
  }
})
</script>

<style lang="scss" scoped>
.plan-create {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 160rpx;
  box-sizing: border-box;
}

// Form
.form {
  display: flex;
  flex-direction: column;
  gap: $ab-space-md;
}

.form-section {
  display: flex;
  flex-direction: column;
  gap: $ab-space-xs;

  &__label {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__sublabel {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    display: block;
  }

  &__required {
    color: $ab-danger;
  }

  &__hint {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    line-height: 1.4;
  }

  &__error {
    font-size: $ab-text-xs;
    color: $ab-danger;
    line-height: 1.4;
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

// Locked field (content type follows project platform)
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

// Picker row (template / theme)
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

// Time picker
.time-picker {
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
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}

// Weekday grid
.weekday-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: $ab-space-xs;
}

.weekday-item {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 72rpx;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-surface;

  &.active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;

    .active & {
      color: $ab-primary;
      font-weight: $ab-font-medium;
    }
  }
}

// Bottom bar
.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-sm $ab-space-md;
  padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom, 0px));
  background-color: $ab-surface;
  box-shadow: $ab-shadow-md;
  z-index: 10;
}

.notice-bar {
  background-color: $ab-warning-bg;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;

  &__text {
    font-size: $ab-text-xs;
    color: $ab-warning;
    line-height: 1.5;
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
</style>
