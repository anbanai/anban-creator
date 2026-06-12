<template>
  <view class="page viral-analysis">
    <!-- URL input section -->
    <view class="va-input-section">
      <text class="field-label">粘贴笔记链接或分享文本</text>
      <view class="va-input-row">
        <AbInput
          v-model="url"
          placeholder="粘贴笔记链接或分享文本，例如 http://xhslink.com/..."
          :disabled="submitting"
        />
        <AbButton
          type="primary"
          size="sm"
          :loading="submitting"
          :disabled="!url.trim()"
          @click="startAnalysis"
        >
          开始分析
        </AbButton>
      </view>
    </view>

    <!-- Analyzing indicator -->
    <view v-if="currentAnalysis && (currentAnalysis.status === 'pending' || currentAnalysis.status === 'analyzing')" class="va-analyzing">
      <AbLoading size="sm" :text="currentAnalysis.status === 'pending' ? '排队等待中...' : '正在分析中...'" />
    </view>

    <!-- Analysis history list -->
    <view class="va-section" v-if="historyList.length > 0">
      <text class="section-title">历史记录</text>
      <view
        v-for="item in historyList"
        :key="item.id"
        class="va-history-item"
        :class="{ 'va-history-item--active': selectedId === item.id }"
        @tap="selectAnalysis(item)"
      >
        <text class="va-history-item__title">{{ extractTitle(item) }}</text>
        <view class="va-history-item__meta">
          <AbBadge
            v-if="item.analysis_result"
            :variant="scoreVariant(item.analysis_result.overall_score.score)"
            size="sm"
          >
            {{ item.analysis_result.overall_score.score }}分
          </AbBadge>
          <AbBadge v-else :variant="statusBadgeVariant(item.status)" size="sm">
            {{ statusLabel(item.status) }}
          </AbBadge>
          <text class="va-history-item__time">{{ relativeTime(item.created_at) }}</text>
        </view>
      </view>
    </view>

    <!-- Analysis result display -->
    <view v-if="result" class="va-result">
      <view class="va-score-card">
        <text class="va-score-card__label">🔥 爆文指数</text>
        <view class="va-score-card__number">
          <text class="va-score-card__score">{{ result.overall_score.score }}</text>
          <text class="va-score-card__max">/100</text>
        </view>
        <text class="va-score-card__confidence">
          置信度 {{ confidenceLabel(result.overall_score.confidence) }} · {{ result.overall_score.evidence_count }} 条证据
        </text>
      </view>

      <view v-if="result.summary?.length" class="va-summary-card">
        <text class="section-title">核心结论</text>
        <text v-for="(item, i) in result.summary" :key="i" class="va-summary-card__item">
          {{ i + 1 }}. {{ item }}
        </text>
      </view>

      <view v-if="result.overall_score.why_not_higher" class="va-summary-card">
        <text class="section-title">为什么没有更高分</text>
        <text class="va-detail-card__text">{{ result.overall_score.why_not_higher }}</text>
      </view>

      <view v-if="result.evidence_table?.length" class="va-detail-card">
        <view class="va-detail-card__header" @tap="toggleDetail('evidence')">
          <text class="va-detail-card__title">证据表</text>
          <text class="va-detail-card__toggle">{{ expandedDetails.evidence ? '收起 ▲' : '展开 ▼' }}</text>
        </view>
        <view v-if="expandedDetails.evidence" class="va-detail-card__body">
          <view v-for="(item, i) in result.evidence_table" :key="i" class="va-evidence-item">
            <text class="va-detail-card__tag">{{ sourceLabel(item.source) }}</text>
            <text class="va-detail-card__text">{{ item.claim }}</text>
            <text class="va-evidence-item__evidence">{{ item.evidence }}</text>
          </view>
        </view>
      </view>

      <view class="va-detail-cards">
        <view
          v-for="dim in result.dimensions"
          :key="dim.name"
          class="va-detail-card"
        >
          <view class="va-detail-card__header" @tap="toggleDetail(dim.name)">
            <text class="va-detail-card__title">{{ dimensionLabel(dim.name) }}</text>
            <view class="va-detail-card__header-right">
              <AbBadge :variant="transferabilityVariant(dim.transferability)" size="sm">
                {{ transferabilityLabel(dim.transferability) }}
              </AbBadge>
              <text class="va-detail-card__toggle">{{ expandedDetails[dim.name] ? '收起 ▲' : '展开 ▼' }}</text>
            </view>
          </view>
          <view v-if="expandedDetails[dim.name]" class="va-detail-card__body">
            <text class="va-detail-card__text">
              <text class="va-detail-card__tag">观察:</text> {{ dim.observation }}
            </text>
            <text class="va-detail-card__text">
              <text class="va-detail-card__tag">机制:</text> {{ dim.mechanism }}
            </text>
            <text class="va-detail-card__text">
              <text class="va-detail-card__tag">迁移行动:</text> {{ dim.action }}
            </text>
          </view>
        </view>
      </view>

      <view class="va-detail-card">
        <view class="va-detail-card__header" @tap="toggleDetail('template')">
          <text class="va-detail-card__title">爆款模板</text>
          <text class="va-detail-card__toggle">{{ expandedDetails.template ? '收起 ▲' : '展开 ▼' }}</text>
        </view>
        <view v-if="expandedDetails.template" class="va-detail-card__body">
          <text class="va-detail-card__text"><text class="va-detail-card__tag">标题:</text> {{ result.viral_template.title_template }}</text>
          <text class="va-detail-card__text"><text class="va-detail-card__tag">封面:</text> {{ result.viral_template.cover_template }}</text>
          <text class="va-detail-card__text"><text class="va-detail-card__tag">正文:</text> {{ result.viral_template.body_template }}</text>
          <text class="va-detail-card__text"><text class="va-detail-card__tag">互动:</text> {{ result.viral_template.interaction_template }}</text>
          <text class="va-detail-card__text"><text class="va-detail-card__tag">人群洞察:</text> {{ result.viral_template.audience_insight }}</text>
          <text class="va-detail-card__text"><text class="va-detail-card__tag">爆款机制:</text> {{ result.viral_template.viral_mechanism }}</text>
        </view>
      </view>

      <view v-if="cloneSuggestionGroups.length" class="va-suggestions">
        <text class="section-title">复刻建议</text>
        <view
          v-for="group in cloneSuggestionGroups"
          :key="group.key"
          class="va-suggestion-item"
        >
          <text class="va-suggestion-item__number">{{ group.label }}</text>
          <text class="va-suggestion-item__text">{{ group.items.join('；') }}</text>
        </view>
      </view>

      <view v-if="result.risks?.length" class="va-suggestions">
        <text class="section-title">风险提醒</text>
        <view v-for="(risk, i) in result.risks" :key="i" class="va-suggestion-item">
          <text class="va-suggestion-item__number">{{ i + 1 }}</text>
          <text class="va-suggestion-item__text">{{ risk }}</text>
        </view>
      </view>
    </view>

    <!-- Failed state -->
    <view v-if="currentAnalysis && currentAnalysis.status === 'failed'" class="va-failed">
      <text class="va-failed__icon">❌</text>
      <text class="va-failed__text">分析失败</text>
      <text v-if="currentAnalysis.error_message" class="va-failed__error">{{ currentAnalysis.error_message }}</text>
    </view>

    <!-- Loading & empty states -->
    <AbLoading v-if="loading" text="加载历史记录..." />
    <AbEmpty
      v-if="!loading && historyList.length === 0 && !currentAnalysis"
      title="还没有分析记录"
      description="粘贴种草笔记链接，开始分析爆款"
    />
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import type { ViralAnalysis, AnalysisResult } from '@/types'
import { viralAnalysesApi } from '@/api/viral-analyses'
import { relativeTime } from '@/utils/format'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'

const url = ref('')
const submitting = ref(false)
const loading = ref(false)
const selectedId = ref<string | null>(null)
const currentAnalysis = ref<ViralAnalysis | null>(null)
const historyList = ref<ViralAnalysis[]>([])
const pollTimer = ref<ReturnType<typeof setInterval> | null>(null)

const expandedDetails = reactive<Record<string, boolean>>({
  evidence: false,
  template: false,
})

const result = computed<AnalysisResult | null>(() => currentAnalysis.value?.analysis_result ?? null)

const cloneSuggestionGroups = computed(() => {
  if (!result.value?.clone_suggestions) return []
  const groups = [
    { key: 'title', label: '标题', items: result.value.clone_suggestions.title },
    { key: 'body', label: '正文', items: result.value.clone_suggestions.body },
    { key: 'cover', label: '封面', items: result.value.clone_suggestions.cover },
    { key: 'tags', label: '标签', items: result.value.clone_suggestions.tags },
    { key: 'interaction', label: '互动', items: result.value.clone_suggestions.interaction },
  ]
  return groups.filter(group => group.items.length > 0)
})

function extractTitle(analysis: ViralAnalysis): string {
  if (analysis.analysis_result?.viral_template?.title_template) {
    return analysis.analysis_result.viral_template.title_template.slice(0, 30)
  }
  if (analysis.analysis_result?.template_meta?.name) {
    return analysis.analysis_result.template_meta.name
  }
  if (analysis.source_url) {
    return '种草笔记'
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

function statusBadgeVariant(status: string): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info' | 'neutral'> = {
    pending: 'warning',
    analyzing: 'info',
    completed: 'success',
    failed: 'danger',
  }
  return map[status] || 'neutral'
}

function scoreVariant(score: number): 'success' | 'warning' | 'danger' | 'info' {
  if (score >= 85) return 'success'
  if (score >= 70) return 'info'
  if (score >= 50) return 'warning'
  return 'danger'
}

function dimensionLabel(name: string): string {
  const map: Record<string, string> = {
    topic_angle: '选题角度',
    title: '标题',
    cover: '封面',
    body: '正文',
    interaction: '互动',
    tags: '标签',
    comment_signals: '评论信号',
  }
  return map[name] || name
}

function confidenceLabel(confidence: string): string {
  const map: Record<string, string> = {
    high: '高',
    medium: '中',
    low: '低',
  }
  return map[confidence] || confidence
}

function transferabilityLabel(value: string): string {
  const map: Record<string, string> = {
    high: '高迁移',
    medium: '中迁移',
    low: '低迁移',
  }
  return map[value] || value
}

function transferabilityVariant(value: string): 'success' | 'warning' | 'danger' | 'info' {
  if (value === 'high') return 'success'
  if (value === 'medium') return 'info'
  if (value === 'low') return 'warning'
  return 'info'
}

function sourceLabel(source: string): string {
  const map: Record<string, string> = {
    title: '标题',
    cover: '封面',
    body: '正文',
    tags: '标签',
    metrics: '数据',
    comments: '评论',
  }
  return map[source] || source
}

function toggleDetail(key: string) {
  expandedDetails[key] = !expandedDetails[key]
}

async function startAnalysis() {
  if (!url.value.trim()) return
  const match = url.value.match(/https?:\/\/[^\s]+/)
  const extracted = match ? match[0].replace(/[.,，。！!？?;；:：]+$/, '') : ''
  if (!extracted) {
    uni.showToast({ title: '未检测到有效链接，请粘贴种草笔记链接或分享文本', icon: 'none' })
    return
  }
  submitting.value = true
  try {
    const analysis = await viralAnalysesApi.create({
      source_type: 'note',
      source_url: extracted,
    })
    currentAnalysis.value = analysis
    selectedId.value = analysis.id
    // Prepend to history
    historyList.value.unshift(analysis)
    startPolling(analysis)
    url.value = ''
    uni.showToast({ title: '已提交分析', icon: 'success' })
  } catch (err: any) {
    const msg = err?.message || '提交失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

async function selectAnalysis(item: ViralAnalysis) {
  selectedId.value = item.id
  currentAnalysis.value = item

  if (item.status === 'pending' || item.status === 'analyzing') {
    startPolling(item)
  } else if (item.status === 'completed' && !item.analysis_result) {
    // Fetch full result if not loaded
    try {
      const full = await viralAnalysesApi.get(item.id)
      currentAnalysis.value = full
      // Update history item
      const idx = historyList.value.findIndex(h => h.id === item.id)
      if (idx >= 0) historyList.value[idx] = full
    } catch (err) {
      console.error('Failed to load analysis:', err)
    }
  }
}

function startPolling(analysis: ViralAnalysis) {
  stopPolling()
  if (analysis.status === 'completed' || analysis.status === 'failed') return

  pollTimer.value = setInterval(async () => {
    if (!currentAnalysis.value) {
      stopPolling()
      return
    }
    try {
      const updated = await viralAnalysesApi.get(currentAnalysis.value.id)
      currentAnalysis.value = updated

      // Update in history list
      const idx = historyList.value.findIndex(h => h.id === updated.id)
      if (idx >= 0) historyList.value[idx] = updated

      if (updated.status === 'completed' || updated.status === 'failed') {
        stopPolling()
      }
    } catch (err) {
      console.error('Poll error:', err)
    }
  }, 3000)
}

function stopPolling() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

async function loadHistory() {
  loading.value = true
  try {
    const res = await viralAnalysesApi.list({ limit: 20 })
    historyList.value = res.items || []

    // Auto-select the latest completed analysis
    if (historyList.value.length > 0) {
      const latest = historyList.value[0]
      if (latest.status === 'completed' || latest.status === 'failed') {
        selectAnalysis(latest)
      } else if (latest.status === 'pending' || latest.status === 'analyzing') {
        currentAnalysis.value = latest
        selectedId.value = latest.id
        startPolling(latest)
      }
    }
  } catch (err) {
    console.error('Failed to load history:', err)
  } finally {
    loading.value = false
  }
}

onMounted(loadHistory)
onUnmounted(stopPolling)
</script>

<style lang="scss" scoped>
.viral-analysis {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
}

.field-label {
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
  display: block;
  margin-bottom: $ab-space-xs;
}

.va-input-section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.va-input-row {
  display: flex;
  gap: $ab-space-sm;
  align-items: flex-start;

  :deep(.ab-input-wrapper) {
    flex: 1;
  }
}

.va-analyzing {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.va-section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.section-title {
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  display: block;
  margin-bottom: $ab-space-sm;
}

.va-history-item {
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;
  transition: background-color 0.2s ease;

  &:last-child {
    border-bottom: none;
  }

  &--active {
    background-color: $ab-primary-bg;
    border-radius: $ab-radius-sm;
    padding: $ab-space-sm $ab-space-sm;
    margin: 0 (-$ab-space-sm);
  }

  &__title {
    font-size: $ab-text-base;
    color: $ab-text;
    display: block;
    margin-bottom: 8rpx;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__meta {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-left: auto;
  }
}

// Score card
.va-score-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
  text-align: center;

  &__label {
    font-size: $ab-text-md;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-xs;
    display: block;
  }

  &__number {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: 4rpx;
  }

  &__score {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    line-height: 1;
  }

  &__max {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}

// Dimensions
.va-dimensions {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.va-dimension {
  margin-bottom: $ab-space-sm;

  &:last-child {
    margin-bottom: 0;
  }

  &__header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8rpx;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__score {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-semibold;
  }
}

// Detail cards
.va-detail-cards {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-md;
}

.va-detail-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__header-right {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__toggle {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__body {
    margin-top: $ab-space-sm;
    padding-top: $ab-space-sm;
    border-top: 2rpx solid $ab-divider;
  }

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.6;
    display: block;
    margin-bottom: 8rpx;

    &:last-child {
      margin-bottom: 0;
    }
  }

  &__tag {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__tags {
    display: flex;
    flex-wrap: wrap;
    gap: 8rpx;
    align-items: center;
    margin-top: 8rpx;
  }

  &__suggestions {
    margin-top: $ab-space-sm;
  }
}

.va-tag-chip {
  font-size: $ab-text-xs;
  color: $ab-text-secondary;
  background-color: $ab-background;
  padding: 4rpx 16rpx;
  border-radius: $ab-radius-full;
  line-height: 1.4;

  &--highlight {
    background-color: $ab-primary-bg;
    color: $ab-primary;
  }
}

// Suggestions
.va-suggestions {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  box-shadow: $ab-shadow-sm;
}

.va-suggestion-item {
  display: flex;
  gap: $ab-space-sm;
  align-items: flex-start;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__number {
    width: 36rpx;
    height: 36rpx;
    border-radius: 50%;
    background-color: $ab-primary-bg;
    color: $ab-primary;
    font-size: $ab-text-xs;
    font-weight: $ab-font-semibold;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.6;
    flex: 1;
  }
}

// Failed
.va-failed {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;

  &__icon {
    font-size: 64rpx;
    margin-bottom: $ab-space-sm;
  }

  &__text {
    font-size: $ab-text-md;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-xs;
  }

  &__error {
    font-size: $ab-text-sm;
    color: $ab-danger;
  }
}
</style>
