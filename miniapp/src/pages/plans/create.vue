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
      </view>

      <!-- Template (optional) -->
      <view class="form-section" v-if="!isEcommerce">
        <text class="form-section__label">使用模板</text>
        <view class="picker-row" @tap="pickTemplate">
          <text v-if="selectedTemplateName" class="picker-row__value">{{ selectedTemplateName }}</text>
          <text v-else class="picker-row__placeholder">可选，套用预设风格</text>
          <text v-if="form.templateId" class="picker-row__clear" @tap.stop="clearTemplate">清除</text>
          <text v-else class="picker-row__arrow">›</text>
        </view>
      </view>

      <!-- Image model -->
      <view class="form-section">
        <text class="form-section__label">图片模型</text>
        <ImageModelSelector v-model="form.imageModelKey" placeholder="选择生图模型" />
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
          <AbInput
            v-model="form.referenceImageUrl"
            placeholder="可选，输入图片 URL 覆盖账号默认参考图"
          />
        </view>

        <view class="switch-row field-spacer">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">图片水印</text>
            <text class="form-section__hint">生成支持水印的图片时添加平台水印</text>
          </view>
          <AbSwitch v-model="form.watermark" />
        </view>
      </view>

      <!-- Goal mode -->
      <view class="form-section">
        <view class="switch-row">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">强目标模式</text>
            <text class="form-section__hint">设定明确的成功标准，循环优化直到达标</text>
          </view>
          <AbSwitch v-model="form.goalMode" />
        </view>
        <view class="field-spacer" v-if="form.goalMode">
          <text class="form-section__label">成功目标 <text class="form-section__required">*</text></text>
          <AbTextarea
            v-model="form.goal"
            placeholder="描述明确的完成标准，如：字数 800+、包含 3 个小标题、CTA 引导关注..."
            :rows="3"
            :maxlength="4000"
            :error="errors.goal"
          />
        </view>
      </view>
    </view>

    <!-- Fixed bottom button -->
    <view class="bottom-bar">
      <AbButton
        type="primary"
        block
        :loading="submitting"
        @click="handleSubmit"
      >
        {{ isEditing ? '保存修改' : '创建计划' }}
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { onLoad } from '@dcloudio/uni-app'
import { plansApi } from '@/api/plans'
import { projectsApi } from '@/api/projects'
import { templatesApi } from '@/api/templates'
import { resourcesApi } from '@/api/resources'
import { contentTypeLabel } from '@/utils/labels'
import type { Project, Template, ResourceEntry } from '@/types'
import ProjectSelector from '@/components/business/ProjectSelector.vue'
import ImageModelSelector from '@/components/business/ImageModelSelector.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'
import SchedulePicker from '@/components/business/SchedulePicker.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbLoading from '@/components/common/AbLoading.vue'

// --- Form state ---
// Mirrors studio planSchema (plan Zod schema). Fields use camelCase internally
// and are mapped to snake_case on submit (CreatePlanRequest payload).
const form = reactive({
  projectId: '',
  cronExpr: '0 9 * * 1,3,5',
  prompt: '',
  weekdays: [] as number[],
  imageModelKey: '',
  visual_style: '',
  writer_key: '',
  theme: '',
  byline: '',
  writing_voice: '',
  persona_avatar: '',
  hasContentImage: true,
  hasTailImage: false,
  skipReferenceImage: false,
  referenceImageUrl: '',
  watermark: false,
  goalMode: false,
  goal: '',
  templateId: '',
})

const selectedTime = ref('09:00')
const errors = reactive({
  projectId: '',
  cronExpr: '',
  goal: '',
})

const submitting = ref(false)
const pageLoading = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => !!editingId.value)

const selectedProject = ref<Project | null>(null)
// Platform-dependent resources, loaded on project change.
const platformTemplates = ref<Template[]>([])
const platformThemes = ref<ResourceEntry[]>([])

// Weekday labels (starting from Sunday = 0)
const weekdays = ['日', '一', '二', '三', '四', '五', '六']

// --- Platform flags (mirror task-create pattern) ---
const platform = computed(() => selectedProject.value?.platform || '')
const isArticle = computed(() => platform.value === 'article')
const isSeednote = computed(() => platform.value === 'seednote')
const isEcommerce = computed(() => platform.value === 'ecommerce')
// Plans only support seednote / article (matches studio's planSchema enum).
const planUnsupported = computed(() => !!selectedProject.value && !isArticle.value && !isSeednote.value)

const selectedTemplateName = computed(() => {
  const t = platformTemplates.value.find((x) => x.id === form.templateId)
  return t?.name || ''
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
  errors.projectId = ''
  loadPlatformResources()
}

// Loads templates + themes for the selected platform. Mirrors task-create.
async function loadPlatformResources() {
  if (!selectedProject.value) return
  const plat = selectedProject.value.platform
  // Templates
  try {
    const res = await templatesApi.list({ type: plat, limit: 50 })
    platformTemplates.value = res.items || []
  } catch {
    platformTemplates.value = []
  }
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

// --- Template picker ---
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
      if (!tpl) return
      // Re-selecting the same template is a no-op (preserve edits, mirror studio).
      if (form.templateId === tpl.id) return
      form.templateId = tpl.id
      // Template → 3 dimensions + persona. Mirrors studio handleTemplateSelect.
      if (tpl.style_prompt) form.visual_style = tpl.style_prompt
      if (tpl.writer_key) form.writer_key = tpl.writer_key
      if (tpl.theme) form.theme = tpl.theme
      if (tpl.author_name) form.byline = tpl.author_name
      if (tpl.writing_voice) form.writing_voice = tpl.writing_voice
      if (tpl.persona_avatar) form.persona_avatar = tpl.persona_avatar
    },
  })
}

function clearTemplate() {
  form.templateId = ''
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
  errors.goal = ''

  if (!form.projectId) {
    errors.projectId = '请选择账号'
    uni.showToast({ title: '请选择账号', icon: 'none' })
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

  if (form.goalMode && !form.goal.trim()) {
    errors.goal = '强目标模式需填写成功目标'
    uni.showToast({ title: '强目标模式需填写成功目标', icon: 'none' })
    return false
  }

  return true
}

// --- Submit ---
// Builds a full CreatePlanRequest mirroring studio's onSubmit.
async function handleSubmit() {
  if (submitting.value) return
  if (!validate()) return
  if (!selectedProject.value) return

  submitting.value = true
  try {
    const cronExpr = form.cronExpr.trim()
    const plat = selectedProject.value.platform as 'seednote' | 'article'

    const payload = {
      type: plat,
      cron_expr: cronExpr,
      prompt: form.prompt.trim() || undefined,
      project_id: form.projectId,
      image_model_key: form.imageModelKey || undefined,
      visual_style: form.visual_style.trim() || undefined,
      writer_key: isArticle.value && form.writer_key.trim() ? form.writer_key.trim() : undefined,
      theme: isArticle.value && form.theme ? form.theme : undefined,
      byline: form.byline.trim() || undefined,
      writing_voice: form.writing_voice.trim() || undefined,
      persona_avatar: form.persona_avatar.trim() || undefined,
      watermark: form.watermark || undefined,
      goal_mode: form.goalMode || undefined,
      goal: form.goalMode && form.goal.trim() ? form.goal.trim() : undefined,
      has_content_image: isSeednote.value ? form.hasContentImage : undefined,
      has_tail_image: isSeednote.value ? form.hasTailImage : undefined,
      skip_reference_image: form.skipReferenceImage || undefined,
      reference_image_url: form.referenceImageUrl.trim() || undefined,
      template_id: form.templateId || undefined,
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
    form.cronExpr = plan.cron_expr || '0 9 * * 1,3,5'
    form.prompt = plan.prompt || ''
    form.imageModelKey = plan.image_model_key || ''
    form.visual_style = plan.visual_style || ''
    form.writer_key = plan.writer_key || ''
    form.theme = plan.theme || ''
    form.byline = plan.byline || ''
    form.writing_voice = plan.writing_voice || ''
    form.persona_avatar = plan.persona_avatar || ''
    form.hasContentImage = plan.has_content_image ?? true
    form.hasTailImage = plan.has_tail_image ?? false
    form.skipReferenceImage = plan.skip_reference_image || false
    form.referenceImageUrl = plan.reference_image_url || ''
    form.watermark = plan.watermark || false
    form.goalMode = plan.goal_mode || false
    form.goal = plan.goal || ''
    form.templateId = plan.template_id || ''

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

// --- Page lifecycle ---
onLoad((query) => {
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
</style>
