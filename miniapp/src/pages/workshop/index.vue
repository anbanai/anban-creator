<template>
  <view class="page workshop-index">
    <!-- Three main feature cards -->
    <view class="workshop-index__grid">
      <view class="feature-card" @tap="navigateTo('viral-analysis')">
        <view class="feature-card__icon">🔬</view>
        <view class="feature-card__body">
          <text class="feature-card__title">爆文拆解</text>
          <text class="feature-card__desc">分析种草笔记爆款笔记，获取创作灵感</text>
        </view>
        <text class="feature-card__arrow">›</text>
      </view>

      <view class="feature-card" @tap="navigateTo('poster')">
        <view class="feature-card__icon">🎨</view>
        <view class="feature-card__body">
          <text class="feature-card__title">海报制作</text>
          <text class="feature-card__desc">AI智能生成商业海报</text>
        </view>
        <text class="feature-card__arrow">›</text>
      </view>

      <view class="feature-card" @tap="navigateTo('clone')">
        <view class="feature-card__icon">📋</view>
        <view class="feature-card__body">
          <text class="feature-card__title">爆款复刻</text>
          <text class="feature-card__desc">复刻爆款内容，融入你的风格</text>
        </view>
        <text class="feature-card__arrow">›</text>
      </view>
    </view>

    <!-- Recent analyses -->
    <view class="workshop-index__section" v-if="recentAnalyses.length > 0">
      <view class="section-header">
        <text class="section-header__title">最近分析</text>
        <text class="section-header__more" @tap="navigateTo('viral-analysis')">更多 ›</text>
      </view>
      <view
        v-for="item in recentAnalyses"
        :key="item.id"
        class="history-item"
        @tap="navigateTo('viral-analysis')"
      >
        <text class="history-item__title">{{ extractTitle(item) }}</text>
        <AbBadge v-if="item.analysis_result" :variant="scoreVariant(item.analysis_result.overall_score.score)" size="sm">
          {{ item.analysis_result.overall_score.score }}分
        </AbBadge>
        <AbBadge v-else variant="neutral" size="sm">
          {{ statusLabel(item.status) }}
        </AbBadge>
        <text class="history-item__time">{{ relativeTime(item.created_at) }}</text>
      </view>
    </view>

    <!-- Recent posters -->
    <view class="workshop-index__section" v-if="recentPosters.length > 0">
      <view class="section-header">
        <text class="section-header__title">最近海报</text>
        <text class="section-header__more" @tap="navigateTo('poster')">更多 ›</text>
      </view>
      <scroll-view scroll-x class="poster-scroll" @tap="navigateTo('poster')">
        <view class="poster-scroll__list">
          <view v-for="poster in recentPosters" :key="poster.id" class="poster-thumb">
            <image
              v-if="poster.images && poster.images.length > 0"
              :src="poster.images[0].url"
              class="poster-thumb__img"
              mode="aspectFill"
            />
            <view v-else class="poster-thumb__placeholder">
              <text>{{ poster.input_content?.title || '海报' }}</text>
            </view>
            <text class="poster-thumb__label">{{ relativeTime(poster.created_at) }}</text>
          </view>
        </view>
      </scroll-view>
    </view>

    <!-- Loading state -->
    <AbLoading v-if="loading && !hasData" text="加载中..." />

    <!-- Empty state (only when not loading) -->
    <AbEmpty
      v-if="!loading && !hasData"
      title="还没有创作记录"
      description="试试分析一篇爆款笔记或生成一张海报"
    />
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { ViralAnalysis, PosterTask } from '@/types'
import { viralAnalysesApi } from '@/api/viral-analyses'
import { postersApi } from '@/api/posters'
import { relativeTime } from '@/utils/format'
import AbBadge from '@/components/common/AbBadge.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'

const loading = ref(false)
const recentAnalyses = ref<ViralAnalysis[]>([])
const recentPosters = ref<PosterTask[]>([])

const hasData = computed(() => recentAnalyses.value.length > 0 || recentPosters.value.length > 0)

function extractTitle(analysis: ViralAnalysis): string {
  if (analysis.analysis_result?.viral_template?.title_template) {
    return analysis.analysis_result.viral_template.title_template
  }
  if (analysis.analysis_result?.template_meta?.name) {
    return analysis.analysis_result.template_meta.name
  }
  if (analysis.source_url) {
    return analysis.source_url.replace(/https?:\/\/[^/]+\/.*/, '种草笔记')
  }
  return '分析记录'
}

function statusLabel(status: string): string {
  const map: Record<string, string> = {
    pending: '等待中',
    analyzing: '分析中',
    completed: '已完成',
    failed: '失败',
  }
  return map[status] || status
}

function scoreVariant(score: number): 'success' | 'warning' | 'danger' | 'info' {
  if (score >= 85) return 'success'
  if (score >= 70) return 'info'
  if (score >= 50) return 'warning'
  return 'danger'
}

function navigateTo(page: string) {
  uni.navigateTo({ url: `/pages/workshop/${page}` })
}

async function loadRecentData() {
  loading.value = true
  try {
    const [analysesRes, postersRes] = await Promise.allSettled([
      viralAnalysesApi.list({ limit: 3 }),
      postersApi.list({ limit: 5 }),
    ])

    if (analysesRes.status === 'fulfilled') {
      recentAnalyses.value = analysesRes.value.items || []
    }
    if (postersRes.status === 'fulfilled') {
      recentPosters.value = postersRes.value.items || []
    }
  } catch (err) {
    console.error('Failed to load recent data:', err)
  } finally {
    loading.value = false
  }
}

onMounted(loadRecentData)
</script>

<style lang="scss" scoped>
.workshop-index {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;

  &__grid {
    display: flex;
    flex-direction: column;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-lg;
  }

  &__section {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    margin-bottom: $ab-space-sm;
    box-shadow: $ab-shadow-sm;
  }
}

.feature-card {
  display: flex;
  align-items: center;
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg $ab-space-md;
  box-shadow: $ab-shadow-sm;
  transition: transform 0.15s ease;

  &:active {
    transform: scale(0.98);
  }

  &__icon {
    font-size: 72rpx;
    line-height: 1;
    flex-shrink: 0;
    margin-right: $ab-space-md;
  }

  &__body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6rpx;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__desc {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.4;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__arrow {
    font-size: $ab-text-xl;
    color: $ab-text-tertiary;
    flex-shrink: 0;
    margin-left: $ab-space-sm;
  }
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: $ab-space-sm;

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__more {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }
}

.history-item {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__title {
    flex: 1;
    font-size: $ab-text-base;
    color: $ab-text;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    flex-shrink: 0;
  }
}

.poster-scroll {
  white-space: nowrap;
  margin: 0 (-$ab-space-sm);

  &__list {
    display: flex;
    gap: $ab-space-sm;
    padding: 0 $ab-space-sm;
  }
}

.poster-thumb {
  display: inline-flex;
  flex-direction: column;
  width: 200rpx;
  flex-shrink: 0;
  gap: 8rpx;

  &__img {
    width: 200rpx;
    height: 260rpx;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
  }

  &__placeholder {
    width: 200rpx;
    height: 260rpx;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: $ab-space-sm;
    box-sizing: border-box;

    text {
      font-size: $ab-text-xs;
      color: $ab-text-tertiary;
      text-align: center;
      overflow: hidden;
      text-overflow: ellipsis;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
    }
  }

  &__label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    text-align: center;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}
</style>
