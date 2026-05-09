<template>
  <view class="page">
    <!-- Type Filter Tabs -->
    <AbTabs :tabs="typeTabs" :model-value="activeType" @update:model-value="onTypeChange" />

    <!-- Search Bar -->
    <view class="search-bar">
      <AbInput
        v-model="searchQuery"
        placeholder="搜索模板..."
      />
    </view>

    <!-- Content -->
    <view v-if="list.refreshing && !list.items.value.length" class="templates-loading">
      <AbLoading text="加载中..." />
    </view>

    <AbEmpty
      v-else-if="!list.items.value.length && !list.loading.value"
      title="暂无模板"
      description="当前筛选条件下没有找到模板"
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
          </view>

          <!-- Info -->
          <view class="template-card__info">
            <text class="template-card__name">{{ tpl.name }}</text>
            <view v-if="tpl.tags && tpl.tags.length" class="template-card__tags">
              <text
                v-for="tag in tpl.tags.slice(0, 3)"
                :key="tag"
                class="template-card__tag"
              >
                {{ tag }}
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
    <view v-if="previewTemplate" class="preview-mask" @tap="previewTemplate = null" />
    <view class="preview-popup" :class="{ 'preview-popup--show': previewTemplate }">
      <view v-if="previewTemplate" class="preview-popup__content">
        <!-- Close button -->
        <view class="preview-popup__close" @tap="previewTemplate = null">
          <text class="preview-popup__close-icon">x</text>
        </view>

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
        </view>

        <!-- Info -->
        <view class="preview-popup__info">
          <text class="preview-popup__name">{{ previewTemplate.name }}</text>
          <view class="preview-popup__meta">
            <text class="preview-popup__category">{{ previewTemplate.category }}</text>
            <AbBadge size="sm" :variant="getTypeBadgeVariant(previewTemplate.type)">
              {{ getTypeLabel(previewTemplate.type) }}
            </AbBadge>
          </view>
          <text v-if="previewTemplate.style_prompt" class="preview-popup__prompt">
            {{ truncate(previewTemplate.style_prompt, 120) }}
          </text>
        </view>

        <!-- Use Template Button -->
        <view class="preview-popup__footer">
          <view class="preview-popup__use" @tap="onUseTemplate(previewTemplate)">
            <text class="preview-popup__use-text">使用此模板</text>
          </view>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { templatesApi } from '@/api/templates'
import type { Template, TemplateType, PaginatedResponse } from '@/types'
import { contentTypeLabel } from '@/utils/labels'
import { useList } from '@/composables/useList'
import AbTabs from '@/components/common/AbTabs.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbBadge from '@/components/common/AbBadge.vue'

// Type tabs
const typeTabs = [
  { key: '', label: '全部' },
  { key: 'poster', label: '海报' },
  { key: 'rednote', label: '小红书' },
  { key: 'article', label: '公众号' },
  { key: 'xls', label: '小绿书' },
]
const activeType = ref('')

// Search
const searchQuery = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null

// List with pagination
const list = useList<Template>({
  fetchFn: async (params) => {
    const apiParams: Record<string, any> = {
      limit: params.limit,
      offset: params.offset,
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

// Filtered items (client-side search is not needed since API handles it)
const filteredItems = computed(() => list.items.value)

function onTypeChange(key: string) {
  activeType.value = key
  list.fetch(true)
}

function onPullRefresh() {
  list.refresh()
}

// Preview
const previewTemplate = ref<Template | null>(null)

function onPreview(tpl: Template) {
  previewTemplate.value = tpl
}

function truncate(str: string, maxLen: number): string {
  if (!str) return ''
  return str.length > maxLen ? str.slice(0, maxLen) + '...' : str
}

function getTypeLabel(type: TemplateType): string {
  if (type === 'poster') return '海报'
  return contentTypeLabel[type] || type
}

function getTypeBadgeVariant(type: TemplateType): 'success' | 'danger' | 'warning' | 'info' | 'neutral' {
  if (type === 'rednote') return 'danger'
  if (type === 'article' || type === 'xls') return 'success'
  return 'neutral'
}

function onUseTemplate(tpl: Template) {
  previewTemplate.value = null
  if (tpl.type === 'poster') {
    uni.navigateTo({ url: '/pages/workshop/poster?template_id=' + tpl.id })
  } else {
    uni.navigateTo({
      url: `/pages/tasks/create?template_id=${tpl.id}&type=${tpl.type}`,
    })
  }
}

// Initial load
import { onMounted } from 'vue'
onMounted(() => {
  list.fetch(true)
})
</script>

<style lang="scss" scoped>
.page {
  min-height: 100vh;
  background-color: $ab-background;
  display: flex;
  flex-direction: column;
}

// Search Bar
.search-bar {
  padding: $ab-space-sm $ab-space-md;
  background-color: $ab-surface;
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
    margin-bottom: 8rpx;
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
  max-height: 80vh;

  &--show {
    transform: translateY(0);
  }

  &__content {
    position: relative;
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
    color: #FFFFFF;
    line-height: 1;
  }

  &__thumb {
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

  &__info {
    padding: $ab-space-md $ab-space-lg;
  }

  &__name {
    font-size: $ab-text-xl;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    margin-bottom: $ab-space-xs;
  }

  &__meta {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__category {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__prompt {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.6;
    background-color: $ab-background;
    padding: $ab-space-sm;
    border-radius: $ab-radius-sm;
  }

  &__footer {
    padding: $ab-space-md $ab-space-lg;
    padding-bottom: calc(#{$ab-space-lg} + env(safe-area-inset-bottom));
  }

  &__use {
    background-color: $ab-primary;
    border-radius: $ab-radius-md;
    padding: $ab-space-sm 0;
    text-align: center;
  }

  &__use-text {
    font-size: $ab-text-md;
    color: #FFFFFF;
    font-weight: $ab-font-semibold;
  }
}
</style>
