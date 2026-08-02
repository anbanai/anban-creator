<template>
  <view class="page">
    <scroll-view class="category-scroll" scroll-x :show-scrollbar="false">
      <view class="category-row">
        <view
          v-for="item in categoryOptions"
          :key="item.key"
          class="category-item"
          :class="{ 'category-item--active': category === item.key }"
          @tap="selectCategory(item.key)"
        >
          <text>{{ item.label }}</text>
        </view>
      </view>
    </scroll-view>

    <view v-if="list.refreshing.value && !list.items.value.length" class="state-block">
      <AbLoading text="加载中..." />
    </view>

    <AbEmpty
      v-else-if="!list.loading.value && !list.items.value.length"
      title="这个分类还没有模板"
      description="模板由管理员创建后会显示在这里"
    />

    <scroll-view
      v-else
      class="template-scroll"
      scroll-y
      :refresher-enabled="true"
      :refresher-triggered="list.refreshing.value"
      @refresherrefresh="list.refresh"
      @scrolltolower="list.loadMore"
    >
      <view class="template-grid">
        <view
          v-for="tpl in list.items.value"
          :key="tpl.id"
          class="template-item"
          @tap="applyTemplate(tpl)"
        >
          <view class="template-thumb">
            <image
              v-if="tpl.thumbnail_url"
              class="template-image"
              :src="tpl.thumbnail_url"
              :alt="tpl.name"
              mode="aspectFill"
            />
            <view v-else class="template-placeholder">
              <text>{{ tpl.name.charAt(0) }}</text>
            </view>
          </view>
          <text class="template-name">{{ tpl.name }}</text>
          <text class="template-prompt">{{ tpl.prompt }}</text>
        </view>
      </view>

      <view v-if="list.loading.value" class="load-more">
        <AbLoading size="sm" />
      </view>
    </scroll-view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { onPullDownRefresh } from '@dcloudio/uni-app'
import { templatesApi } from '@/api/templates'
import { useList } from '@/composables/useList'
import {
  SEEDNOTE_TEMPLATE_CATEGORIES,
  type SeednoteTemplateCategory,
  type Template,
} from '@/types'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'

type CategoryFilter = '' | SeednoteTemplateCategory

const category = ref<CategoryFilter>('')
const categoryOptions: Array<{ key: CategoryFilter; label: string }> = [
  { key: '', label: '全部' },
  ...SEEDNOTE_TEMPLATE_CATEGORIES.map((item) => ({ key: item, label: item })),
]

const list = useList<Template>({
  fetchFn: ({ limit, offset }) => templatesApi.list({
    type: 'seednote',
    ...(category.value ? { category: category.value } : {}),
    limit,
    offset,
  }),
  pageSize: 20,
  immediate: false,
})

function selectCategory(next: CategoryFilter) {
  if (category.value === next) return
  category.value = next
  void list.fetch(true)
}

function applyTemplate(tpl: Template) {
  uni.navigateTo({
    url: `/pages/tasks/create?type=seednote&prompt=${encodeURIComponent(tpl.prompt)}`,
  })
}

onPullDownRefresh(() => {
  void list.refresh()
})

onMounted(() => {
  void list.fetch(true)
})
</script>

<style lang="scss" scoped>
.page {
  box-sizing: border-box;
  min-height: 100vh;
  padding: $ab-space-lg;
  background: $ab-background;
}

.category-scroll {
  width: 100%;
  margin-bottom: $ab-space-lg;
  white-space: nowrap;
}

.category-row {
  display: inline-flex;
  gap: $ab-space-sm;
  padding-bottom: $ab-space-xs;
}

.category-item {
  padding: $ab-space-sm $ab-space-md;
  color: $ab-text-secondary;
  font-size: $ab-text-sm;
  background: $ab-surface;
  border: 1rpx solid $ab-border;
  border-radius: $ab-radius-md;

  &--active {
    color: $ab-primary;
    border-color: $ab-primary;
  }
}

.state-block {
  display: flex;
  min-height: 320rpx;
  align-items: center;
  justify-content: center;
}

.template-scroll {
  height: calc(100vh - 150rpx);
}

.template-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: $ab-space-lg $ab-space-md;
}

.template-item {
  min-width: 0;
}

.template-thumb {
  position: relative;
  width: 100%;
  padding-top: 133.333%;
  overflow: hidden;
  background: $ab-divider;
  border: 1rpx solid $ab-border;
  border-radius: $ab-radius-md;
}

.template-image,
.template-placeholder {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.template-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  color: $ab-text-tertiary;
  font-size: $ab-text-xl;
}

.template-name,
.template-prompt {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
}

.template-name {
  margin-top: $ab-space-sm;
  color: $ab-text;
  font-size: $ab-text-base;
  font-weight: $ab-font-semibold;
  line-height: 1.4;
  -webkit-line-clamp: 2;
}

.template-prompt {
  margin-top: $ab-space-xs;
  color: $ab-text-tertiary;
  font-size: $ab-text-xs;
  line-height: 1.5;
  -webkit-line-clamp: 2;
}

.load-more {
  display: flex;
  justify-content: center;
  padding: $ab-space-xl 0;
}
</style>
