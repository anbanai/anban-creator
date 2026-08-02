<template>
  <view class="page project-detail">
    <AbLoading v-if="pageLoading && !isNew" text="加载中" />

    <view v-else>
      <!-- Step indicator (new mode only) -->
      <view v-if="isNew" class="project-detail__steps">
        <view
          v-for="(s, i) in steps"
          :key="i"
          class="project-detail__step"
          :class="{
            'project-detail__step--active': currentStep === i,
            'project-detail__step--done': currentStep > i,
          }"
        >
          <view class="project-detail__step-dot">
            <text v-if="currentStep > i" class="project-detail__step-check">✓</text>
            <text v-else>{{ i + 1 }}</text>
          </view>
          <text class="project-detail__step-label">{{ s }}</text>
        </view>
      </view>

      <!-- Step 1: Platform selection -->
      <view v-if="isNew && currentStep === 0" class="project-detail__content">
        <view class="project-detail__section-title">选择平台</view>

        <view
          v-for="p in platformOptions"
          :key="p.value"
          class="platform-card"
          :class="{ 'platform-card--selected': form.platform === p.value }"
          @tap="form.platform = p.value"
        >
          <view class="platform-card__indicator">
            <PlatformAvatar :platform="p.value" :size="48" />
            <view v-if="form.platform === p.value" class="platform-card__check">✓</view>
          </view>
          <view class="platform-card__info">
            <text class="platform-card__name">{{ p.label }}</text>
            <text class="platform-card__desc">{{ p.description }}</text>
          </view>
        </view>
      </view>

      <!-- Step 2: Basic info -->
      <view v-if="isNew ? currentStep === 1 : true" class="project-detail__content">
        <view v-if="isNew" class="project-detail__section-title">基础信息</view>
        <view v-else class="project-detail__collapsible-header" @tap="sections.basic = !sections.basic">
          <text class="project-detail__collapsible-title">基础信息</text>
          <text class="project-detail__collapsible-arrow">{{ sections.basic ? '收起' : '展开' }}</text>
        </view>

        <view v-if="isNew || sections.basic" class="project-detail__fields">
          <!-- Avatar upload -->
          <view class="field-group">
            <text class="field-label">账号头像</text>
            <view class="avatar-uploader" @tap="onChooseAvatar">
              <image
                v-if="form.avatar_url"
                :src="form.avatar_url"
                class="avatar-uploader__img"
                mode="aspectFill"
              />
              <view v-else class="avatar-uploader__placeholder">
                <PlatformAvatar v-if="form.platform" :platform="form.platform" :size="80" />
                <text v-else class="avatar-uploader__hint">点击上传</text>
              </view>
              <view v-if="avatarUploading" class="avatar-uploader__mask">
                <text>上传中…</text>
              </view>
            </view>
            <view v-if="form.avatar_url" class="avatar-uploader__actions">
              <text class="avatar-uploader__action" @tap="onChooseAvatar">更换</text>
              <text
                class="avatar-uploader__action avatar-uploader__action--danger"
                @tap="form.avatar_url = ''"
              >
                移除
              </text>
            </view>
            <text class="field-hint">支持从相册选择，或填写 URL；留空将使用平台默认头像。</text>
            <AbInput
              v-model="form.avatar_url"
              placeholder="或输入图片 URL"
              style="margin-top: 8rpx;"
            />
          </view>

          <!-- Profile URL (seednote only) -->
          <view v-if="isSeednote" class="field-group">
            <text class="field-label">账号链接</text>
            <view class="field-row">
              <AbInput
                v-model="form.profile_url"
                placeholder="粘贴种草笔记主页链接"
                :error="errors.profile_url"
              />
              <AbButton
                type="ghost"
                size="sm"
                :loading="fetchingProfile"
                @click="onFetchProfile"
                style="margin-left: 16rpx; flex-shrink: 0;"
              >
                自动拉取
              </AbButton>
            </view>
          </view>

          <!-- Name -->
          <view class="field-group">
            <text class="field-label">账号名称 <text class="field-required">*</text></text>
            <AbInput
              v-model="form.name"
              placeholder="输入账号名称"
              :error="errors.name"
            />
          </view>

          <!-- Positioning -->
          <view class="field-group">
            <text class="field-label">账号定位</text>
            <AbTextarea
              v-model="form.instructions"
              placeholder="一句话描述账号定位"
              :rows="2"
            />
          </view>

          <!-- Keywords -->
          <view class="field-group">
            <text class="field-label">关键词标签</text>
            <TagInput
              v-model="keywordList"
              placeholder="输入后按回车添加"
              :max-tags="10"
            />
          </view>

          <!-- Visual style -->
          <view class="field-group">
            <text class="field-label">视觉风格</text>
            <view class="style-block">
              <AbTextarea
                v-model="form.visual_style"
                :placeholder="stylePlaceholder"
                :rows="3"
              />
              <view v-if="analyzingStyle" class="style-block__analyzing">
                <text>正在分析参考图…</text>
              </view>
            </view>
            <text class="field-hint">{{ styleHint }}</text>
          </view>

          <!-- Reference image (seednote/ecommerce): upload + auto-analyze -->
          <view v-if="isSeednote || isEcommerce" class="field-group">
            <text class="field-label">参考图片</text>
            <view class="ref-uploader" @tap="onChooseReference">
              <image
                v-if="referencePreviewUrl"
                :src="referencePreviewUrl"
                class="ref-uploader__img"
                mode="aspectFill"
              />
              <view v-else class="ref-uploader__placeholder">
                <text class="ref-uploader__hint">点击上传参考图</text>
              </view>
              <view v-if="referenceUploading" class="ref-uploader__mask">
                <text>上传中…</text>
              </view>
            </view>
            <view v-if="referencePreviewUrl" class="ref-uploader__actions">
              <text class="ref-uploader__action" @tap="onAnalyzeReference">识别风格</text>
              <text
                class="ref-uploader__action ref-uploader__action--danger"
                @tap="clearReferenceImage"
              >
                清除
              </text>
            </view>
            <text class="field-hint">上传参考图后可自动识别视觉风格（种草笔记）。</text>
          </view>

          <view v-if="isEcommerce" class="field-group">
            <text class="field-label">默认图像能力</text>
            <ImageCapabilitySelector
              v-model="form.image_capability_key"
              :options="imageCapabilities"
              :loading="imageCapabilitiesLoading"
            />
          </view>

          <!-- Image ratio -->
          <view class="field-group">
            <text class="field-label">图片比例</text>
            <ImageAspectRatioField
              v-model="form.image_ratio"
              :ratios="selectedChannelConfig?.supported_image_ratios || []"
            />
          </view>
        </view>
      </view>

      <!-- Step 3: Publishing config -->
      <view v-if="isNew ? currentStep === 2 : true">
        <template v-if="!isSeednote">
          <view v-if="isNew" class="project-detail__section-title">发布配置</view>
          <view v-else class="project-detail__collapsible-header" @tap="sections.publishing = !sections.publishing">
            <text class="project-detail__collapsible-title">发布配置</text>
            <text class="project-detail__collapsible-arrow">{{ sections.publishing ? '收起' : '展开' }}</text>
          </view>

          <view v-if="isNew || sections.publishing" class="project-detail__fields">
            <!-- Auto-publish switch (article only — xls/ecommerce don't publish) -->
            <view v-if="isArticle" class="field-group">
              <view class="switch-row">
                <text class="field-label" style="margin-bottom: 0;">自动发布到微信</text>
                <AbSwitch v-model="form.enable_publishing" />
              </view>
              <text v-if="form.enable_publishing" class="field-hint">
                开启后任务完成将自动发布到公众号。
              </text>
            </view>

            <!-- WeChat credentials (shown when auto-publish is on) -->
            <template v-if="isArticle && form.enable_publishing">
              <view class="field-group">
                <text class="field-label">微信 AppID</text>
                <AbInput
                  v-model="form.wechat_app_id"
                  placeholder="输入微信公众号 AppID"
                />
              </view>

              <view class="field-group">
                <text class="field-label">微信 AppSecret</text>
                <AbInput
                  v-model="form.wechat_secret"
                  type="password"
                  :placeholder="isNew ? '输入微信公众号 AppSecret' : '留空则保持原有密钥不变'"
                />
                <text class="field-hint">安全提示：AppSecret 仅在服务端使用，请勿在公共设备上填写。</text>
              </view>
            </template>
          </view>
        </template>
      </view>

      <!-- Step 4: Persona + Advanced settings -->
      <view v-if="isNew ? currentStep === 3 : true">
        <view v-if="isNew" class="project-detail__section-title">高级设置（可选）</view>
        <view v-else class="project-detail__collapsible-header" @tap="sections.advanced = !sections.advanced">
          <text class="project-detail__collapsible-title">高级设置</text>
          <text class="project-detail__collapsible-arrow">{{ sections.advanced ? '收起' : '展开' }}</text>
        </view>

        <view v-if="isNew || sections.advanced" class="project-detail__fields">
          <!-- Template import (article / ecommerce): one-shot import of persona/style -->
          <view v-if="isArticle || isEcommerce" class="field-group">
            <text class="field-label">从模板导入</text>
            <view class="picker-row" @tap="pickTemplate">
              <text v-if="selectedTemplateName" class="picker-row__value">{{ selectedTemplateName }}</text>
              <text v-else class="picker-row__placeholder">可选，导入模板风格/人设</text>
              <text v-if="form.template_id" class="picker-row__clear" @tap.stop="clearTemplateImport">清除</text>
              <text v-else class="picker-row__arrow">›</text>
            </view>
            <text class="field-hint">选择模板后，视觉风格/作者/写作风格/排版将一次性填入，可继续编辑。</text>
          </view>

          <!-- Persona block (article / seednote) — byline ≠ writer_key persona -->
          <view v-if="isArticle || isSeednote" class="persona-block">
            <text class="field-label persona-block__title">作者人设</text>

            <view class="field-spacer">
              <text class="field-sublabel">作者署名</text>
              <AbInput v-model="form.byline" placeholder="显示在文章/笔记的作者名（≠ 写作风格）" />
            </view>

            <view class="field-spacer">
              <text class="field-sublabel">写作风格模仿</text>
              <AbTextarea
                v-model="form.writing_voice"
                placeholder="模仿某位作者/博主的文风，描述其语言习惯、句式特点…"
                :rows="2"
              />
            </view>

            <view class="field-spacer">
              <text class="field-sublabel">人设头像</text>
              <view class="ref-uploader ref-uploader--sm" @tap="onChooseAuthorAvatar">
                <image
                  v-if="form.persona_avatar"
                  :src="form.persona_avatar"
                  class="ref-uploader__img"
                  mode="aspectFill"
                />
                <view v-else class="ref-uploader__placeholder">
                  <text class="ref-uploader__hint">点击上传人设头像</text>
                </view>
                <view v-if="authorAvatarUploading" class="ref-uploader__mask">
                  <text>上传中…</text>
                </view>
              </view>
              <AbInput
                v-model="form.persona_avatar"
                placeholder="或输入图片 URL"
                style="margin-top: 8rpx;"
              />
            </view>
          </view>

          <!-- Theme (article) -->
          <view v-if="isArticle" class="field-group">
            <text class="field-label">排版主题</text>
            <AbSelect
              v-model="form.theme"
              :options="themeOptions"
              :disabled="resourcesLoading"
              placeholder="选择转换主题"
            />
          </view>

          <view v-if="isArticle" class="field-group">
            <text class="field-label">文章版式</text>
            <AbSelect
              v-model="form.layout"
              :options="layoutOptions"
              :disabled="resourcesLoading"
              placeholder="选择版式布局"
            />
          </view>

          <!-- Author name for seednote (article uses the persona block above) -->
          <view v-if="isSeednote" class="field-group">
            <text class="field-label">作者名</text>
            <AbInput v-model="form.byline" placeholder="署名" />
          </view>

        </view>
      </view>

      <!-- Stats (edit mode only) -->
      <view v-if="!isNew && stats" class="project-detail__content">
        <view class="project-detail__collapsible-header" @tap="sections.stats = !sections.stats">
          <text class="project-detail__collapsible-title">运行统计</text>
          <text class="project-detail__collapsible-arrow">{{ sections.stats ? '收起' : '展开' }}</text>
        </view>

        <view v-if="sections.stats" class="stats-grid">
          <view class="stats-cell">
            <text class="stats-cell__num">{{ stats.total_tasks ?? 0 }}</text>
            <text class="stats-cell__label">总任务</text>
          </view>
          <view class="stats-cell">
            <text class="stats-cell__num stats-cell__num--success">{{ stats.completed_tasks ?? 0 }}</text>
            <text class="stats-cell__label">已完成</text>
          </view>
          <view class="stats-cell">
            <text class="stats-cell__num stats-cell__num--danger">{{ stats.failed_tasks ?? 0 }}</text>
            <text class="stats-cell__label">失败</text>
          </view>
          <view class="stats-cell">
            <text class="stats-cell__num stats-cell__num--info">{{ stats.success_rate ?? 0 }}%</text>
            <text class="stats-cell__label">成功率</text>
          </view>
        </view>
      </view>

      <!-- Topic pool (edit mode only) -->
      <view v-if="!isNew" class="project-detail__content">
        <view class="project-detail__collapsible-header" @tap="sections.topics = !sections.topics">
          <text class="project-detail__collapsible-title">选题池</text>
          <text class="project-detail__collapsible-arrow">{{ sections.topics ? '收起' : '展开' }}</text>
        </view>

        <view v-if="sections.topics" class="project-detail__fields">
          <view class="field-group">
            <text class="field-label">批量添加选题</text>
            <AbTextarea
              v-model="newTopics"
              placeholder="每行一个选题"
              :rows="4"
            />
            <AbButton
              type="primary"
              size="md"
              :loading="topicSaving"
              :disabled="!newTopics.trim()"
              @click="addTopics"
            >
              添加选题
            </AbButton>
          </view>

          <view class="field-group">
            <text class="field-label">筛选</text>
            <AbSelect
              v-model="topicStatus"
              :options="topicStatusOptions"
              placeholder="筛选选题状态"
            />
          </view>

          <AbLoading v-if="topicsLoading" size="sm" text="加载选题" />

          <view v-else-if="topics.length === 0" class="topic-empty">
            <text>暂无选题</text>
          </view>

          <view v-else class="topic-list">
            <view v-for="topic in topics" :key="topic.id" class="topic-item">
              <view class="topic-item__body">
                <text class="topic-item__title">{{ topic.topic }}</text>
                <view class="topic-item__meta">
                  <AbBadge :variant="topic.status === 'unused' ? 'info' : 'neutral'" size="sm">
                    {{ topic.status === 'unused' ? '未使用' : '已使用' }}
                  </AbBadge>
                  <text v-if="topic.used_at" class="topic-item__time">{{ topic.used_at.slice(0, 10) }}</text>
                </view>
              </view>
              <view class="topic-item__actions">
                <AbButton
                  v-if="topic.status === 'used'"
                  type="ghost"
                  size="sm"
                  @click="resetTopic(topic)"
                >
                  重置
                </AbButton>
                <AbButton type="danger" size="sm" @click="deleteTopic(topic)">
                  删除
                </AbButton>
              </view>
            </view>
          </view>
        </view>
      </view>
    </view>

    <!-- Bottom actions -->
    <view class="project-detail__bottom">
      <template v-if="isNew">
        <AbButton v-if="currentStep > 0" type="ghost" size="lg" @click="prevStep" style="flex: 1;">
          上一步
        </AbButton>
        <AbButton
          v-if="currentStep < maxStep"
          type="primary"
          size="lg"
          :disabled="!canNext"
          :loading="saving"
          @click="nextStep"
          style="flex: 2;"
        >
          下一步
        </AbButton>
        <AbButton
          v-else
          type="primary"
          size="lg"
          :disabled="referenceUploading"
          :loading="saving"
          @click="onSave"
          style="flex: 2;"
        >
          保存
        </AbButton>
      </template>

      <template v-else>
        <AbButton
          type="primary"
          size="lg"
          block
          :disabled="referenceUploading"
          :loading="saving"
          @click="onSave"
        >
          保存修改
        </AbButton>
        <view class="project-detail__bottom-actions">
          <AbButton
            type="ghost"
            size="sm"
            @click="onArchive"
          >
            归档账号
          </AbButton>
          <AbButton
            v-if="projectArchived"
            type="ghost"
            size="sm"
            @click="onRestore"
          >
            恢复账号
          </AbButton>
          <AbButton
            type="danger"
            size="sm"
            @click="onDelete"
          >
            删除账号
          </AbButton>
        </view>
      </template>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { onLoad } from '@dcloudio/uni-app'
import type {
  Project,
  ProjectStats,
  CreateProjectRequest,
  ResourceEntry,
  TopicPool,
  Template,
  ReferenceImageSelection,
  ImageCapabilityOption,
  EcommerceProjectDefaults,
  PlatformConfig,
} from '@/types'
import { projectsApi } from '@/api/projects'
import { resourcesApi } from '@/api/resources'
import { templatesApi } from '@/api/templates'
import { topicPoolApi } from '@/api/topic-pool'
import { imageCapabilitiesApi } from '@/api/image-capabilities'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSelect from '@/components/common/AbSelect.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import ImageCapabilitySelector from '@/components/business/ImageCapabilitySelector.vue'
import ImageAspectRatioField from '@/components/business/ImageAspectRatioField.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'
import TagInput from '@/components/business/TagInput.vue'

const platformOptions = [
  { value: 'seednote', label: '种草笔记', description: '社交种草，图文笔记' },
  { value: 'article', label: '公众号', description: '长图文深度文章' },
  { value: 'moments', label: '朋友圈', description: '私域内容，生活化表达' },
  { value: 'ecommerce', label: '电商出图', description: '商品主图/详情/封面' },
]

const steps = ['选择平台', '基础信息', '发布配置', '高级设置']

const isNew = ref(true)
const editId = ref('')
const pageLoading = ref(false)
const saving = ref(false)
const fetchingProfile = ref(false)
const analyzingStyle = ref(false)
const avatarUploading = ref(false)
const referenceUploading = ref(false)
const authorAvatarUploading = ref(false)
const projectArchived = ref(false)
const currentStep = ref(0)
const stats = ref<ProjectStats | null>(null)
const returnToUrl = ref('')

const form = reactive({
  platform: '',
  name: '',
  profile_url: '',
  avatar_url: '',
  keywords: '',
  instructions: '',
  visual_style: '',
  // Article persona — orthogonal to byline (project memory invariant).
  writer_key: '',
  byline: '',
  writing_voice: '',
  persona_avatar: '',
  template_id: '',
  theme: '',
  layout: '',
  reference_image: null as ReferenceImageSelection | null,
  image_ratio: 'auto',
  image_capability_key: '',
  enable_publishing: false,
  wechat_app_id: '',
  wechat_secret: '',
})

const referencePreviewUrl = ref('')
const imageCapabilities = ref<ImageCapabilityOption[]>([])
const imageCapabilitiesLoading = ref(false)
const defaultImageCapability = ref('')
const existingEcommerceDefaults = ref<EcommerceProjectDefaults>({})
const channelConfigs = ref<PlatformConfig[]>([])

const keywordList = ref<string[]>([])
const themes = ref<ResourceEntry[]>([])
const layouts = ref<ResourceEntry[]>([])
const resourcesLoading = ref(false)
const platformTemplates = ref<Template[]>([])
const topics = ref<TopicPool[]>([])
const topicStatus = ref<'unused' | 'used' | ''>('unused')
const topicsLoading = ref(false)
const topicSaving = ref(false)
const newTopics = ref('')

const errors = reactive<Record<string, string>>({})

const sections = reactive({
  basic: true,
  publishing: false,
  advanced: false,
  stats: false,
  topics: false,
})

const isSeednote = computed(() => form.platform === 'seednote')
const isArticle = computed(() => form.platform === 'article')
const isEcommerce = computed(() => form.platform === 'ecommerce')
const selectedCapability = computed(() => imageCapabilities.value.find((item) => item.key === form.image_capability_key))
const imageCapabilityUnavailable = computed(() =>
  isEcommerce.value && !!form.image_capability_key && (
    !selectedCapability.value
    || selectedCapability.value.enabled !== true
    || selectedCapability.value.price_available !== true
  ),
)
const selectedChannelConfig = computed(() => channelConfigs.value.find((item) => item.id === form.platform))

const themeOptions = computed(() => resourceOptions(themes.value))
const layoutOptions = computed(() => resourceOptions(layouts.value))
const topicStatusOptions = [
  { value: 'unused', label: '未使用' },
  { value: 'used', label: '已使用' },
  { value: '', label: '全部' },
]

const selectedTemplateName = computed(() => {
  const t = platformTemplates.value.find((x) => x.id === form.template_id)
  return t?.name || ''
})

const maxStep = computed(() => {
  // Skip step 3 (publishing) for seednote
  return isSeednote.value ? 2 : 3
})

const canNext = computed(() => {
  if (currentStep.value === 0) return !!form.platform
  return true
})

const stylePlaceholder = computed(() => {
  if (isSeednote.value) {
    return '描述图片视觉风格，如：手绘感，暖色调，小清新，治愈系水彩插画风格'
  }
  if (isEcommerce.value) {
    return '描述品牌视觉风格基线，如：高端极简白底、国潮暖橙插画、电商爆款高饱和促销感'
  }
  if (isArticle.value) {
    return '描述文章封面与配图的视觉风格，如：温暖自然的生活摄影、柔光大地色系'
  }
  return '描述你想要的视觉风格'
})

const styleHint = computed(() => {
  if (isSeednote.value) return '将用于封面和内容图的风格提示'
  if (isEcommerce.value) return '品牌视觉维度——作为电商素材跨图一致的视觉基线'
  if (isArticle.value) return '图片视觉维度——仅决定封面与配图的视觉，与写作风格、排版样式相互独立'
  return ''
})

watch(keywordList, (val) => {
  form.keywords = val.join(',')
})

watch(() => form.platform, (val) => {
  if (val === 'ecommerce' && !form.image_capability_key) form.image_capability_key = defaultImageCapability.value
  if (isNew.value) form.image_ratio = selectedChannelConfig.value?.default_image_ratio || 'auto'
  void loadResources(val)
  void loadPlatformTemplates(val)
})

watch(topicStatus, () => {
  if (!isNew.value && editId.value) {
    void loadTopics()
  }
})

function resourceOptions(items: ResourceEntry[]) {
  return [
    { value: '', label: '不设置' },
    ...items.map((item) => ({
      value: item.name,
      label: item.display_name || item.english_name || item.name,
    })),
  ]
}

async function loadResources(platform?: string) {
  if (!platform) {
    themes.value = []
    layouts.value = []
    return
  }
  resourcesLoading.value = true
  try {
    const [themeRes, layoutRes] = await Promise.all([
      resourcesApi.list('themes', platform).catch(() => ({ items: [] as ResourceEntry[] })),
      resourcesApi.list('layouts', platform).catch(() => ({ items: [] as ResourceEntry[] })),
    ])
    themes.value = themeRes.items || []
    layouts.value = layoutRes.items || []
  } catch (err) {
    console.error('Failed to load resources:', err)
  } finally {
    resourcesLoading.value = false
  }
}

async function loadPlatformTemplates(platform?: string) {
  if (!platform) {
    platformTemplates.value = []
    return
  }
  try {
    const res = await templatesApi.list({ type: platform, limit: 50 })
    platformTemplates.value = res.items || []
  } catch {
    platformTemplates.value = []
  }
}

async function loadProject(id: string) {
  pageLoading.value = true
  try {
    const detail = await projectsApi.get(id)
    const ch = detail.project || (detail as any)
    form.platform = ch.platform || ''
    form.name = ch.name || ''
    form.profile_url = ch.profile_url || ''
    form.avatar_url = ch.avatar_url || ''
    form.keywords = ch.keywords || ''
    form.instructions = ch.instructions || ch.positioning || ''
    form.visual_style = ch.visual_style || ''
    form.writer_key = ch.writer_key || ''
    form.byline = ch.byline || ''
    form.writing_voice = ch.writing_voice || ''
    form.persona_avatar = ch.persona_avatar || ''
    form.template_id = ch.template_id || ''
    form.theme = ch.theme || ''
    form.layout = ch.layout || ''
    form.reference_image = ch.reference_image?.asset_id
      ? { asset_id: ch.reference_image.asset_id }
      : null
    referencePreviewUrl.value = ch.reference_image?.download_url || ''
    form.image_ratio = ch.image_ratio || selectedChannelConfig.value?.default_image_ratio || 'auto'
    form.image_capability_key = ch.ecommerce_defaults?.image_capability_key || defaultImageCapability.value
    existingEcommerceDefaults.value = ch.ecommerce_defaults || {}
    form.enable_publishing = ch.config?.enable_publishing || false
    form.wechat_app_id = ch.config?.wechat_app_id || ''
    form.wechat_secret = ch.config?.wechat_secret || ''
    projectArchived.value = ch.status === 'archived'

    if (ch.keywords) {
      keywordList.value = ch.keywords.split(',').filter(Boolean)
    }
    stats.value = detail.stats || ch.stats || null
    await loadResources(form.platform)
    await loadPlatformTemplates(form.platform)

    // Imported-model compat: if a template_id is set, surface its persona as
    // the default values for any empty project fields (matches studio). The
    // backend clears template_id on next save (project owns its own persona).
    if (form.template_id) {
      try {
        const tpl = await templatesApi.get(form.template_id)
        if (!form.visual_style && tpl.style_prompt) form.visual_style = tpl.style_prompt
        if (!form.byline && tpl.author_name) form.byline = tpl.author_name
        if (!form.writing_voice && tpl.writing_voice) {
          form.writing_voice = tpl.writing_voice
        }
        if (!form.persona_avatar && tpl.persona_avatar) {
          form.persona_avatar = tpl.persona_avatar
        }
        if (!form.theme && tpl.theme) form.theme = tpl.theme
      } catch {
        /* template deleted — leave fields empty */
      }
    }

    await loadTopics()
  } catch (err) {
    console.error('Failed to load project:', err)
    uni.showToast({ title: '加载失败', icon: 'none' })
  } finally {
    pageLoading.value = false
  }
}

async function loadTopics() {
  if (!editId.value) return
  topicsLoading.value = true
  try {
    const res = await topicPoolApi.list(editId.value, {
      status: topicStatus.value || undefined,
      limit: 50,
    })
    topics.value = res.items || []
  } catch (err) {
    console.error('Failed to load topic pool:', err)
    uni.showToast({ title: '加载选题池失败', icon: 'none' })
  } finally {
    topicsLoading.value = false
  }
}

async function addTopics() {
  const entries = newTopics.value
    .split('\n')
    .map((topic) => topic.trim())
    .filter(Boolean)
  if (entries.length === 0 || topicSaving.value || !editId.value) return
  topicSaving.value = true
  try {
    await topicPoolApi.create(editId.value, { topics: entries })
    newTopics.value = ''
    uni.showToast({ title: '已添加选题', icon: 'success' })
    await loadTopics()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '添加失败', icon: 'none' })
  } finally {
    topicSaving.value = false
  }
}

async function resetTopic(topic: TopicPool) {
  if (!editId.value) return
  try {
    await topicPoolApi.reset(editId.value, topic.id)
    uni.showToast({ title: '已重置', icon: 'success' })
    await loadTopics()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '重置失败', icon: 'none' })
  }
}

function deleteTopic(topic: TopicPool) {
  if (!editId.value) return
  uni.showModal({
    title: '删除选题',
    content: `确定删除「${topic.topic}」吗？`,
    confirmColor: '#DC2626',
    success: async (res) => {
      if (!res.confirm) return
      try {
        await topicPoolApi.delete(editId.value, topic.id)
        uni.showToast({ title: '已删除', icon: 'success' })
        await loadTopics()
      } catch (err: any) {
        uni.showToast({ title: err?.message || '删除失败', icon: 'none' })
      }
    },
  })
}

async function onFetchProfile() {
  if (!form.profile_url) {
    uni.showToast({ title: '请输入账号链接', icon: 'none' })
    return
  }
  fetchingProfile.value = true
  try {
    const profile = await projectsApi.fetchProfile(
      form.platform,
      form.profile_url,
      form.wechat_app_id || undefined,
      form.wechat_secret || undefined,
    )
    if (profile.name) form.name = profile.name
    if (profile.avatar_url) form.avatar_url = profile.avatar_url
    if (profile.positioning) form.instructions = profile.positioning
    if (profile.keywords) {
      const kw = typeof profile.keywords === 'string'
        ? profile.keywords.split(',').filter(Boolean)
        : profile.keywords
      keywordList.value = kw
    }
    if (profile.visual_style) form.visual_style = profile.visual_style
    uni.showToast({ title: '已自动识别账号信息', icon: 'success' })
  } catch (err: any) {
    const msg = err?.message || '拉取失败，请手动填写'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    fetchingProfile.value = false
  }
}

// Avatar / reference / author-avatar uploads use OSS direct upload.
function chooseAndUpload(
  target: 'avatar' | 'reference' | 'author_avatar',
  purpose: 'project_reference',
) {
  uni.chooseImage({
    count: 1,
    success: async (chosen) => {
      const filePath = chosen.tempFilePaths?.[0]
      if (!filePath) return
      const flag =
        target === 'avatar' ? avatarUploading
          : target === 'reference' ? referenceUploading
            : authorAvatarUploading
      flag.value = true
      try {
        const result = await projectsApi.uploadImage(filePath, purpose)
        if (target === 'avatar') form.avatar_url = result.url
        else if (target === 'reference') {
          form.reference_image = { upload_session_id: result.upload_session_id }
          referencePreviewUrl.value = result.preview_url
        }
        else form.persona_avatar = result.url
        uni.showToast({ title: '上传成功', icon: 'success' })
      } catch (err: any) {
        uni.showToast({ title: err?.message || '上传失败', icon: 'none' })
      } finally {
        flag.value = false
      }
    },
  })
}

function onChooseAvatar() {
  if (avatarUploading.value) return
  chooseAndUpload('avatar', 'project_reference')
}

function onChooseReference() {
  if (referenceUploading.value) return
  chooseAndUpload('reference', 'project_reference')
}

function onChooseAuthorAvatar() {
  if (authorAvatarUploading.value) return
  chooseAndUpload('author_avatar', 'project_reference')
}

async function onAnalyzeReference() {
  if (!referencePreviewUrl.value) {
    uni.showToast({ title: '请先上传参考图', icon: 'none' })
    return
  }
  if (analyzingStyle.value) return
  analyzingStyle.value = true
  try {
    const result = await projectsApi.analyzeImage(referencePreviewUrl.value)
    if (result.visual_style) {
      form.visual_style = result.visual_style
      uni.showToast({ title: '已识别视觉风格', icon: 'success' })
    } else {
      uni.showToast({ title: '未能识别风格', icon: 'none' })
    }
  } catch (err: any) {
    uni.showToast({ title: err?.message || '识别失败', icon: 'none' })
  } finally {
    analyzingStyle.value = false
  }
}

function clearReferenceImage() {
  form.reference_image = null
  referencePreviewUrl.value = ''
}

function pickTemplate() {
  if (platformTemplates.value.length === 0) {
    uni.showToast({ title: '该平台暂无可用模板', icon: 'none' })
    return
  }
  const labels = platformTemplates.value.map((t) => t.name)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const tpl = platformTemplates.value[res.tapIndex]
      if (tpl) importTemplate(tpl)
    },
  })
}

// One-shot import of persona/style from a template (mirrors studio).
// Per project memory: do NOT auto-fill byline from a writer-persona source
// unless the template explicitly carries author_name.
function importTemplate(tpl: Template) {
  form.template_id = tpl.id
  if (tpl.style_prompt) form.visual_style = tpl.style_prompt
  if (tpl.author_name) form.byline = tpl.author_name
  if (tpl.writing_voice) form.writing_voice = tpl.writing_voice
  if (tpl.persona_avatar) form.persona_avatar = tpl.persona_avatar
  if (tpl.theme) form.theme = tpl.theme
  uni.showToast({ title: `已导入「${tpl.name}」`, icon: 'success' })
}

function clearTemplateImport() {
  form.template_id = ''
}

function validate(): boolean {
  Object.keys(errors).forEach((k) => delete errors[k])

  if (!form.name?.trim()) {
    errors.name = '请输入账号名称'
    return false
  }
  if (imageCapabilityUnavailable.value) {
    uni.showToast({ title: '该图像能力已停用，请重新选择', icon: 'none' })
    return false
  }
  if (form.enable_publishing && isArticle.value && !form.wechat_app_id?.trim()) {
    errors.wechat_app_id = '启用自动发布时，微信 AppID 为必填项'
    return false
  }
  return true
}

function buildPayload(): CreateProjectRequest {
  return {
    platform: form.platform,
    name: form.name.trim() || undefined,
    profile_url: form.profile_url || undefined,
    avatar_url: form.avatar_url || undefined,
    keywords: form.keywords || undefined,
    instructions: form.instructions || undefined,
    visual_style: form.visual_style || undefined,
    writer_key: isArticle.value && form.writer_key ? form.writer_key : undefined,
    writing_voice: (isArticle.value || isSeednote.value) && form.writing_voice
      ? form.writing_voice
      : undefined,
    persona_avatar: (isArticle.value || isSeednote.value) && form.persona_avatar
      ? form.persona_avatar
      : undefined,
    template_id: form.template_id || undefined,
    theme: form.theme || undefined,
    layout: form.layout || undefined,
    byline: form.byline || undefined,
    reference_image: form.reference_image,
    image_ratio: form.image_ratio,
    ecommerce_defaults: isEcommerce.value ? {
      ...existingEcommerceDefaults.value,
      image_capability_key: form.image_capability_key || undefined,
    } : undefined,
    enable_publishing: form.enable_publishing || undefined,
    wechat_app_id: form.wechat_app_id || undefined,
    wechat_secret: form.wechat_secret || undefined,
  }
}

async function onSave() {
  if (saving.value || referenceUploading.value) return
  if (!validate()) return

  saving.value = true
  try {
    const payload = buildPayload()
    let createdProjectId = ''
    if (isNew.value) {
      const created = await projectsApi.create(payload)
      createdProjectId = created.project?.id || ''
      uni.showToast({ title: '创建成功', icon: 'success' })
    } else {
      await projectsApi.update(editId.value, payload)
      uni.showToast({ title: '保存成功', icon: 'success' })
    }
    setTimeout(() => {
      if (isNew.value && returnToUrl.value && createdProjectId) {
        uni.redirectTo({ url: appendReturnProject(returnToUrl.value, createdProjectId) })
        return
      }
      uni.navigateBack()
    }, 500)
  } catch (err: any) {
    const msg = err?.message || '保存失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    saving.value = false
  }
}

function appendReturnProject(url: string, projectId: string): string {
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}project_id=${encodeURIComponent(projectId)}`
}

function safeDecodeQuery(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function nextStep() {
  if (currentStep.value === 0 && !form.platform) {
    uni.showToast({ title: '请选择平台', icon: 'none' })
    return
  }
  // Skip step 2 (publishing) for seednote
  if (currentStep.value === 1 && isSeednote.value) {
    currentStep.value = 3
    return
  }
  if (currentStep.value < 3) {
    currentStep.value++
  }
}

function prevStep() {
  // Skip step 2 (publishing) for seednote going back
  if (currentStep.value === 3 && isSeednote.value) {
    currentStep.value = 1
    return
  }
  if (currentStep.value > 0) {
    currentStep.value--
  }
}

function onArchive() {
  uni.showModal({
    title: '归档账号',
    content: '确定归档该账号吗？归档后可随时恢复。',
    success: async (res) => {
      if (res.confirm) {
        try {
          await projectsApi.archive(editId.value)
          projectArchived.value = true
          uni.showToast({ title: '已归档', icon: 'success' })
        } catch {
          uni.showToast({ title: '操作失败', icon: 'none' })
        }
      }
    },
  })
}

function onRestore() {
  uni.showModal({
    title: '恢复账号',
    content: '确定恢复该账号吗？',
    success: async (res) => {
      if (res.confirm) {
        try {
          await projectsApi.restore(editId.value)
          projectArchived.value = false
          uni.showToast({ title: '已恢复', icon: 'success' })
        } catch {
          uni.showToast({ title: '操作失败', icon: 'none' })
        }
      }
    },
  })
}

function onDelete() {
  uni.showModal({
    title: '删除账号',
    content: '确定删除该账号吗？此操作不可恢复。',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (res.confirm) {
        try {
          await projectsApi.delete(editId.value)
          uni.showToast({ title: '已删除', icon: 'success' })
          setTimeout(() => uni.navigateBack(), 500)
        } catch {
          uni.showToast({ title: '删除失败', icon: 'none' })
        }
      }
    },
  })
}

onLoad((query) => {
  void loadImageCapabilities()
  void loadPlatformConfigs()
  if (query?.return_to) {
    returnToUrl.value = safeDecodeQuery(String(query.return_to))
  }
  if (query?.platform && !query.id) {
    form.platform = String(query.platform)
  }
  if (query?.id) {
    isNew.value = false
    editId.value = query.id
    loadProject(query.id)
  }
})

async function loadPlatformConfigs() {
  try {
    channelConfigs.value = await projectsApi.platformConfigs()
    if (isNew.value && form.platform) {
      form.image_ratio = selectedChannelConfig.value?.default_image_ratio || 'auto'
    }
  } catch {
    channelConfigs.value = []
  }
}

async function loadImageCapabilities() {
  imageCapabilitiesLoading.value = true
  try {
    const response = await imageCapabilitiesApi.list()
    imageCapabilities.value = response.items || []
    defaultImageCapability.value = response.default_capability || response.items?.[0]?.key || ''
    if (isEcommerce.value && !form.image_capability_key) form.image_capability_key = defaultImageCapability.value
  } catch {
    imageCapabilities.value = []
  } finally {
    imageCapabilitiesLoading.value = false
  }
}

</script>

<style lang="scss" scoped>
.project-detail {
  min-height: 100vh;
  background-color: $ab-background;
  padding-bottom: 240rpx;

  &__steps {
    display: flex;
    justify-content: center;
    gap: $ab-space-lg;
    padding: $ab-space-lg $ab-space-md;
    background-color: $ab-surface;
    border-bottom: 2rpx solid $ab-border;
  }

  &__step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: $ab-space-xs;

    &--done &__step-dot {
      background-color: $ab-success;
      border-color: $ab-success;
      color: #FFFFFF;
    }

    &--active &__step-dot {
      border-color: $ab-primary;
      background-color: $ab-primary;
      color: #FFFFFF;
    }
  }

  &__step-dot {
    width: 52rpx;
    height: 52rpx;
    border-radius: 50%;
    border: 3rpx solid $ab-border;
    background-color: $ab-surface;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    font-weight: $ab-font-medium;
  }

  &__step-check {
    color: #FFFFFF;
  }

  &__step-label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__step--active &__step-label {
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }

  &__content {
    padding: $ab-space-md;
  }

  &__section-title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    margin-bottom: $ab-space-md;
  }

  &__collapsible-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: $ab-space-md;
    background-color: $ab-divider;
    cursor: pointer;
  }

  &__collapsible-title {
    font-size: $ab-text-md;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__collapsible-arrow {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__fields {
    padding: 0 $ab-space-md $ab-space-md;
  }

  &__bottom {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background-color: $ab-surface;
    padding: $ab-space-md $ab-space-lg;
    padding-bottom: calc(#{$ab-space-md} + env(safe-area-inset-bottom));
    box-shadow: 0 -4rpx 12rpx rgba(0, 0, 0, 0.06);
    z-index: 20;

    &-actions {
      display: flex;
      gap: $ab-space-md;
      margin-top: $ab-space-sm;
    }
  }
}

// Platform card
.platform-card {
  display: flex;
  align-items: center;
  gap: $ab-space-md;
  padding: $ab-space-md;
  background-color: $ab-surface;
  border: 3rpx solid $ab-border;
  border-radius: $ab-radius-md;
  margin-bottom: $ab-space-sm;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;

  &--selected {
    border-color: $ab-primary;
    box-shadow: 0 0 0 3rpx rgba($ab-primary, 0.1);
  }

  &__indicator {
    position: relative;
  }

  &__check {
    position: absolute;
    top: -8rpx;
    right: -8rpx;
    width: 32rpx;
    height: 32rpx;
    background-color: $ab-primary;
    color: #FFFFFF;
    border-radius: 50%;
    font-size: 20rpx;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  &__info {
    flex: 1;
  }

  &__name {
    font-size: $ab-text-md;
    font-weight: $ab-font-medium;
    color: $ab-text;
    display: block;
  }

  &__desc {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    display: block;
    margin-top: 4rpx;
  }
}

// Form fields
.field-group {
  margin-bottom: $ab-space-md;
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

.field-hint {
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  display: block;
  margin-top: $ab-space-xs;
  line-height: 1.4;
}

.field-error {
  display: block;
  margin-top: $ab-space-xs;
  color: $ab-danger;
  font-size: $ab-text-xs;
  line-height: 1.4;
}

.field-row {
  display: flex;
  align-items: center;
}

.field-spacer {
  margin-top: $ab-space-md;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

// Avatar uploader
.avatar-uploader {
  position: relative;
  width: 160rpx;
  height: 160rpx;
  border-radius: 50%;
  overflow: hidden;
  background-color: $ab-divider;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: $ab-space-xs;

  &__img {
    width: 100%;
    height: 100%;
  }

  &__placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__mask {
    position: absolute;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.5);
    color: #FFFFFF;
    font-size: $ab-text-xs;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  &__actions {
    display: flex;
    gap: $ab-space-md;
    margin-top: $ab-space-xs;
  }

  &__action {
    font-size: $ab-text-sm;
    color: $ab-primary;
    padding: 4rpx $ab-space-sm;

    &--danger {
      color: $ab-danger;
    }
  }
}

// Reference image uploader (also used for author avatar, --sm variant)
.ref-uploader {
  position: relative;
  width: 240rpx;
  height: 180rpx;
  border: 2rpx dashed $ab-border;
  border-radius: $ab-radius-sm;
  overflow: hidden;
  background-color: $ab-surface;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: $ab-space-xs;

  &--sm {
    width: 160rpx;
    height: 160rpx;
    border-radius: 50%;
  }

  &__img {
    width: 100%;
    height: 100%;
  }

  &__placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__mask {
    position: absolute;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.5);
    color: #FFFFFF;
    font-size: $ab-text-xs;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  &__actions {
    display: flex;
    gap: $ab-space-md;
    margin-top: $ab-space-xs;
  }

  &__action {
    font-size: $ab-text-sm;
    color: $ab-primary;
    padding: 4rpx $ab-space-sm;

    &--danger {
      color: $ab-danger;
    }
  }
}

// Persona block
.persona-block {
  padding: $ab-space-md;
  background-color: $ab-primary-bg;
  border-radius: $ab-radius-sm;
  margin-bottom: $ab-space-md;

  &__title {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-primary;
  }
}

// Style block (with analyzing indicator)
.style-block {
  position: relative;

  &__analyzing {
    margin-top: $ab-space-xs;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Picker row (template)
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

// Stats grid
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: $ab-space-sm;
  padding: $ab-space-md;
}

.stats-cell {
  background-color: $ab-surface;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-xs;
  text-align: center;

  &__num {
    display: block;
    font-size: $ab-text-lg;
    font-weight: $ab-font-medium;
    color: $ab-text;

    &--success { color: $ab-success; }
    &--danger { color: $ab-danger; }
    &--info { color: $ab-info; }
  }

  &__label {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 4rpx;
  }
}

.topic-empty {
  padding: $ab-space-lg 0;
  text-align: center;
  font-size: $ab-text-sm;
  color: $ab-text-tertiary;
}

.topic-list {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
}

.topic-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-sm;
  padding: $ab-space-sm;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-surface;

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__title {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text;
    line-height: 1.5;
  }

  &__meta {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    margin-top: $ab-space-xs;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__actions {
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
    flex-shrink: 0;
  }
}

// Ratio button group
.ratio-group {
  display: flex;
  gap: $ab-space-sm;
  flex-wrap: wrap;
}

.ratio-btn {
  padding: 12rpx $ab-space-lg;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  font-size: $ab-text-sm;
  color: $ab-text-secondary;
  background-color: $ab-surface;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    color: $ab-primary;
    background-color: $ab-primary-bg;
  }
}
</style>
