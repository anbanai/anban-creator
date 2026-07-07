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

    <!-- Template (optional) -->
    <view class="task-create__section">
      <text class="field-label">使用模板</text>
      <view class="picker-row" @tap="pickTemplate">
        <text v-if="selectedTemplateName" class="picker-row__value">{{ selectedTemplateName }}</text>
        <text v-else class="picker-row__placeholder">可选，套用预设风格</text>
        <text v-if="form.template_id" class="picker-row__clear" @tap.stop="clearTemplate">清除</text>
        <text v-else class="picker-row__arrow">›</text>
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
      <view class="inspiration-hint" @tap="applyInspiration">
        <text class="inspiration-hint__icon">灵</text>
        <text class="inspiration-hint__text">试试: {{ currentInspiration }}</text>
      </view>
    </view>

    <!-- Image model -->
    <view class="task-create__section">
      <text class="field-label">图片模型 <text class="field-required" v-if="hasImageModels">*</text></text>
      <ImageModelSelector v-model="form.image_model_key" placeholder="选择生图模型" />
      <text v-if="errors.image_model_key" class="field-error">{{ errors.image_model_key }}</text>
    </view>

    <view class="task-create__advanced-toggle" @tap="advancedOpen = !advancedOpen">
      <view class="task-create__advanced-toggle-main">
        <text class="task-create__advanced-toggle-title">高级控制</text>
        <text class="task-create__advanced-toggle-desc">视觉、人设、参考图和强目标模式</text>
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
      <text class="field-hint">上传产品图，选择交付模块。创建只扣基础任务费，后续图片生成和理解按实际用量结算。</text>

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
                <text v-if="modulePrice(mod.key) != null" class="module-row__price">{{ modulePrice(mod.key) }} 积分/{{ mod.qtyLabel }}</text>
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

    <!-- Image ratio selector -->
    <view class="task-create__section" v-if="advancedOpen">
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
        <AbInput
          v-model="form.reference_image_url"
          placeholder="可选，输入图片 URL 覆盖账号默认参考图"
        />
      </view>

      <view class="switch-row field-spacer">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">图片水印</text>
          <text class="field-hint">生成支持水印的图片时添加平台水印</text>
        </view>
        <AbSwitch v-model="form.watermark" />
      </view>
    </view>

    <!-- Goal mode -->
    <view class="task-create__section" v-if="advancedOpen && !isEcommerce">
      <view class="switch-row">
        <view class="switch-row__text">
          <text class="field-label switch-row__label">强目标模式</text>
          <text class="field-hint">设定明确的成功标准，循环优化直到达标</text>
        </view>
        <AbSwitch v-model="form.goal_mode" />
      </view>
      <view class="field-spacer" v-if="form.goal_mode">
        <text class="field-label">成功目标 <text class="field-required">*</text></text>
        <AbTextarea
          v-model="form.goal"
          placeholder="描述明确的完成标准，如：字数 800+、包含 3 个小标题、CTA 引导关注..."
          :rows="3"
          :maxlength="4000"
          :error="errors.goal"
        />
      </view>
    </view>

    <!-- Credit info -->
    <view class="task-create__section">
      <view class="credit-info">
        <view class="credit-info__row">
          <text class="credit-info__label">基础任务费</text>
          <text class="credit-info__value credit-info__value--cost">约 {{ estimatedCost }} 积分</text>
        </view>
        <view class="credit-info__row">
          <text class="credit-info__label">Claude运行预留</text>
          <text class="credit-info__value credit-info__value--cost">约 {{ runtimeReserve }} 积分</text>
        </view>
        <view class="credit-info__row">
          <text class="credit-info__label">当前余额</text>
          <text class="credit-info__value" :class="{ 'credit-info__value--low': balance < creationCost }">
            {{ balance.toLocaleString() }} 积分
          </text>
        </view>
      </view>
      <text class="field-hint">Claude Code 运行预留会在执行完成后按实际 token 多退少补。</text>
      <text v-if="!isEcommerce" class="field-hint">模型、图片、视频等 MCP 操作费用按实际用量另计。</text>
      <text v-else class="field-hint">交付模块会影响后续图片生成和理解操作用量，最终以交易明细汇总为准。</text>
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
        开始创作
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { onLoad, onShow } from '@dcloudio/uni-app'
import type { Project, CreditPricing, Template, ResourceEntry } from '@/types'
import { tasksApi } from '@/api/tasks'
import { templatesApi } from '@/api/templates'
import { resourcesApi } from '@/api/resources'
import { creditsApi } from '@/api/credits'
import { projectsApi } from '@/api/projects'
import { TASK_QUANTITIES, IMAGE_RATIOS } from '@/utils/constants'
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
import ImageModelSelector from '@/components/business/ImageModelSelector.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

const inspirations = [
  '写一篇关于春季护肤的笔记，风格温暖自然',
  '分享5个提升效率的办公好物，搭配实拍图',
  '做一期测评：对比3款热门面霜的真实体验',
  '分享一周穿搭灵感，适合通勤和约会',
]

const TARGET_PLATFORM_LABELS = ecommerceTargetPlatformOptions.map((o) => o.label)
const LANGUAGE_LABELS = ecommerceLanguageOptions.map((o) => o.label)
const DEFAULT_TASK_COSTS: Record<string, number> = {
  article: 4000,
  seednote: 3600,
  ecommerce: 3000,
  video: 2000,
  viral_analysis: 1200,
}

const selectedProject = ref<Project | null>(null)
const balance = ref(0)
const pricing = ref<CreditPricing | null>(null)
const submitting = ref(false)
const inspirationIndex = ref(0)
const requestedType = ref('')
const prefillProjectId = ref('')
const advancedOpen = ref(false)
const projectSelectorRef = ref<{ refresh?: () => void } | null>(null)

// Loaded on platform change
const platformTemplates = ref<Template[]>([])
const platformThemes = ref<ResourceEntry[]>([])
const hasImageModels = ref(true)

const form = reactive({
  project_id: '',
  prompt: '',
  quantity: 1,
  image_ratio: '3:4',
  image_model_key: '',
  visual_style: '',
  writer_key: '',
  theme: '',
  byline: '',
  writing_voice: '',
  persona_avatar: '',
  goal_mode: false,
  goal: '',
  template_id: '',
  template_name: '',
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
  reference_image_url: '',
  watermark: false,
})

const errors = reactive<Record<string, string>>({})

const platform = computed(() => selectedProject.value?.platform || '')
const isArticle = computed(() => platform.value === 'article')
const isSeednote = computed(() => platform.value === 'seednote')
const isEcommerce = computed(() => platform.value === 'ecommerce')
const billableGoalMode = computed(() => !isEcommerce.value && form.goal_mode)

// ---- E-commerce: product photos + delivery modules ----
const uploadingPhoto = ref(false)

function modulePrice(key: string): number | undefined {
  return pricing.value?.ecommerce_module_prices?.[key]
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

const selectedTemplateName = computed(() => form.template_name)
const selectedThemeName = computed(() => {
  const t = platformThemes.value.find((x) => x.name === form.theme)
  return t?.display_name || t?.name || form.theme || ''
})

const estimatedCost = computed(() => {
  if (!selectedProject.value) return 0
  const costPerTask = pricing.value?.task_costs?.[selectedProject.value.platform]
    ?? DEFAULT_TASK_COSTS[selectedProject.value.platform]
    ?? 3600
  const multiplier = billableGoalMode.value ? 3 : 1
  const billableQuantity = isEcommerce.value ? 1 : form.quantity
  return costPerTask * billableQuantity * multiplier
})

const runtimeReserve = computed(() => {
  if (!selectedProject.value) return 0
  const reservePerTask = pricing.value?.agent_runtime_reserve?.[selectedProject.value.platform]
    ?? DEFAULT_TASK_COSTS[selectedProject.value.platform]
    ?? 0
  const multiplier = billableGoalMode.value ? 3 : 1
  const billableQuantity = isEcommerce.value ? 1 : form.quantity
  return reservePerTask * billableQuantity * multiplier
})

const creationCost = computed(() => estimatedCost.value + runtimeReserve.value)

const canSubmit = computed(() => {
  if (!form.project_id || !form.prompt.trim()) return false
  if (billableGoalMode.value && !form.goal.trim()) return false
  if (balance.value < creationCost.value) return false
  return !submitting.value
})

function onProjectChange(project: Project) {
  selectedProject.value = project
  form.project_id = project.id

  if (project.image_ratio) {
    form.image_ratio = project.image_ratio
  } else {
    const defaults: Record<string, string> = {
      article: '16:9',
      seednote: '3:4',
      ecommerce: '1:1',
      xls: '3:4',
    }
    form.image_ratio = defaults[project.platform] || '3:4'
  }

  delete errors.project
  loadPlatformResources()
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
  // Templates for this platform/type
  try {
    const res = await templatesApi.list({ type: plat, limit: 50 })
    platformTemplates.value = res.items || []
  } catch {
    platformTemplates.value = []
  }
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

function pickTemplate() {
  if (platformTemplates.value.length === 0) {
    uni.showToast({ title: '暂无可用模板', icon: 'none' })
    return
  }
  const labels = platformTemplates.value.map((t) => t.name)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const tpl = platformTemplates.value[res.tapIndex]
      if (tpl) {
        applyTemplate(tpl)
      }
    },
  })
}

function applyTemplate(tpl: Template) {
  form.template_id = tpl.id
  form.template_name = tpl.name
  if (tpl.style_prompt) form.visual_style = tpl.style_prompt
  if (tpl.writer_key) form.writer_key = tpl.writer_key
  if (tpl.theme) form.theme = tpl.theme
  if (tpl.author_name) form.byline = tpl.author_name
  if (tpl.writing_voice) form.writing_voice = tpl.writing_voice
  if (tpl.persona_avatar) form.persona_avatar = tpl.persona_avatar
  if (tpl.ecommerce?.image_model_key) form.image_model_key = tpl.ecommerce.image_model_key
  if (tpl.ecommerce?.default_selected_modules) {
    form.selected_modules = { ...tpl.ecommerce.default_selected_modules }
  }
  if (tpl.ecommerce?.target_platform) form.target_platform = tpl.ecommerce.target_platform
  if (tpl.ecommerce?.brand_brief && !form.selling_points) {
    form.selling_points = tpl.ecommerce.brand_brief
  }
  if (
    tpl.style_prompt ||
    tpl.writer_key ||
    tpl.theme ||
    tpl.author_name ||
    tpl.writing_voice ||
    tpl.persona_avatar ||
    tpl.ecommerce
  ) {
    advancedOpen.value = true
  }
  delete errors.prompt
}

async function applyTemplateFromId(templateId: string) {
  if (!templateId) return
  form.template_id = templateId
  try {
    const tpl = await templatesApi.get(templateId)
    applyTemplate(tpl)
  } catch (err) {
    console.error('Failed to prefill template:', err)
    uni.showToast({ title: '模板加载失败，请手动选择', icon: 'none' })
  }
}

function clearTemplate() {
  form.template_id = ''
  form.template_name = ''
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
  if (billableGoalMode.value && !form.goal.trim()) {
    errors.goal = '强目标模式需填写成功目标'
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
  if (balance.value < creationCost.value) {
    uni.showToast({ title: '积分不足，请先充值', icon: 'none' })
    return false
  }
  return true
}

async function onSubmit() {
  if (!validate()) return
  if (!selectedProject.value) return

  submitting.value = true
  try {
    const task = await tasksApi.create({
      type: selectedProject.value.platform as any,
      project_id: form.project_id,
      prompt: form.prompt.trim(),
      quantity: isEcommerce.value ? undefined : form.quantity,
      image_ratio: form.image_ratio,
      image_model_key: form.image_model_key || undefined,
      visual_style: form.visual_style.trim() || undefined,
      writer_key: isArticle.value && form.writer_key.trim() ? form.writer_key.trim() : undefined,
      theme: isArticle.value && form.theme ? form.theme : undefined,
      template_id: form.template_id || undefined,
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
      goal: billableGoalMode.value && form.goal.trim() ? form.goal.trim() : undefined,
      goal_mode: billableGoalMode.value || undefined,
      skip_reference_image: form.skip_reference_image || undefined,
      reference_image_url: form.reference_image_url.trim() || undefined,
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

function safeDecodeQuery(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

onLoad((query) => {
  if (query?.type) {
    requestedType.value = String(query.type)
  }
  if (query?.prompt) {
    form.prompt = safeDecodeQuery(String(query.prompt))
  }
  if (query?.project_id) {
    void applyProjectById(String(query.project_id))
  }
  if (query?.template_id) {
    void applyTemplateFromId(String(query.template_id))
  }
})

onShow(() => {
  projectSelectorRef.value?.refresh?.()
})

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
