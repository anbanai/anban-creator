<template>
  <view class="page">
    <!-- Scope Tabs (全部 / 公共 / 我的) -->
    <view class="scope-bar">
      <view
        v-for="s in scopeTabs"
        :key="s.value"
        class="scope-bar__item"
        :class="{ 'scope-bar__item--active': scope === s.value }"
        @tap="onScopeChange(s.value)"
      >
        <text class="scope-bar__label">{{ s.label }}</text>
      </view>
      <view class="scope-bar__spacer" />
      <view class="scope-bar__create" @tap="openCreate">
        <text class="scope-bar__create-icon">+</text>
        <text class="scope-bar__create-text">新建</text>
      </view>
    </view>

    <!-- Type Filter Tabs -->
    <AbTabs :tabs="typeTabs" :model-value="activeType" @update:model-value="onTypeChange" />

    <!-- Search Bar -->
    <view class="search-bar">
      <AbInput
        v-model="searchQuery"
        placeholder="搜索模板标签..."
      />
      <text v-if="list.total.value > 0" class="search-bar__count">共 {{ list.total.value }} 个</text>
    </view>

    <!-- Content -->
    <view v-if="list.refreshing.value && !list.items.value.length" class="templates-loading">
      <AbLoading text="加载中..." />
    </view>

    <AbEmpty
      v-else-if="!list.items.value.length && !list.loading.value"
      :title="scope === 'mine' ? '你还没有创建过模板' : '暂无模板'"
      :description="scope === 'mine' ? '点击右上角「新建」开始创建' : '当前筛选条件下没有找到模板'"
    />

    <scroll-view
      v-else
      class="templates-scroll"
      scroll-y
      :refresher-enabled="true"
      :refresher-triggered="list.refreshing.value"
      @refresherrefresh="onPullRefresh"
      @scrolltolower="list.loadMore"
    >
      <view class="templates-grid">
        <view
          v-for="tpl in filteredItems"
          :key="tpl.id"
          class="template-card"
          @tap="onPreview(tpl)"
        >
          <!-- Thumbnail -->
          <view class="template-card__thumb">
            <image
              v-if="tpl.thumbnail_url"
              class="template-card__image"
              :src="tpl.thumbnail_url"
              mode="aspectFill"
            />
            <view v-else class="template-card__placeholder">
              <text class="template-card__placeholder-text">{{ tpl.name.charAt(0) }}</text>
            </view>
            <!-- Type badge overlay -->
            <view class="template-card__type-badge">
              <AbBadge size="sm" :variant="getTypeBadgeVariant(tpl.type)">
                {{ getTypeLabel(tpl.type) }}
              </AbBadge>
            </view>
            <!-- Visibility badge overlay (private only) -->
            <view v-if="isPrivate(tpl)" class="template-card__vis-badge">
              <text class="template-card__vis-text">私有</text>
            </view>
          </view>

          <!-- Info -->
          <view class="template-card__info">
            <text class="template-card__name">{{ tpl.name }}</text>
            <text v-if="tpl.style_prompt" class="template-card__prompt">{{ tpl.style_prompt }}</text>
            <!-- Article persona preview -->
            <view v-if="tpl.type === 'article' && (tpl.author_name || tpl.author_style_intro)" class="template-card__persona">
              <image
                v-if="tpl.author_avatar_url"
                class="template-card__avatar"
                :src="tpl.author_avatar_url"
                mode="aspectFill"
              />
              <view v-else class="template-card__avatar template-card__avatar--placeholder">
                <text class="template-card__avatar-text">{{ (tpl.author_name || '?').charAt(0) }}</text>
              </view>
              <text class="template-card__author">{{ tpl.author_name || '未命名' }}</text>
            </view>
            <view v-if="tpl.tags && tpl.tags.length" class="template-card__tags">
              <text
                v-for="tag in tpl.tags.slice(0, 3)"
                :key="tag"
                class="template-card__tag"
              >
                {{ tag }}
              </text>
              <text v-if="tpl.tags.length > 3" class="template-card__tag template-card__tag--more">
                +{{ tpl.tags.length - 3 }}
              </text>
            </view>
          </view>
        </view>
      </view>

      <!-- Load More -->
      <view v-if="list.hasMore.value" class="load-more">
        <AbLoading v-if="list.loading.value" size="sm" />
        <text v-else class="load-more__text">上拉加载更多</text>
      </view>
      <text v-else-if="list.items.value.length" class="load-more__end">-- 已经到底了 --</text>
    </scroll-view>

    <!-- Template Preview Popup -->
    <view v-if="previewTemplate || detailLoading" class="preview-mask" @tap="closePreview" />
    <view class="preview-popup" :class="{ 'preview-popup--show': previewTemplate || detailLoading }">
      <view v-if="previewTemplate" class="preview-popup__content">
        <!-- Close button -->
        <view class="preview-popup__close" @tap="closePreview">
          <text class="preview-popup__close-icon">×</text>
        </view>

        <!-- Scrollable body -->
        <scroll-view class="preview-popup__body" scroll-y>
          <!-- Thumbnail -->
          <view class="preview-popup__thumb">
            <image
              v-if="previewTemplate.thumbnail_url"
              class="preview-popup__image"
              :src="previewTemplate.thumbnail_url"
              mode="aspectFit"
            />
            <view v-else class="preview-popup__placeholder">
              <text class="preview-popup__placeholder-text">
                {{ previewTemplate.name.charAt(0) }}
              </text>
            </view>
            <!-- Type + visibility badges -->
            <view class="preview-popup__badges">
              <AbBadge size="sm" :variant="getTypeBadgeVariant(previewTemplate.type)">
                {{ getTypeLabel(previewTemplate.type) }}
              </AbBadge>
              <AbBadge v-if="isPrivate(previewTemplate)" size="sm" variant="neutral">私有</AbBadge>
              <AbBadge v-else size="sm" variant="info">公开</AbBadge>
            </view>
          </view>

          <!-- Info -->
          <view class="preview-popup__info">
            <text class="preview-popup__name">{{ previewTemplate.name }}</text>
            <view class="preview-popup__meta">
              <text v-if="previewTemplate.category" class="preview-popup__category">
                分类：{{ previewTemplate.category }}
              </text>
            </view>

            <!-- Style prompt -->
            <view v-if="previewTemplate.style_prompt" class="preview-popup__block">
              <text class="preview-popup__block-label">视觉风格</text>
              <text class="preview-popup__block-text">{{ previewTemplate.style_prompt }}</text>
            </view>

            <!-- Poster scaffold -->
            <template v-if="previewTemplate.type === 'poster'">
              <view v-if="previewTemplate.writing_style" class="preview-popup__block">
                <text class="preview-popup__block-label">写作风格</text>
                <text class="preview-popup__block-text">{{ previewTemplate.writing_style }}</text>
              </view>
              <view v-if="scaffoldText(previewTemplate.structure)" class="preview-popup__block">
                <text class="preview-popup__block-label">内容结构</text>
                <text class="preview-popup__block-text">{{ scaffoldText(previewTemplate.structure) }}</text>
              </view>
              <view v-if="scaffoldText(previewTemplate.example_content)" class="preview-popup__block">
                <text class="preview-popup__block-label">示例内容</text>
                <text class="preview-popup__block-text">{{ scaffoldText(previewTemplate.example_content) }}</text>
              </view>
            </template>

            <!-- Article persona + theme -->
            <template v-if="previewTemplate.type === 'article'">
              <view
                v-if="previewTemplate.author_name || previewTemplate.author_style_intro || previewTemplate.author_avatar_url"
                class="preview-popup__block"
              >
                <text class="preview-popup__block-label">写作风格</text>
                <view class="preview-popup__persona">
                  <image
                    v-if="previewTemplate.author_avatar_url"
                    class="preview-popup__persona-avatar"
                    :src="previewTemplate.author_avatar_url"
                    mode="aspectFill"
                  />
                  <view v-else class="preview-popup__persona-avatar preview-popup__persona-avatar--placeholder">
                    <text class="preview-popup__persona-avatar-text">
                      {{ (previewTemplate.author_name || '?').charAt(0) }}
                    </text>
                  </view>
                  <view class="preview-popup__persona-info">
                    <text v-if="previewTemplate.author_name" class="preview-popup__persona-name">
                      {{ previewTemplate.author_name }}
                    </text>
                    <text v-if="previewTemplate.author_style_intro" class="preview-popup__persona-intro">
                      {{ previewTemplate.author_style_intro }}
                    </text>
                  </view>
                </view>
              </view>
              <view v-if="previewTemplate.theme" class="preview-popup__block">
                <text class="preview-popup__block-label">排版主题</text>
                <text class="preview-popup__block-text">{{ themeLabel(previewTemplate.theme) }}</text>
              </view>
            </template>

            <!-- Tags -->
            <view v-if="previewTemplate.tags && previewTemplate.tags.length" class="preview-popup__tags">
              <text
                v-for="tag in previewTemplate.tags"
                :key="tag"
                class="preview-popup__tag"
              >
                {{ tag }}
              </text>
            </view>
          </view>
        </scroll-view>

        <!-- Footer actions -->
        <view class="preview-popup__footer">
          <view class="preview-popup__actions">
            <view
              v-if="previewTemplate.type === 'poster' || previewTemplate.type === 'article'"
              class="preview-popup__btn preview-popup__btn--primary"
              @tap="onUseTemplate(previewTemplate, 'article')"
            >
              <text class="preview-popup__btn-text preview-popup__btn-text--primary">用于公众号</text>
            </view>
            <view
              v-if="previewTemplate.type === 'seednote' || previewTemplate.type === 'poster'"
              class="preview-popup__btn preview-popup__btn--ghost"
              @tap="onUseTemplate(previewTemplate, 'seednote')"
            >
              <text class="preview-popup__btn-text preview-popup__btn-text--ghost">用于种草笔记</text>
            </view>
            <view
              v-if="previewTemplate.type === 'ecommerce'"
              class="preview-popup__btn preview-popup__btn--primary"
              @tap="onUseTemplate(previewTemplate, 'ecommerce')"
            >
              <text class="preview-popup__btn-text preview-popup__btn-text--primary">用于电商</text>
            </view>
            <view
              v-if="isOwner(previewTemplate)"
              class="preview-popup__btn preview-popup__btn--ghost"
              @tap="openEdit(previewTemplate)"
            >
              <text class="preview-popup__btn-text preview-popup__btn-text--ghost">编辑</text>
            </view>
            <view
              v-if="isOwner(previewTemplate)"
              class="preview-popup__btn preview-popup__btn--danger-ghost"
              @tap="onDelete(previewTemplate)"
            >
              <text class="preview-popup__btn-text preview-popup__btn-text--danger">删除</text>
            </view>
          </view>
        </view>
      </view>
    </view>

    <!-- Create / Edit Sheet -->
    <view v-if="formOpen" class="form-mask" @tap="closeForm" />
    <view class="form-sheet" :class="{ 'form-sheet--show': formOpen }">
      <view v-if="formOpen" class="form-sheet__content">
        <!-- Header -->
        <view class="form-sheet__header">
          <text class="form-sheet__title">{{ isEditing ? '编辑模板' : '新建模板' }}</text>
          <view class="form-sheet__close" @tap="closeForm">
            <text class="form-sheet__close-icon">×</text>
          </view>
        </view>

        <!-- Scrollable body -->
        <scroll-view class="form-sheet__body" scroll-y>
          <!-- Name -->
          <view class="form-section">
            <text class="field-label">
              名称
              <text class="field-hint-inline">（可选，留空将根据风格自动生成）</text>
            </text>
            <AbInput v-model="form.name" placeholder="例如：暖系生活感" />
          </view>

          <!-- Type + Visibility -->
          <view class="form-section">
            <text class="field-label">类别</text>
            <view class="picker-row" @tap="pickType">
              <text class="picker-row__value">{{ getTypeLabel(form.type) }}</text>
              <text class="picker-row__arrow">›</text>
            </view>
          </view>

          <view class="form-section">
            <view class="switch-row">
              <view class="switch-row__text">
                <text class="field-label switch-row__label">可见性</text>
                <text class="field-hint">{{ form.visibility === 'public' ? '公开（所有人可见）' : '私有（仅自己）' }}</text>
              </view>
              <AbSwitch
                :model-value="form.visibility === 'public'"
                @update:model-value="(v: boolean) => (form.visibility = v ? 'public' : 'private')"
              />
            </view>
          </view>

          <!-- Thumbnail + Style prompt -->
          <view class="form-section">
            <view class="form-section__head">
              <text class="field-label">视觉风格</text>
              <view class="form-section__head-right">
                <text v-if="analyzing" class="form-section__analyzing">识别中…</text>
                <text
                  v-else-if="form.thumbnail_url"
                  class="form-section__reanalyze"
                  @tap="analyzeStyle(true)"
                >
                  重新识别
                </text>
              </view>
            </view>
            <view class="thumb-row">
              <view class="thumb-box" @tap="chooseThumbnail">
                <image
                  v-if="form.thumbnail_url"
                  class="thumb-box__image"
                  :src="form.thumbnail_url"
                  mode="aspectFill"
                />
                <view v-else class="thumb-box__placeholder">
                  <text class="thumb-box__placeholder-text">{{ thumbUploading ? '上传中' : '+ 图片' }}</text>
                </view>
                <view v-if="thumbUploading || analyzing" class="thumb-box__overlay">
                  <AbLoading size="sm" />
                </view>
              </view>
              <view class="thumb-row__prompt">
                <AbTextarea
                  v-model="form.style_prompt"
                  placeholder="描述视觉风格（艺术流派、画面氛围、质感…）"
                  :rows="4"
                  :maxlength="1024"
                />
              </view>
            </view>
            <text class="field-hint">上传后系统会自动识别视觉风格，你也可以手动调整。</text>
          </view>

          <!-- Poster scaffold -->
          <view v-if="form.type === 'poster'" class="form-scaffold">
            <view class="form-scaffold__head">
              <text class="form-scaffold__title">内容脚手架</text>
              <text class="form-scaffold__hint">可选，建任务选此模板时送达 AI</text>
            </view>
            <view class="form-section">
              <text class="field-sublabel">写作风格 / 调性</text>
              <AbTextarea
                v-model="form.writing_style"
                placeholder="例如：犀利、接地气、像朋友聊天；多用短句和反问"
                :rows="2"
                :maxlength="1024"
              />
            </view>
            <view class="form-section">
              <text class="field-sublabel">内容结构</text>
              <AbTextarea
                v-model="form.structure"
                placeholder="例如：&#10;1. 开头钩子&#10;2. 3 个论点&#10;3. 行动号召"
                :rows="4"
              />
            </view>
            <view class="form-section">
              <text class="field-sublabel">示例内容</text>
              <AbTextarea
                v-model="form.example"
                placeholder="贴一段你认可的成稿片段，AI 会模仿它的语气与节奏"
                :rows="4"
              />
            </view>
            <view class="form-section">
              <text class="field-sublabel">分类</text>
              <AbInput v-model="form.category" placeholder="例如：个人成长" />
            </view>
            <view class="form-section">
              <text class="field-sublabel">标签（逗号分隔）</text>
              <AbInput v-model="form.tags_text" placeholder="例如：干货, 方法论" />
            </view>
          </view>

          <!-- Article: persona + theme -->
          <template v-if="form.type === 'article'">
            <view class="form-scaffold">
              <view class="form-scaffold__head">
                <text class="form-scaffold__title">作者署名 · 写作风格</text>
                <text class="form-scaffold__hint">署名=发布作者名 · 写作风格=供 AI 模仿的口吻</text>
              </view>
              <view v-if="writerResources.length > 0" class="form-section">
                <text class="field-sublabel">从写作风格库导入（仅填写作风格，不覆盖署名）</text>
                <view class="picker-row" @tap="pickWriter">
                  <text v-if="selectedWriterLabel" class="picker-row__value">{{ selectedWriterLabel }}</text>
                  <text v-else class="picker-row__placeholder">选择写作风格，自动填入简介</text>
                  <text class="picker-row__arrow">›</text>
                </view>
              </view>
              <view class="thumb-row">
                <view class="thumb-box thumb-box--avatar" @tap="chooseAuthorAvatar">
                  <image
                    v-if="form.author_avatar_url"
                    class="thumb-box__image"
                    :src="form.author_avatar_url"
                    mode="aspectFill"
                  />
                  <view v-else class="thumb-box__placeholder">
                    <text class="thumb-box__placeholder-text">{{ authorAvatarUploading ? '上传中' : '+ 头像' }}</text>
                  </view>
                  <view v-if="authorAvatarUploading" class="thumb-box__overlay">
                    <AbLoading size="sm" />
                  </view>
                </view>
                <view class="thumb-row__prompt">
                  <view class="form-section form-section--tight">
                    <text class="field-sublabel">发布署名 · 文章作者位显示的真实姓名/品牌</text>
                    <AbInput v-model="form.author_name" placeholder="例如：李雷、某某实验室" />
                  </view>
                  <view class="form-section form-section--tight">
                    <text class="field-sublabel">写作风格 · 供 AI 模仿的口吻</text>
                    <AbTextarea
                      v-model="form.author_style_intro"
                      placeholder="例如：犀利、接地气；多用短句和反问；爱用具体数字和案例"
                      :rows="3"
                      :maxlength="1024"
                    />
                  </view>
                </view>
              </view>
            </view>

            <view class="form-section">
              <text class="field-label">排版主题</text>
              <view class="picker-row" @tap="pickTheme">
                <text v-if="selectedThemeLabel" class="picker-row__value">{{ selectedThemeLabel }}</text>
                <text v-else class="picker-row__placeholder">可选，选择公众号排版主题</text>
                <text v-if="form.theme" class="picker-row__clear" @tap.stop="form.theme = ''">清除</text>
                <text v-else class="picker-row__arrow">›</text>
              </view>
            </view>
          </template>

          <!-- Ecommerce defaults -->
          <view v-if="form.type === 'ecommerce'" class="form-scaffold">
            <view class="form-scaffold__head">
              <text class="form-scaffold__title">电商默认配置</text>
              <text class="form-scaffold__hint">可选，建任务选此模板时自动带入</text>
            </view>
            <view class="form-section">
              <text class="field-sublabel">默认交付模块</text>
              <view class="module-list">
                <view
                  v-for="mod in ecommerceModuleCatalog"
                  :key="mod.key"
                  class="module-row"
                >
                  <view class="module-row__info">
                    <view class="module-row__title-row">
                      <text class="module-row__label">{{ mod.label }}</text>
                      <text class="module-row__ratio">{{ mod.ratio }}</text>
                    </view>
                  </view>
                  <view class="module-row__ctrl">
                    <AbSwitch
                      :model-value="(form.ecommerce_modules[mod.key] ?? 0) >= 1"
                      @update:model-value="(on: boolean) => toggleEcommerceModule(mod.key, on)"
                    />
                  </view>
                </view>
              </view>
            </view>
            <view class="form-section">
              <text class="field-sublabel">默认目标平台</text>
              <view class="picker-row" @tap="pickEcomPlatform">
                <text v-if="form.ecommerce_target_platform" class="picker-row__value">
                  {{ ecomPlatformLabel(form.ecommerce_target_platform) }}
                </text>
                <text v-else class="picker-row__placeholder">不指定（建任务时再选）</text>
                <text class="picker-row__arrow">›</text>
              </view>
            </view>
            <view class="form-section">
              <text class="field-sublabel">品牌定位 / 调性</text>
              <AbTextarea
                v-model="form.ecommerce_brand_brief"
                placeholder="例如：新锐国货美妆、主打成分党、高级简约视觉"
                :rows="2"
                :maxlength="1024"
              />
            </view>
          </view>
        </scroll-view>

        <!-- Footer -->
        <view class="form-sheet__footer">
          <view class="form-sheet__btn form-sheet__btn--ghost" @tap="closeForm">
            <text class="form-sheet__btn-text form-sheet__btn-text--ghost">取消</text>
          </view>
          <view
            class="form-sheet__btn form-sheet__btn--primary"
            :class="{ 'form-sheet__btn--disabled': submitting || analyzing }"
            @tap="submitForm"
          >
            <text class="form-sheet__btn-text form-sheet__btn-text--primary">
              {{ submitting ? '保存中' : isEditing ? '保存' : '创建' }}
            </text>
          </view>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { onShow, onPullDownRefresh } from '@dcloudio/uni-app'
import { templatesApi } from '@/api/templates'
import { resourcesApi } from '@/api/resources'
import { projectsApi } from '@/api/projects'
import { useAuthStore } from '@/stores/auth'
import type {
  Template,
  TemplateType,
  TemplateScope,
  TemplateVisibility,
  ResourceEntry,
} from '@/types'
import { contentTypeLabel, ecommerceModuleCatalog } from '@/utils/labels'
import { useList } from '@/composables/useList'
import AbTabs from '@/components/common/AbTabs.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'

const authStore = useAuthStore()

// ---- Filters ---------------------------------------------------------------
const scopeTabs: { value: TemplateScope; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'public', label: '公共' },
  { value: 'mine', label: '我的' },
]
const scope = ref<TemplateScope>('all')

const typeTabs = [
  { key: '', label: '全部' },
  { key: 'poster', label: '海报' },
  { key: 'seednote', label: '种草笔记' },
  { key: 'article', label: '公众号' },
  { key: 'ecommerce', label: '电商' },
]
const activeType = ref('')

const searchQuery = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null

// ---- List ------------------------------------------------------------------
const list = useList<Template>({
  fetchFn: async (params) => {
    const apiParams: Record<string, any> = {
      limit: params.limit,
      offset: params.offset,
      scope: scope.value,
    }
    if (activeType.value) {
      apiParams.type = activeType.value
    }
    if (searchQuery.value.trim()) {
      apiParams.tag = searchQuery.value.trim()
    }
    return templatesApi.list(apiParams)
  },
  pageSize: 20,
  immediate: false,
})

// Debounced search
watch(searchQuery, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    list.fetch(true)
  }, 300)
})

const filteredItems = computed(() => list.items.value)

function onScopeChange(s: TemplateScope) {
  if (scope.value === s) return
  scope.value = s
  list.fetch(true)
}

function onTypeChange(key: string) {
  activeType.value = key
  list.fetch(true)
}

function onPullRefresh() {
  list.refresh()
}

// ---- Preview ---------------------------------------------------------------
const previewTemplate = ref<Template | null>(null)
const detailLoading = ref(false)

async function onPreview(tpl: Template) {
  // Optimistically show the summary we already have, then fetch full detail so
  // poster scaffold / article persona fields render in the popup.
  previewTemplate.value = tpl
  if (!hasFullDetail(tpl)) {
    detailLoading.value = true
    try {
      const full = await templatesApi.get(tpl.id)
      // Only swap if the user hasn't closed the popup mid-fetch.
      if (previewTemplate.value && previewTemplate.value.id === tpl.id) {
        previewTemplate.value = full
      }
    } catch {
      // Keep the summary on failure
    } finally {
      detailLoading.value = false
    }
  }
}

function closePreview() {
  previewTemplate.value = null
  detailLoading.value = false
}

function hasFullDetail(tpl: Template): boolean {
  // Full detail carries either structure payload (poster) or article persona.
  return (
    (!!tpl.structure && Object.keys(tpl.structure).length > 0) ||
    !!tpl.author_name ||
    !!tpl.author_style_intro ||
    !!tpl.writing_style ||
    !!tpl.theme
  )
}

function isOwner(tpl: Template): boolean {
  const uid = authStore.user?.id
  return !!uid && !!tpl.user_id && tpl.user_id === uid
}

function isPrivate(tpl: Template): boolean {
  return tpl.visibility === 'private'
}

function scaffoldText(value: Record<string, unknown> | undefined): string {
  if (!value) return ''
  const text = (value as Record<string, unknown>).text
  return typeof text === 'string' ? text : ''
}

function getTypeLabel(type: TemplateType): string {
  if (type === 'poster') return '海报'
  if (type === 'ecommerce') return '电商'
  return contentTypeLabel[type] || type
}

function getTypeBadgeVariant(type: TemplateType): 'success' | 'danger' | 'warning' | 'info' | 'neutral' {
  if (type === 'seednote') return 'danger'
  if (type === 'article') return 'success'
  if (type === 'ecommerce') return 'warning'
  return 'neutral'
}

function themeLabel(theme: string): string {
  const found = themeResources.value.find((t) => t.name === theme)
  return found?.display_name || found?.description || found?.name || theme
}

function ecomPlatformLabel(value: string): string {
  const found = ECOM_PLATFORM_OPTIONS.find((o) => o.value === value)
  return found?.label || value
}

function onUseTemplate(tpl: Template, target: 'article' | 'seednote' | 'ecommerce') {
  closePreview()
  if (target === 'ecommerce' || tpl.type === 'ecommerce') {
    uni.navigateTo({
      url: `/pages/tasks/create?template_id=${tpl.id}&type=ecommerce`,
    })
    return
  }
  if (tpl.type === 'poster' && target === 'seednote') {
    // poster can also seed a seednote task
    uni.navigateTo({
      url: `/pages/tasks/create?template_id=${tpl.id}&type=seednote`,
    })
    return
  }
  uni.navigateTo({
    url: `/pages/tasks/create?template_id=${tpl.id}&type=${target}`,
  })
}

// ---- Delete ----------------------------------------------------------------
function onDelete(tpl: Template) {
  uni.showModal({
    title: '删除模板？',
    content: `确定删除「${tpl.name}」吗？此操作无法撤销。已使用该模板创建的任务不受影响。`,
    confirmColor: '#DC2626',
    success: async (res) => {
      if (!res.confirm) return
      try {
        await templatesApi.remove(tpl.id)
        uni.showToast({ title: '已删除', icon: 'success' })
        closePreview()
        list.refresh()
      } catch (err: any) {
        uni.showToast({ title: err?.message || '删除失败', icon: 'none' })
      }
    },
  })
}

// ---- Create / Edit form ----------------------------------------------------
const ECOM_PLATFORM_OPTIONS = [
  { value: 'taobao', label: '淘宝 / 天猫' },
  { value: 'jd', label: '京东' },
  { value: 'douyin', label: '抖音电商' },
  { value: 'xhs', label: '小红书电商' },
  { value: 'general', label: '通用' },
]

const TYPE_OPTIONS: { value: TemplateType; label: string }[] = [
  { value: 'poster', label: '海报' },
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'ecommerce', label: '电商' },
]

interface TemplateFormState {
  id: string
  name: string
  type: TemplateType
  thumbnail_url: string
  style_prompt: string
  visibility: TemplateVisibility
  // poster scaffold
  writing_style: string
  structure: string
  example: string
  category: string
  tags_text: string
  // article
  theme: string
  author_name: string
  author_style_intro: string
  author_avatar_url: string
  // ecommerce defaults
  ecommerce_modules: Record<string, number>
  ecommerce_target_platform: string
  ecommerce_brand_brief: string
}

const formOpen = ref(false)
const isEditing = ref(false)
const submitting = ref(false)
const analyzing = ref(false)
const thumbUploading = ref(false)
const authorAvatarUploading = ref(false)

// Monotonic id guard so stale analyze results don't clobber newer state.
let analyzeReqId = 0

const themeResources = ref<ResourceEntry[]>([])
const writerResources = ref<ResourceEntry[]>([])
const selectedWriterName = ref<string>('')

const form = reactive<TemplateFormState>({
  id: '',
  name: '',
  type: 'seednote',
  thumbnail_url: '',
  style_prompt: '',
  visibility: 'public',
  writing_style: '',
  structure: '',
  example: '',
  category: '',
  tags_text: '',
  theme: '',
  author_name: '',
  author_style_intro: '',
  author_avatar_url: '',
  ecommerce_modules: {},
  ecommerce_target_platform: '',
  ecommerce_brand_brief: '',
})

const selectedThemeLabel = computed(() => {
  if (!form.theme) return ''
  return themeLabel(form.theme)
})

const selectedWriterLabel = computed(() => {
  if (!selectedWriterName.value) return ''
  const w = writerResources.value.find(
    (x) => x.name === selectedWriterName.value || x.english_name === selectedWriterName.value,
  )
  if (!w) return selectedWriterName.value
  return w.category_cn ? `${w.name}（${w.category_cn}）` : w.name
})

function resetForm() {
  form.id = ''
  form.name = ''
  form.type = 'seednote'
  form.thumbnail_url = ''
  form.style_prompt = ''
  form.visibility = 'public'
  form.writing_style = ''
  form.structure = ''
  form.example = ''
  form.category = ''
  form.tags_text = ''
  form.theme = ''
  form.author_name = ''
  form.author_style_intro = ''
  form.author_avatar_url = ''
  form.ecommerce_modules = {}
  form.ecommerce_target_platform = ''
  form.ecommerce_brand_brief = ''
  selectedWriterName.value = ''
  analyzing.value = false
  thumbUploading.value = false
  authorAvatarUploading.value = false
}

function openCreate() {
  resetForm()
  isEditing.value = false
  formOpen.value = true
}

function openEdit(tpl: Template) {
  resetForm()
  isEditing.value = true
  form.id = tpl.id
  form.name = tpl.name
  form.type = tpl.type
  form.thumbnail_url = tpl.thumbnail_url
  form.style_prompt = tpl.style_prompt
  form.visibility = tpl.visibility === 'private' ? 'private' : 'public'
  form.writing_style = tpl.writing_style ?? ''
  form.theme = tpl.theme ?? ''
  form.author_name = tpl.author_name ?? ''
  form.author_style_intro = tpl.author_style_intro ?? ''
  form.author_avatar_url = tpl.author_avatar_url ?? ''
  form.structure = scaffoldText(tpl.structure)
  form.example = scaffoldText(tpl.example_content)
  form.category = tpl.category ?? ''
  form.tags_text = Array.isArray(tpl.tags) ? tpl.tags.join(', ') : ''
  form.ecommerce_modules = { ...(tpl.ecommerce?.default_selected_modules ?? {}) }
  form.ecommerce_target_platform = tpl.ecommerce?.target_platform ?? ''
  form.ecommerce_brand_brief = tpl.ecommerce?.brand_brief ?? ''
  closePreview()
  formOpen.value = true
}

function closeForm() {
  // Don't close mid-submit; user can cancel after it settles or fails.
  if (submitting.value) return
  formOpen.value = false
}

// ---- Pickers ---------------------------------------------------------------
function pickType() {
  const labels = TYPE_OPTIONS.map((o) => o.label)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      form.type = TYPE_OPTIONS[res.tapIndex].value
    },
  })
}

function pickTheme() {
  if (themeResources.value.length === 0) {
    uni.showToast({ title: '暂无可用主题', icon: 'none' })
    return
  }
  const labels = themeResources.value.map((t) => t.display_name || t.description || t.name)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const theme = themeResources.value[res.tapIndex]
      if (theme) form.theme = theme.name
    },
  })
}

function pickWriter() {
  if (writerResources.value.length === 0) {
    uni.showToast({ title: '暂无可用风格', icon: 'none' })
    return
  }
  const sorted = [...writerResources.value].sort((a, b) =>
    (a.name || '').localeCompare(b.name || ''),
  )
  const labels = sorted.map((w) => (w.category_cn ? `${w.name}（${w.category_cn}）` : w.name))
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const w = sorted[res.tapIndex]
      if (!w) return
      selectedWriterName.value = w.name
      // Only fill writing-style intro, never overwrite author name (byline).
      if (w.description) form.author_style_intro = w.description
    },
  })
}

function pickEcomPlatform() {
  const labels = ECOM_PLATFORM_OPTIONS.map((o) => o.label)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      form.ecommerce_target_platform = ECOM_PLATFORM_OPTIONS[res.tapIndex].value
    },
  })
}

function toggleEcommerceModule(key: string, on: boolean) {
  const next = { ...form.ecommerce_modules }
  if (on) {
    next[key] = next[key] && next[key] >= 1 ? next[key] : 1
  } else {
    delete next[key]
  }
  form.ecommerce_modules = next
}

// ---- Image upload + analyze ------------------------------------------------
function chooseThumbnail() {
  if (thumbUploading.value || analyzing.value) return
  uni.chooseImage({
    count: 1,
    success: async (chosen) => {
      const filePath = chosen.tempFilePaths?.[0]
      if (!filePath) return
      thumbUploading.value = true
      try {
        const result = await projectsApi.uploadImage(filePath, 'reference')
        form.thumbnail_url = result.url
        uni.showToast({ title: '上传成功', icon: 'success' })
        // Auto-analyze when uploading a new image (skip when editing existing url)
        void analyzeStyle(false)
      } catch (err: any) {
        uni.showToast({ title: err?.message || '上传失败', icon: 'none' })
      } finally {
        thumbUploading.value = false
      }
    },
  })
}

function chooseAuthorAvatar() {
  if (authorAvatarUploading.value) return
  uni.chooseImage({
    count: 1,
    success: async (chosen) => {
      const filePath = chosen.tempFilePaths?.[0]
      if (!filePath) return
      authorAvatarUploading.value = true
      try {
        const result = await projectsApi.uploadImage(filePath, 'project')
        form.author_avatar_url = result.url
        uni.showToast({ title: '上传成功', icon: 'success' })
      } catch (err: any) {
        uni.showToast({ title: err?.message || '上传失败', icon: 'none' })
      } finally {
        authorAvatarUploading.value = false
      }
    },
  })
}

// force=true (manual re-analyze) overwrites existing style; force=false only
// fills empty fields (called automatically after a fresh upload).
async function analyzeStyle(force: boolean) {
  const url = form.thumbnail_url
  if (!url) {
    uni.showToast({ title: '请先上传图片', icon: 'none' })
    return
  }
  // Skip auto-analyze if the URL was loaded from the template being edited.
  if (!force && isEditing.value) return
  const reqId = ++analyzeReqId
  analyzing.value = true
  try {
    const res = await projectsApi.analyzeImage(url)
    if (reqId !== analyzeReqId) return
    if (!res.style) return
    if (force || !form.style_prompt.trim()) {
      form.style_prompt = res.style
    }
    if (!form.name.trim()) {
      form.name = deriveTemplateName(res.style)
    }
  } catch (err: any) {
    if (reqId !== analyzeReqId) return
    uni.showToast({ title: err?.message || '风格识别失败，请手动填写', icon: 'none' })
  } finally {
    if (reqId === analyzeReqId) {
      analyzing.value = false
    }
  }
}

// Derive a default name from the first clause of the style description (≤20 chars).
function deriveTemplateName(style: string): string {
  const firstClause = style.trim().split(/[\n。，,.]/)[0]
  return firstClause.slice(0, 20).trim()
}

// ---- Submit ----------------------------------------------------------------
async function submitForm() {
  // Name is optional; fall back to deriving from style_prompt.
  const finalName = form.name.trim() || deriveTemplateName(form.style_prompt)
  if (!finalName) {
    uni.showToast({ title: '请上传图片或填写模板名称', icon: 'none' })
    return
  }
  if (!form.thumbnail_url) {
    uni.showToast({ title: '请上传一张图片', icon: 'none' })
    return
  }
  if (submitting.value) return
  submitting.value = true
  try {
    const trimmedTags = form.tags_text
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean)

    // Type-aware payload: only include fields the current type renders. This
    // guards the writer-key trap (article/seednote must NOT carry writing_style
    // — backend style_resolve would copy it into Task.WritingStyle).
    const payload: Record<string, any> = {
      name: finalName,
      type: form.type,
      thumbnail_url: form.thumbnail_url,
      style_prompt: form.style_prompt.trim(),
      visibility: form.visibility,
    }

    if (form.type === 'poster') {
      const writingStyleTrimmed = form.writing_style.trim()
      if (writingStyleTrimmed) payload.writing_style = writingStyleTrimmed
      const structureTrimmed = form.structure.trim()
      if (structureTrimmed) payload.structure = structureTrimmed
      const exampleTrimmed = form.example.trim()
      if (exampleTrimmed) payload.example_content = exampleTrimmed
      const categoryTrimmed = form.category.trim()
      if (categoryTrimmed) payload.category = categoryTrimmed
      if (trimmedTags.length > 0) payload.tags = trimmedTags
    }

    if (form.type === 'article') {
      const themeTrimmed = form.theme.trim()
      if (themeTrimmed) payload.theme = themeTrimmed
      const authorNameTrimmed = form.author_name.trim()
      if (authorNameTrimmed) payload.author_name = authorNameTrimmed
      const authorAvatarTrimmed = form.author_avatar_url.trim()
      if (authorAvatarTrimmed) payload.author_avatar_url = authorAvatarTrimmed
      const authorIntroTrimmed = form.author_style_intro.trim()
      if (authorIntroTrimmed) payload.author_style_intro = authorIntroTrimmed
    }

    if (form.type === 'ecommerce') {
      const modules: Record<string, number> = {}
      for (const [k, q] of Object.entries(form.ecommerce_modules)) {
        if (q >= 1) modules[k] = q
      }
      const ecommerce: Record<string, any> = {}
      if (Object.keys(modules).length > 0) ecommerce.default_selected_modules = modules
      if (form.ecommerce_target_platform) ecommerce.target_platform = form.ecommerce_target_platform
      const brandBriefTrimmed = form.ecommerce_brand_brief.trim()
      if (brandBriefTrimmed) ecommerce.brand_brief = brandBriefTrimmed
      if (Object.keys(ecommerce).length > 0) payload.ecommerce = ecommerce
    }

    if (isEditing.value && form.id) {
      await templatesApi.update(form.id, payload as any)
      uni.showToast({ title: '模板已更新', icon: 'success' })
    } else {
      await templatesApi.create(payload as any)
      uni.showToast({ title: '模板已创建', icon: 'success' })
    }
    formOpen.value = false
    list.refresh()
  } catch (err: any) {
    uni.showToast({
      title: err?.message || (isEditing.value ? '更新失败，请重试' : '创建失败，请重试'),
      icon: 'none',
    })
  } finally {
    submitting.value = false
  }
}

// ---- Resources -------------------------------------------------------------
async function loadResources() {
  // Themes are platform-scoped; load article themes (the only type that uses them).
  try {
    const res = await resourcesApi.list('themes', 'article')
    themeResources.value = res.items || []
  } catch {
    themeResources.value = []
  }
  try {
    const res = await resourcesApi.list('writers')
    writerResources.value = res.items || []
  } catch {
    writerResources.value = []
  }
}

// ---- Lifecycle -------------------------------------------------------------
// onPullDownRefresh is exposed as a Vue page hook; uni-app invokes it from the
// page config's enablePullDownRefresh.
onPullDownRefresh(() => {
  list.refresh()
})

onMounted(() => {
  list.fetch(true)
  loadResources()
})

// Refresh when navigating back (e.g. after using a template).
onShow(() => {
  if (list.items.value.length > 0) {
    list.fetch(true)
  }
})
</script>

<style lang="scss" scoped>
.page {
  min-height: 100vh;
  background-color: $ab-background;
  display: flex;
  flex-direction: column;
}

// Scope bar
.scope-bar {
  display: flex;
  align-items: center;
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-surface;
  gap: $ab-space-xs;

  &__item {
    padding: $ab-space-xs $ab-space-md;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
    transition: all 0.2s ease;

    &--active {
      background-color: $ab-primary;
    }
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;

    .scope-bar__item--active & {
      color: #ffffff;
      font-weight: $ab-font-medium;
    }
  }

  &__spacer {
    flex: 1;
  }

  &__create {
    display: flex;
    align-items: center;
    gap: 4rpx;
    padding: $ab-space-xs $ab-space-md;
    background-color: $ab-primary-bg;
    border-radius: $ab-radius-sm;

    &:active {
      opacity: 0.7;
    }
  }

  &__create-icon {
    font-size: $ab-text-md;
    color: $ab-primary;
    line-height: 1;
  }

  &__create-text {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}

// Search Bar
.search-bar {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-surface;

  &__count {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    flex-shrink: 0;
  }
}

// Loading
.templates-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 120rpx 0;
}

// Scroll + Grid
.templates-scroll {
  flex: 1;
  height: 0;
}

.templates-grid {
  display: flex;
  flex-wrap: wrap;
  padding: $ab-space-sm $ab-space-sm;
  gap: $ab-space-sm;
}

// Template Card
.template-card {
  width: calc(50% - #{$ab-space-sm} / 2);
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  overflow: hidden;
  box-shadow: $ab-shadow-sm;
  transition: transform 0.15s ease;

  &:active {
    transform: scale(0.98);
  }

  &__thumb {
    position: relative;
    width: 100%;
    height: 280rpx;
    background-color: $ab-divider;
    overflow: hidden;
  }

  &__image {
    width: 100%;
    height: 100%;
  }

  &__placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: $ab-primary-bg;
  }

  &__placeholder-text {
    font-size: 80rpx;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    opacity: 0.4;
  }

  &__type-badge {
    position: absolute;
    top: $ab-space-xs;
    left: $ab-space-xs;
  }

  &__vis-badge {
    position: absolute;
    top: $ab-space-xs;
    right: $ab-space-xs;
    background-color: rgba(17, 24, 39, 0.6);
    padding: 2rpx 12rpx;
    border-radius: $ab-radius-full;
  }

  &__vis-text {
    font-size: $ab-text-xs;
    color: #ffffff;
    line-height: 1.4;
  }

  &__info {
    padding: $ab-space-xs $ab-space-sm $ab-space-sm;
  }

  &__name {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
    line-height: 1.3;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-bottom: 6rpx;
  }

  &__prompt {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-bottom: 8rpx;
  }

  &__persona {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    margin-bottom: 8rpx;
  }

  &__avatar {
    width: 36rpx;
    height: 36rpx;
    border-radius: 50%;
    flex-shrink: 0;
    background-color: $ab-divider;

    &--placeholder {
      display: flex;
      align-items: center;
      justify-content: center;
      background-color: $ab-primary-bg;
    }
  }

  &__avatar-text {
    font-size: $ab-text-xs;
    color: $ab-primary;
    line-height: 1;
  }

  &__author {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6rpx;
  }

  &__tag {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    background-color: $ab-divider;
    padding: 2rpx 10rpx;
    border-radius: $ab-radius-full;
    line-height: 1.4;

    &--more {
      color: $ab-text-tertiary;
    }
  }
}

// Load More
.load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md 0 $ab-space-xl;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__end {
    display: block;
    text-align: center;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    padding: $ab-space-md 0 $ab-space-xl;
  }
}

// Preview Popup
.preview-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 100;
}

.preview-popup {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg $ab-radius-lg 0 0;
  z-index: 101;
  transform: translateY(100%);
  transition: transform 0.3s ease;
  max-height: 85vh;
  display: flex;
  flex-direction: column;

  &--show {
    transform: translateY(0);
  }

  &__content {
    position: relative;
    display: flex;
    flex-direction: column;
    max-height: 85vh;
  }

  &__close {
    position: absolute;
    top: $ab-space-sm;
    right: $ab-space-sm;
    width: 56rpx;
    height: 56rpx;
    border-radius: 50%;
    background-color: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2;
  }

  &__close-icon {
    font-size: $ab-text-lg;
    color: #ffffff;
    line-height: 1;
  }

  &__body {
    flex: 1;
    min-height: 0;
  }

  &__thumb {
    position: relative;
    width: 100%;
    height: 400rpx;
    background-color: $ab-divider;
    overflow: hidden;
  }

  &__image {
    width: 100%;
    height: 100%;
  }

  &__placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: $ab-primary-bg;
  }

  &__placeholder-text {
    font-size: 120rpx;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    opacity: 0.3;
  }

  &__badges {
    position: absolute;
    top: $ab-space-sm;
    left: $ab-space-sm;
    display: flex;
    gap: $ab-space-xs;
    z-index: 1;
  }

  &__info {
    padding: $ab-space-md $ab-space-lg;
  }

  &__name {
    font-size: $ab-text-xl;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    margin-bottom: $ab-space-xs;
    display: block;
  }

  &__meta {
    margin-bottom: $ab-space-sm;
  }

  &__category {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__block {
    margin-top: $ab-space-sm;
    padding: $ab-space-sm;
    background-color: $ab-background;
    border-radius: $ab-radius-sm;
  }

  &__block-label {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-bottom: $ab-space-xs;
  }

  &__block-text {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text;
    line-height: 1.6;
  }

  &__persona {
    display: flex;
    align-items: flex-start;
    gap: $ab-space-sm;
  }

  &__persona-avatar {
    width: 96rpx;
    height: 96rpx;
    border-radius: 50%;
    flex-shrink: 0;
    background-color: $ab-divider;

    &--placeholder {
      display: flex;
      align-items: center;
      justify-content: center;
      background-color: $ab-primary-bg;
    }
  }

  &__persona-avatar-text {
    font-size: $ab-text-md;
    color: $ab-primary;
    line-height: 1;
  }

  &__persona-info {
    flex: 1;
    min-width: 0;
  }

  &__persona-name {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
    margin-bottom: 4rpx;
  }

  &__persona-intro {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.5;
  }

  &__tags {
    display: flex;
    flex-wrap: wrap;
    gap: $ab-space-xs;
    margin-top: $ab-space-sm;
  }

  &__tag {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    background-color: $ab-divider;
    padding: 4rpx 16rpx;
    border-radius: $ab-radius-full;
    line-height: 1.4;
  }

  &__footer {
    padding: $ab-space-sm $ab-space-lg;
    padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom));
    border-top: 2rpx solid $ab-divider;
    background-color: $ab-surface;
  }

  &__actions {
    display: flex;
    flex-wrap: wrap;
    gap: $ab-space-sm;
  }

  &__btn {
    flex: 1;
    min-width: 200rpx;
    padding: $ab-space-sm 0;
    border-radius: $ab-radius-sm;
    text-align: center;
    transition: opacity 0.2s ease;

    &:active {
      opacity: 0.7;
    }

    &--primary {
      background-color: $ab-primary;
    }

    &--ghost {
      background-color: transparent;
      border: 2rpx solid $ab-border;
    }

    &--danger-ghost {
      background-color: transparent;
      border: 2rpx solid $ab-danger;
    }
  }

  &__btn-text {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;

    &--primary {
      color: #ffffff;
    }

    &--ghost {
      color: $ab-text;
    }

    &--danger {
      color: $ab-danger;
    }
  }
}

// Create / Edit Sheet
.form-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 110;
}

.form-sheet {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg $ab-radius-lg 0 0;
  z-index: 111;
  transform: translateY(100%);
  transition: transform 0.3s ease;
  max-height: 90vh;
  display: flex;
  flex-direction: column;

  &--show {
    transform: translateY(0);
  }

  &__content {
    position: relative;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: $ab-space-md $ab-space-lg;
    border-bottom: 2rpx solid $ab-divider;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__close {
    width: 56rpx;
    height: 56rpx;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: $ab-divider;
  }

  &__close-icon {
    font-size: $ab-text-lg;
    color: $ab-text-secondary;
    line-height: 1;
  }

  &__body {
    flex: 1;
    min-height: 0;
    padding: 0 $ab-space-lg $ab-space-md;
  }

  &__footer {
    display: flex;
    gap: $ab-space-sm;
    padding: $ab-space-sm $ab-space-lg;
    padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom));
    border-top: 2rpx solid $ab-divider;
    background-color: $ab-surface;
  }

  &__btn {
    flex: 1;
    padding: $ab-space-sm 0;
    border-radius: $ab-radius-sm;
    text-align: center;
    transition: opacity 0.2s ease;

    &:active {
      opacity: 0.7;
    }

    &--primary {
      background-color: $ab-primary;
    }

    &--ghost {
      background-color: transparent;
      border: 2rpx solid $ab-border;
    }

    &--disabled {
      opacity: 0.5;
    }
  }

  &__btn-text {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;

    &--primary {
      color: #ffffff;
    }

    &--ghost {
      color: $ab-text;
    }
  }
}

// Form fields
.form-section {
  margin-top: $ab-space-md;

  &--tight {
    margin-top: $ab-space-sm;
  }

  &__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-xs;
  }

  &__head-right {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
  }

  &__analyzing {
    font-size: $ab-text-xs;
    color: $ab-primary;
  }

  &__reanalyze {
    font-size: $ab-text-xs;
    color: $ab-primary;
    padding: 4rpx 12rpx;
  }
}

.form-scaffold {
  margin-top: $ab-space-md;
  padding: $ab-space-md $ab-space-md $ab-space-sm;
  background-color: $ab-background;
  border-radius: $ab-radius-sm;
  border: 2rpx dashed $ab-border;

  &__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-xs;
  }

  &__title {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.field-label {
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
  display: block;
  margin-bottom: $ab-space-xs;
}

.field-hint-inline {
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  font-weight: $ab-font-normal;
}

.field-sublabel {
  font-size: $ab-text-sm;
  color: $ab-text-secondary;
  display: block;
  margin-bottom: $ab-space-xs;
}

.field-hint {
  display: block;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  line-height: 1.4;
  margin-top: $ab-space-xs;
}

// Thumb + style row
.thumb-row {
  display: flex;
  gap: $ab-space-sm;

  &__prompt {
    flex: 1;
    min-width: 0;
  }
}

.thumb-box {
  position: relative;
  width: 180rpx;
  height: 180rpx;
  border-radius: $ab-radius-sm;
  overflow: hidden;
  background-color: $ab-divider;
  flex-shrink: 0;
  border: 2rpx dashed $ab-border;
  display: flex;
  align-items: center;
  justify-content: center;

  &--avatar {
    width: 120rpx;
    height: 120rpx;
    border-radius: 50%;
  }

  &__image {
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

  &__placeholder-text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__overlay {
    position: absolute;
    inset: 0;
    background-color: rgba(255, 255, 255, 0.7);
    display: flex;
    align-items: center;
    justify-content: center;
  }
}

// Switch row
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

// Picker row
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

// E-commerce module list
.module-list {
  background-color: $ab-surface;
  border-radius: $ab-radius-sm;
  border: 2rpx solid $ab-border;
}

.module-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-sm $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__info {
    flex: 1;
    min-width: 0;
  }

  &__title-row {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__ratio {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    background-color: $ab-divider;
    padding: 2rpx 10rpx;
    border-radius: $ab-radius-full;
  }
}
</style>
