<template>
  <view class="page channel-detail">
    <AbLoading v-if="pageLoading && !isNew" text="加载中" />

    <view v-else>
      <!-- Step indicator (new mode only) -->
      <view v-if="isNew" class="channel-detail__steps">
        <view
          v-for="(s, i) in steps"
          :key="i"
          class="channel-detail__step"
          :class="{
            'channel-detail__step--active': currentStep === i,
            'channel-detail__step--done': currentStep > i,
          }"
        >
          <view class="channel-detail__step-dot">
            <text v-if="currentStep > i" class="channel-detail__step-check">✓</text>
            <text v-else>{{ i + 1 }}</text>
          </view>
          <text class="channel-detail__step-label">{{ s }}</text>
        </view>
      </view>

      <!-- Step 1: Platform selection -->
      <view v-if="isNew && currentStep === 0" class="channel-detail__content">
        <view class="channel-detail__section-title">选择平台</view>

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
      <view v-if="isNew ? currentStep === 1 : true" class="channel-detail__content">
        <view v-if="isNew" class="channel-detail__section-title">基础信息</view>
        <view v-else class="channel-detail__collapsible-header" @tap="sections.basic = !sections.basic">
          <text class="channel-detail__collapsible-title">基础信息</text>
          <text class="channel-detail__collapsible-arrow">{{ sections.basic ? '收起' : '展开' }}</text>
        </view>

        <view v-if="isNew || sections.basic" class="channel-detail__fields">
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
              v-model="form.positioning"
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

          <!-- Writing style -->
          <view class="field-group">
            <text class="field-label">写作风格</text>
            <AbTextarea
              v-model="form.style"
              placeholder="描述你想要的写作风格"
              :rows="2"
            />
          </view>

          <!-- Image ratio -->
          <view class="field-group">
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
        </view>
      </view>

      <!-- Step 3: Publishing config (only for article/xls) -->
      <view v-if="isNew ? currentStep === 2 : true">
        <template v-if="!isSeednote">
          <view v-if="isNew" class="channel-detail__section-title">发布配置</view>
          <view v-else class="channel-detail__collapsible-header" @tap="sections.publishing = !sections.publishing">
            <text class="channel-detail__collapsible-title">发布配置</text>
            <text class="channel-detail__collapsible-arrow">{{ sections.publishing ? '收起' : '展开' }}</text>
          </view>

          <view v-if="isNew || sections.publishing" class="channel-detail__fields">
            <!-- Auto-publish switch -->
            <view class="field-group">
              <view class="switch-row">
                <text class="field-label" style="margin-bottom: 0;">自动发布到微信</text>
                <AbSwitch v-model="form.enable_publishing" />
              </view>
            </view>

            <!-- WeChat credentials (shown when auto-publish is on) -->
            <template v-if="form.enable_publishing">
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
                  placeholder="输入微信公众号 AppSecret"
                />
                <text class="field-hint">安全提示：AppSecret 仅在服务端使用，请勿在公共设备上填写。</text>
              </view>
            </template>
          </view>
        </template>
      </view>

      <!-- Step 4: Advanced settings -->
      <view v-if="isNew ? currentStep === 3 : true">
        <view v-if="isNew" class="channel-detail__section-title">高级设置（可选）</view>
        <view v-else class="channel-detail__collapsible-header" @tap="sections.advanced = !sections.advanced">
          <text class="channel-detail__collapsible-title">高级设置</text>
          <text class="channel-detail__collapsible-arrow">{{ sections.advanced ? '收起' : '展开' }}</text>
        </view>

        <view v-if="isNew || sections.advanced" class="channel-detail__fields">
          <!-- Reference image URL -->
          <view class="field-group">
            <text class="field-label">参考图片</text>
            <AbInput
              v-model="form.reference_image_url"
              placeholder="输入图片URL"
            />
          </view>

          <!-- Theme -->
          <view class="field-group">
            <text class="field-label">主题</text>
            <AbSelect
              v-model="form.theme"
              :options="themeOptions"
              :disabled="resourcesLoading"
              placeholder="选择转换主题"
            />
          </view>

          <view class="field-group">
            <text class="field-label">文章版式</text>
            <AbSelect
              v-model="form.layout"
              :options="layoutOptions"
              :disabled="resourcesLoading"
              placeholder="选择版式布局"
            />
          </view>

          <view class="field-group">
            <text class="field-label">图片预设</text>
            <AbSelect
              v-model="form.image_preset"
              :options="imagePresetOptions"
              :disabled="resourcesLoading"
              placeholder="选择封面/配图预设"
            />
          </view>

          <!-- Author name -->
          <view class="field-group">
            <text class="field-label">作者名</text>
            <AbInput
              v-model="form.author"
              placeholder="署名"
            />
          </view>
        </view>
      </view>

      <!-- Topic pool (edit mode only) -->
      <view v-if="!isNew" class="channel-detail__content">
        <view class="channel-detail__collapsible-header" @tap="sections.topics = !sections.topics">
          <text class="channel-detail__collapsible-title">选题池</text>
          <text class="channel-detail__collapsible-arrow">{{ sections.topics ? '收起' : '展开' }}</text>
        </view>

        <view v-if="sections.topics" class="channel-detail__fields">
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
    <view class="channel-detail__bottom">
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
          :loading="saving"
          @click="onSave"
        >
          保存修改
        </AbButton>
        <view class="channel-detail__bottom-actions">
          <AbButton
            type="ghost"
            size="sm"
            @click="onArchive"
          >
            归档账号
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
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { onLoad } from '@dcloudio/uni-app'
import type { Channel, CreateChannelRequest, ResourceEntry, TopicPool } from '@/types'
import { channelsApi } from '@/api/channels'
import { resourcesApi } from '@/api/resources'
import { topicPoolApi } from '@/api/topic-pool'
import { IMAGE_RATIOS } from '@/utils/constants'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSelect from '@/components/common/AbSelect.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'
import TagInput from '@/components/business/TagInput.vue'

const platformOptions = [
  { value: 'seednote', label: '种草笔记', description: '社交种草，图文笔记' },
  { value: 'article', label: '公众号', description: '长图文深度文章' },
  { value: 'xls', label: '小绿书', description: '图片帖，轻量分享' },
]

const steps = ['选择平台', '基础信息', '发布配置', '高级设置']

const isNew = ref(true)
const editId = ref('')
const pageLoading = ref(false)
const saving = ref(false)
const fetchingProfile = ref(false)
const currentStep = ref(0)

const form = reactive({
  platform: '',
  name: '',
  profile_url: '',
  avatar_url: '',
  positioning: '',
  keywords: '',
  style: '',
  theme: '',
  layout: '',
  image_preset: '',
  author: '',
  reference_image_url: '',
  image_ratio: '3:4',
  enable_publishing: false,
  wechat_app_id: '',
  wechat_secret: '',
})

const keywordList = ref<string[]>([])
const themes = ref<ResourceEntry[]>([])
const layouts = ref<ResourceEntry[]>([])
const imagePresets = ref<ResourceEntry[]>([])
const resourcesLoading = ref(false)
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
  topics: false,
})

const isSeednote = computed(() => form.platform === 'seednote')
const themeOptions = computed(() => resourceOptions(themes.value))
const layoutOptions = computed(() => resourceOptions(layouts.value))
const imagePresetOptions = computed(() => resourceOptions(imagePresets.value))
const topicStatusOptions = [
  { value: 'unused', label: '未使用' },
  { value: 'used', label: '已使用' },
  { value: '', label: '全部' },
]
const maxStep = computed(() => {
  // Skip step 3 (publishing) for seednote
  return isSeednote.value ? 2 : 3
})

const canNext = computed(() => {
  if (currentStep.value === 0) return !!form.platform
  return true
})

watch(keywordList, (val) => {
  form.keywords = val.join(',')
})

watch(() => form.platform, (val) => {
  // Reset image ratio to platform default when platform changes
  const defaults: Record<string, string> = {
    article: '16:9',
    seednote: '3:4',
    xls: '3:4',
  }
  if (val && defaults[val]) {
    form.image_ratio = defaults[val]
  }
  void loadResources(val)
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
    imagePresets.value = []
    return
  }
  resourcesLoading.value = true
  try {
    const [themeRes, layoutRes, presetRes] = await Promise.all([
      resourcesApi.list('themes', platform),
      resourcesApi.list('layouts', platform),
      resourcesApi.list('image_presets', platform),
    ])
    themes.value = themeRes.items || []
    layouts.value = layoutRes.items || []
    imagePresets.value = presetRes.items || []
  } catch (err) {
    console.error('Failed to load resources:', err)
    uni.showToast({ title: '加载资源配置失败', icon: 'none' })
  } finally {
    resourcesLoading.value = false
  }
}

async function loadChannel(id: string) {
  pageLoading.value = true
  try {
    const detail = await channelsApi.get(id)
    const ch = detail.channel || detail as any
    form.platform = ch.platform || ''
    form.name = ch.name || ''
    form.profile_url = ch.profile_url || ''
    form.avatar_url = ch.avatar_url || ''
    form.positioning = ch.positioning || ''
    form.keywords = ch.keywords || ''
    form.style = ch.style || ''
    form.theme = ch.theme || ''
    form.layout = ch.layout || ''
    form.image_preset = ch.image_preset || ''
    form.author = ch.author || ''
    form.reference_image_url = ch.reference_image_url || ''
    form.image_ratio = ch.image_ratio || '3:4'
    form.enable_publishing = ch.config?.enable_publishing || false
    form.wechat_app_id = ch.config?.wechat_app_id || ''
    form.wechat_secret = ch.config?.wechat_secret || ''

    if (ch.keywords) {
      keywordList.value = ch.keywords.split(',').filter(Boolean)
    }
    await loadResources(form.platform)
    await loadTopics()
  } catch (err) {
    console.error('Failed to load channel:', err)
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
    const profile = await channelsApi.fetchProfile(form.platform, form.profile_url)
    if (profile.name) form.name = profile.name
    if (profile.avatar_url) form.avatar_url = profile.avatar_url
    if (profile.positioning) form.positioning = profile.positioning
    if (profile.keywords) {
      const kw = typeof profile.keywords === 'string'
        ? profile.keywords.split(',').filter(Boolean)
        : profile.keywords
      keywordList.value = kw
    }
    if (profile.style) form.style = profile.style
    uni.showToast({ title: '已自动识别账号信息', icon: 'success' })
  } catch (err: any) {
    const msg = err?.message || '拉取失败，请手动填写'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    fetchingProfile.value = false
  }
}

function validate(): boolean {
  Object.keys(errors).forEach((k) => delete errors[k])

  if (!form.name?.trim()) {
    errors.name = '请输入账号名称'
    return false
  }
  return true
}

function buildPayload(): CreateChannelRequest {
  return {
    platform: form.platform,
    name: form.name.trim(),
    profile_url: form.profile_url || undefined,
    avatar_url: form.avatar_url || undefined,
    positioning: form.positioning || undefined,
    keywords: form.keywords || undefined,
    style: form.style || undefined,
    theme: form.theme || undefined,
    layout: form.layout || undefined,
    image_preset: form.image_preset || undefined,
    author: form.author || undefined,
    reference_image_url: form.reference_image_url || undefined,
    image_ratio: form.image_ratio || undefined,
    enable_publishing: form.enable_publishing || undefined,
    wechat_app_id: form.wechat_app_id || undefined,
    wechat_secret: form.wechat_secret || undefined,
  }
}

async function onSave() {
  if (!validate()) return

  saving.value = true
  try {
    const payload = buildPayload()
    if (isNew.value) {
      await channelsApi.create(payload)
      uni.showToast({ title: '创建成功', icon: 'success' })
    } else {
      await channelsApi.update(editId.value, payload)
      uni.showToast({ title: '保存成功', icon: 'success' })
    }
    setTimeout(() => {
      uni.navigateBack()
    }, 500)
  } catch (err: any) {
    const msg = err?.message || '保存失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    saving.value = false
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
          await channelsApi.archive(editId.value)
          uni.showToast({ title: '已归档', icon: 'success' })
          setTimeout(() => uni.navigateBack(), 500)
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
          await channelsApi.delete(editId.value)
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
  if (query?.id) {
    isNew.value = false
    editId.value = query.id
    loadChannel(query.id)
  }
})
</script>

<style lang="scss" scoped>
.channel-detail {
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

.field-row {
  display: flex;
  align-items: center;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
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
