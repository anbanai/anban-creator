<template>
  <view class="page task-detail">
    <AbLoading v-if="initialLoading" text="加载中" />

    <view v-else-if="task">
      <!-- Goal mode banner -->
      <view v-if="task.goal_mode" class="task-detail__goal-banner">
        <text class="task-detail__goal-icon">目</text>
        <view class="task-detail__goal-body">
          <text class="task-detail__goal-title">强目标模式</text>
          <text class="task-detail__goal-text">
            目标条件：{{ task.goal || '(未设置)' }}
          </text>
          <text class="task-detail__goal-hint">
            AI 会自动检查产出是否符合目标条件，未达成会继续修订直到符合（或达到最大尝试次数）。
          </text>
        </view>
      </view>

      <!-- Header card -->
      <view class="task-detail__header">
        <view class="task-detail__badges">
          <PlatformAvatar :platform="task.type" :size="36" />
          <AbBadge variant="neutral" size="sm">{{ typeLabel }}</AbBadge>
          <AbBadge :variant="statusBadgeVariant">{{ statusLabel }}</AbBadge>
        </view>
        <text class="task-detail__title">{{ task.title || '未命名任务' }}</text>
        <view v-if="project" class="task-detail__project" @tap="goToProjects">
          <image
            v-if="project.avatar_url"
            :src="project.avatar_url"
            class="task-detail__project-avatar"
            mode="aspectFill"
          />
          <text v-else class="task-detail__project-avatar task-detail__project-avatar--fallback">
            {{ (project.name || '?').charAt(0) }}
          </text>
          <text class="task-detail__project-name">{{ project.name }}</text>
        </view>
        <view class="task-detail__times">
          <text class="task-detail__time">创建: {{ formatFullDateTimeCN(task.created_at) }}</text>
          <text v-if="task.started_at" class="task-detail__time">
            开始: {{ formatFullDateTimeCN(task.started_at) }}
          </text>
          <text v-if="task.completed_at" class="task-detail__time">
            完成: {{ formatFullDateTimeCN(task.completed_at) }}
          </text>
          <text class="task-detail__time">
            来源: {{ task.plan_id ? '计划任务' : '手动创建' }}
          </text>
        </view>
      </view>

      <view v-if="task.agent_profile_snapshot" class="task-detail__section agent-profile-details">
        <text class="section-title">Agent 执行配置</text>
        <view class="agent-profile-details__grid">
          <view class="agent-profile-details__item">
            <text class="agent-profile-details__label">档位</text>
            <text class="agent-profile-details__value">{{ task.agent_profile_snapshot.display_name }}</text>
          </view>
          <view
            v-for="row in agentProfileRows"
            :key="row.label"
            class="agent-profile-details__item"
          >
            <text class="agent-profile-details__label">{{ row.label }}</text>
            <text class="agent-profile-details__value">{{ row.value }}</text>
          </view>
        </view>
      </view>

      <view v-if="billingTotal !== undefined" class="task-detail__section billing-details">
        <text class="section-title">积分明细</text>
        <view class="billing-details__total">
          <text class="billing-details__total-label">累计扣费</text>
          <text class="billing-details__total-value">{{ billingTotal.toLocaleString() }} 积分</text>
        </view>
        <view
          v-for="(detail, index) in billingDetails"
          :key="detail.id || `${detail.sku_id || detail.charge_kind}-${detail.created_at || index}-${index}`"
          class="billing-detail"
        >
          <view class="billing-detail__body">
            <text class="billing-detail__label">{{ taskBillingChargeLabel(detail) }}</text>
            <text class="billing-detail__identity">{{ taskBillingIdentity(detail) }}</text>
            <text v-if="taskBillingPricingEvidence(detail)" class="billing-detail__pricing">
              {{ taskBillingPricingEvidence(detail) }}
            </text>
          </view>
          <text
            class="billing-detail__amount"
            :class="{ 'billing-detail__amount--reversal': detail.charge_kind === 'reversal' || detail.credits < 0 }"
          >
            {{ taskBillingAmountLabel(detail) }}
          </text>
        </view>
      </view>

      <!-- Next actions -->
      <view v-if="nextActions.length > 0" class="task-detail__section next-actions">
        <view class="section-header">
          <text class="section-title">下一步操作</text>
          <text class="next-actions__hint">{{ nextActionHint }}</text>
        </view>
        <view class="next-actions__grid">
          <view
            v-for="action in nextActions"
            :key="action.key"
            class="next-action"
            :class="`next-action--${action.tone}`"
            @tap="runNextAction(action.key)"
          >
            <text class="next-action__label">{{ action.label }}</text>
            <text class="next-action__desc">{{ action.desc }}</text>
          </view>
        </view>
      </view>

      <!-- Progress section (running/pending) -->
      <view
        v-if="task.status === 'running' || task.status === 'pending'"
        class="task-detail__section"
      >
        <view class="section-header">
          <text class="section-title">执行进度</text>
          <text v-if="progressStage" class="task-detail__stage-chip">
            阶段：{{ stageLabel(progressStage) }}
          </text>
        </view>
        <view class="task-detail__progress-row">
          <text class="task-detail__progress-title">{{ progressTitle }}</text>
          <text class="task-detail__progress-percent">{{ progressValue }}%</text>
        </view>
        <AbProgress
          :percent="progressValue"
          :show-text="false"
          height="16rpx"
          :color="task.status === 'running' ? '#4F46E5' : '#D97706'"
        />
        <text v-if="progressDescription" class="task-detail__progress-desc">
          {{ progressDescription }}
        </text>
        <text v-if="polling" class="task-detail__polling-indicator">
          <text class="polling-dot" /> 实时更新中
        </text>

        <!-- Workflow stages grid -->
        <view v-if="workflowStages.length > 0" class="workflow-grid">
          <view
            v-for="stage in workflowStages"
            :key="stage.key"
            class="workflow-stage"
            :class="`workflow-stage--${stage.status}`"
          >
            <text class="workflow-stage__icon">{{ stageIcon(stage.status) }}</text>
            <text class="workflow-stage__label">{{ stage.label }}</text>
          </view>
        </view>

        <!-- Workflow warnings -->
        <view v-if="workflowWarnings.length > 0" class="task-detail__warnings">
          <view
            v-for="(w, i) in workflowWarnings"
            :key="i"
            class="task-detail__warning"
          >
            <text class="task-detail__warning-icon">!</text>
            <text class="task-detail__warning-text">{{ w.message }}</text>
          </view>
        </view>
      </view>

      <!-- Error message -->
      <view v-if="task.status === 'failed'" class="task-detail__section task-detail__section--error">
        <view class="section-header">
          <text class="section-title">错误信息</text>
          <text
            v-if="errorMessage"
            class="section-action"
            @tap="copyText(errorMessage, '错误信息已复制')"
          >
            复制
          </text>
        </view>
        <text class="task-detail__error-text">{{ errorMessage }}</text>
      </view>

      <!-- Generated images gallery -->
      <view
        v-if="imageUrls.length > 0"
        class="task-detail__section"
      >
        <view class="section-header">
          <text class="section-title">生成结果</text>
          <text
            v-if="task.type === 'article' && task.status === 'completed'"
            class="section-action"
            @tap="onPreviewArticle"
          >
            预览 HTML
          </text>
        </view>
        <view class="image-grid">
          <image
            v-for="(url, i) in imageUrls"
            :key="i"
            :src="url"
            class="image-grid__item"
            mode="aspectFill"
            @tap="onPreviewImage(i)"
          />
        </view>
      </view>

      <!-- Generated files -->
      <view v-if="taskFiles.length > 0" class="task-detail__section">
        <view class="section-header">
          <text class="section-title">生成文件 ({{ taskFiles.length }})</text>
          <text class="section-action" @tap="downloadZip">下载 ZIP</text>
        </view>
        <view
          v-for="file in taskFiles"
          :key="file.id"
          class="file-row"
        >
          <view class="file-row__body" @tap="previewFile(file)">
            <text class="file-row__name">{{ file.file_name }}</text>
            <text class="file-row__meta">{{ file.mime_type || '文件' }} · {{ formatFileSize(file.file_size) }}</text>
          </view>
          <AbButton type="ghost" size="sm" @click="downloadFile(file)">下载</AbButton>
        </view>
      </view>

      <!-- Content preview -->
      <view
        v-if="task.result?.output"
        class="task-detail__section"
      >
        <text class="section-title">内容预览</text>
        <view class="content-preview" :class="{ 'content-preview--collapsed': !contentExpanded }">
          <rich-text :nodes="sanitizeHtml(task.result.output)" />
        </view>
        <text
          v-if="task.result.output.length > 300"
          class="content-expand-btn"
          @tap="contentExpanded = !contentExpanded"
        >
          {{ contentExpanded ? '收起' : '展开全部' }}
        </text>
      </view>

      <!-- Execution log -->
      <view v-if="showLogs" class="task-detail__section">
        <view class="section-header">
          <text class="section-title">执行日志</text>
          <view class="section-actions">
            <text
              class="section-action"
              :class="{ 'section-action--muted': !followLogs }"
              @tap="followLogs = !followLogs"
            >
              {{ followLogs ? '跟随输出' : '已暂停' }}
            </text>
            <text
              class="section-action"
              :class="{ 'section-action--disabled': logs.length === 0 }"
              @tap="copyLogs"
            >
              复制
            </text>
          </view>
        </view>
        <scroll-view
          class="log-scroll"
          scroll-y
          :scroll-top="logScrollTop"
          :scroll-with-animation="true"
        >
          <view v-if="logs.length === 0" class="log-empty">
            <text class="log-empty__text">等待输出中...</text>
          </view>
          <view
            v-for="(log, i) in logs"
            :key="i"
            class="log-entry"
            :class="{ 'log-entry--latest': i === logs.length - 1 }"
          >
            <text class="log-entry__text">{{ log }}</text>
          </view>
        </scroll-view>
      </view>

      <!-- Quality review section -->
      <view v-if="review" class="task-detail__section">
        <text class="section-title">质量评估</text>
        <view class="review-card">
          <view class="review-card__score">
            <text class="review-card__score-number">{{ review.overall_score }}</text>
            <text class="review-card__score-max">/100</text>
          </view>
          <view class="review-card__readiness">
            <text class="review-card__readiness-label">发布就绪</text>
            <AbBadge :variant="review.readiness === 'ready' ? 'success' : 'warning'">
              {{ review.readiness === 'ready' ? '是' : '否' }}
            </AbBadge>
          </view>

          <!-- Strengths -->
          <view v-if="review.strengths?.length" class="review-card__section">
            <text class="review-card__section-title">优势</text>
            <text
              v-for="(s, i) in review.strengths"
              :key="i"
              class="review-card__item review-card__item--positive"
            >
              {{ s }}
            </text>
          </view>

          <!-- Risks -->
          <view v-if="review.risks?.length" class="review-card__section">
            <text class="review-card__section-title">风险</text>
            <text
              v-for="(r, i) in review.risks"
              :key="i"
              class="review-card__item review-card__item--negative"
            >
              {{ r }}
            </text>
          </view>
        </view>
      </view>

      <!-- Seednote analytics -->
      <view v-if="seednoteAnalytics" class="task-detail__section">
        <view class="section-header">
          <text class="section-title">种草笔记数据</text>
          <AbBadge v-if="seednoteAnalytics.tracking" variant="info" size="sm">
            {{ seednoteTrackingLabel }}
          </AbBadge>
        </view>
        <view v-if="seednoteAnalytics.tracking" class="analytics-card">
          <text class="analytics-card__title">{{ seednoteAnalytics.tracking.note_title || '等待绑定公开笔记' }}</text>
          <text v-if="seednoteAnalytics.tracking.note_url" class="analytics-card__link" @tap="copyText(seednoteAnalytics.tracking.note_url, '链接已复制')">
            {{ seednoteAnalytics.tracking.note_url }}
          </text>
          <view v-if="seednoteAnalytics.tracking.note_cover_url" class="analytics-card__cover-wrap">
            <image :src="seednoteAnalytics.tracking.note_cover_url" class="analytics-card__cover" mode="aspectFill" />
          </view>
          <view class="metrics-grid">
            <view v-for="metric in analyticsMetrics" :key="metric.label" class="metric-tile">
              <text class="metric-tile__label">{{ metric.label }}</text>
              <text class="metric-tile__value">{{ metric.value }}</text>
              <text v-if="metric.delta" class="metric-tile__delta">+{{ metric.delta }}</text>
            </view>
          </view>
          <view class="analytics-card__meta">
            <text>最近采集: {{ formatFullDateTimeCN(seednoteAnalytics.tracking.last_run_at || '') || '--' }}</text>
            <text>下次采集: {{ formatFullDateTimeCN(seednoteAnalytics.tracking.next_run_at || '') || '--' }}</text>
            <text>采集次数: {{ seednoteAnalytics.tracking.run_count }}</text>
          </view>
          <text v-if="seednoteAnalytics.tracking.last_error" class="analytics-card__error">
            {{ seednoteAnalytics.tracking.last_error }}
          </text>
        </view>
        <text v-else class="analytics-card__empty">追踪任务正在准备中</text>
      </view>

      <!-- Bottom actions -->
      <view class="task-detail__bottom">
        <!-- Running: cancel -->
        <AbButton
          v-if="task.status === 'running' || task.status === 'pending'"
          type="ghost"
          size="lg"
          block
          :loading="actionLoading"
          @click="onCancel"
        >
          取消任务
        </AbButton>

        <!-- Failed / Cancelled: retry + delete -->
        <template v-if="task.status === 'failed' || task.status === 'cancelled'">
          <view class="task-detail__bottom-row">
            <AbButton
              type="ghost"
              size="md"
              :loading="actionLoading"
              @click="onDelete"
              style="flex: 1;"
            >
              删除
            </AbButton>
            <AbButton
              type="primary"
              size="md"
              :loading="actionLoading"
              @click="onRetry"
              style="flex: 1;"
            >
              重新执行
            </AbButton>
          </view>
        </template>

        <!-- Completed: published + share + delete + preview -->
        <template v-if="task.status === 'completed'">
          <view class="task-detail__bottom-row">
            <AbButton
              type="ghost"
              size="md"
              :loading="actionLoading"
              @click="onDelete"
              style="flex: 1;"
            >
              删除
            </AbButton>
            <AbButton
              type="ghost"
              size="md"
              :loading="actionLoading"
              @click="onTogglePublished"
              style="flex: 1;"
            >
              {{ task.published ? '取消发布' : '标记已发布' }}
            </AbButton>
          </view>
          <view class="task-detail__bottom-row" style="margin-top: 12rpx;">
            <AbButton
              v-if="task.type === 'article'"
              type="ghost"
              size="md"
              style="flex: 1;"
              @click="onPreviewArticle"
            >
              预览
            </AbButton>
            <AbButton
              type="primary"
              size="md"
              :style="{ flex: 1 }"
              @click="onShare"
            >
              分享结果
            </AbButton>
          </view>
        </template>
      </view>
    </view>

    <!-- Task not found -->
    <AbEmpty v-else title="任务不存在" />

    <!-- Article HTML preview overlay (in-page, no separate route) -->
    <view v-if="previewVisible" class="preview-overlay" @tap="closePreview">
      <view class="preview-overlay__panel" @tap.stop>
        <view class="preview-overlay__header">
          <text class="preview-overlay__title">文章预览</text>
          <view class="preview-overlay__actions">
            <text class="preview-overlay__action" @tap="copyPreviewHtml">复制 HTML</text>
            <text class="preview-overlay__action" @tap="closePreview">关闭</text>
          </view>
        </view>
        <scroll-view scroll-y class="preview-overlay__body">
          <rich-text v-if="previewHtml" :nodes="sanitizeHtml(previewHtml)" />
          <view v-else-if="previewLoading" class="preview-overlay__loading">
            <text>加载中...</text>
          </view>
          <view v-else class="preview-overlay__empty">
            <text>暂无预览内容</text>
          </view>
        </scroll-view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { onLoad, onShareAppMessage, onUnload } from '@dcloudio/uni-app'
import type { Project, SeednoteAnalytics, Task, TaskBillingChargeDetail, TaskFile, TaskStatus, WorkflowStage, WorkflowReview, WorkflowWarning } from '@/types'
import { tasksApi } from '@/api/tasks'
import { projectsApi } from '@/api/projects'
import { taskStatusLabel, contentTypeLabel, progressStageLabel } from '@/utils/labels'
import { formatDateTimeCN, formatFullDateTimeCN, sanitizeHtml } from '@/utils/format'
import {
  taskBillingAmountLabel,
  taskBillingChargeLabel,
  taskBillingDetails,
  taskBillingIdentity,
  taskBillingPricingEvidence,
  taskBillingTotal,
} from '@/utils/task-billing'
import { usePolling } from '@/composables/usePolling'
import AbBadge from '@/components/common/AbBadge.vue'
import AbProgress from '@/components/common/AbProgress.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

const taskId = ref('')
const initialLoading = ref(true)
const actionLoading = ref(false)
const contentExpanded = ref(false)
const followLogs = ref(true)
const logScrollTop = ref(0)
const taskFiles = ref<TaskFile[]>([])
const seednoteAnalytics = ref<SeednoteAnalytics | null>(null)
const project = ref<Project | null>(null)

// In-page article HTML preview overlay
const previewVisible = ref(false)
const previewHtml = ref('')
const previewLoading = ref(false)

const {
  task,
  logs,
  progress,
  status,
  progressMessage,
  polling,
  startPolling,
  stopPolling,
} = usePolling()

const typeLabel = computed(() => {
  const t = task.value?.type
  return t ? (contentTypeLabel[t] || t) : ''
})

const statusLabel = computed(() => {
  const taskStatus = task.value?.status
  return taskStatus ? taskStatusLabel[taskStatus as TaskStatus] || '' : ''
})

const statusBadgeVariant = computed(() => {
  switch (task.value?.status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'info'
    case 'pending': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
})

const billingTotal = computed(() => task.value ? taskBillingTotal(task.value) : undefined)
const billingDetails = computed<TaskBillingChargeDetail[]>(() => task.value ? taskBillingDetails(task.value) : [])

function formatTokenCount(value: string | undefined): string | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined
  return BigInt(value).toLocaleString()
}

const agentProfileRows = computed(() => {
  const envs = task.value?.agent_profile_snapshot?.envs
  if (!envs) return []
  const rows: Array<{ label: string; value: string }> = []
  if (envs.CLAUDE_CODE_EFFORT_LEVEL) rows.push({ label: '推理强度', value: envs.CLAUDE_CODE_EFFORT_LEVEL })
  const context = formatTokenCount(envs.CLAUDE_CODE_MAX_CONTEXT_TOKENS)
  if (context) rows.push({ label: '最大上下文', value: context })
  if (envs.CLAUDE_CODE_DISABLE_THINKING === 'true' || envs.CLAUDE_CODE_DISABLE_THINKING === 'false') {
    rows.push({ label: '思考模式', value: envs.CLAUDE_CODE_DISABLE_THINKING === 'true' ? '关闭' : '开启' })
  }
  return rows
})

const workflowStages = computed<WorkflowStage[]>(() => {
  const ws = task.value?.workflow_status
  if (!ws || typeof ws === 'string') return []
  return (ws as any).stages || []
})

const workflowWarnings = computed<WorkflowWarning[]>(() => {
  const ws = task.value?.workflow_status
  if (!ws || typeof ws === 'string') return []
  return (ws as any).warnings || []
})

const review = computed<WorkflowReview | null>(() => {
  const ws = task.value?.workflow_status
  if (!ws || typeof ws === 'string') return null
  return (ws as any).review || null
})

const errorMessage = computed(() => {
  const t = task.value
  if (!t) return ''
  return t.error_message || t.error || '任务执行失败'
})

const imageUrls = computed(() => {
  const files = taskFiles.value.length > 0 ? taskFiles.value : task.value?.result?.files
  if (!files) return []
  return files
    .filter((f) => f.mime_type?.startsWith('image/'))
    .map((f) => f.url)
})

// Monotonic progress: max(latest_progress.percent, task.progress) — mirrors studio's
// progressValue logic (UpdateProgressColumn is monotonic server-side).
const progressValue = computed(() => {
  const t = task.value
  if (!t) return 0
  const live = t.latest_progress?.percent ?? 0
  const persisted = t.progress ?? 0
  return Math.max(0, Math.min(100, Math.max(live, persisted)))
})

const progressStage = computed(() => {
  return task.value?.latest_progress?.stage ?? null
})

const progressTitle = computed(() => {
  const t = task.value
  if (!t) return ''
  const fallback = t.status === 'pending' ? '任务等待执行中...' : '任务执行中...'
  return t.latest_progress?.title ?? fallback
})

const progressDescription = computed(() => {
  return task.value?.latest_progress?.description ?? null
})

const showLogs = computed(() => {
  return logs.value.length > 0 || task.value?.status === 'running'
})

const seednoteTrackingLabel = computed(() => {
  const status = seednoteAnalytics.value?.tracking?.status
  const map: Record<string, string> = {
    waiting_discovery: '等待识别',
    tracking: '追踪中',
    stopped: '已停止',
    failed: '采集失败',
  }
  return status ? map[status] || status : ''
})

const analyticsMetrics = computed(() => {
  const latest = seednoteAnalytics.value?.latest
  const deltas = seednoteAnalytics.value?.deltas
  if (!latest) return []
  return [
    { label: '点赞', value: formatNumber(latest.like_count), delta: deltas?.like_count ? formatNumber(deltas.like_count) : '' },
    { label: '收藏', value: formatNumber(latest.collect_count), delta: deltas?.collect_count ? formatNumber(deltas.collect_count) : '' },
    { label: '评论', value: formatNumber(latest.comment_count), delta: deltas?.comment_count ? formatNumber(deltas.comment_count) : '' },
    { label: '分享', value: formatNumber(latest.share_count), delta: deltas?.share_count ? formatNumber(deltas.share_count) : '' },
    { label: '曝光', value: latest.view_count == null ? '暂无' : formatNumber(latest.view_count), delta: '' },
  ]
})

interface NextAction {
  key: string
  label: string
  desc: string
  tone: 'primary' | 'neutral' | 'warning' | 'danger'
}

const availableFiles = computed(() => {
  return taskFiles.value.length > 0 ? taskFiles.value : task.value?.result?.files || []
})

const resultText = computed(() => {
  const output = task.value?.result?.output?.trim()
  if (output) return output
  return task.value?.prompt?.trim() || ''
})

const nextActionHint = computed(() => {
  switch (task.value?.status) {
    case 'completed': return '交付已就绪'
    case 'failed': return '先恢复任务'
    case 'cancelled': return '可重新执行'
    case 'running': return '实时执行中'
    case 'pending': return '等待排队'
    default: return ''
  }
})

const nextActions = computed<NextAction[]>(() => {
  const t = task.value
  if (!t) return []
  if (t.status === 'running' || t.status === 'pending') {
    return [
      { key: 'copy-logs', label: '复制日志', desc: logs.value.length > 0 ? '带走当前执行输出' : '暂无日志可复制', tone: 'neutral' },
      { key: 'cancel', label: '取消任务', desc: '固定任务费及已完成增值操作不退款', tone: 'danger' },
    ]
  }
  if (t.status === 'failed' || t.status === 'cancelled') {
    return [
      { key: 'retry', label: '重新执行', desc: '保留原任务全部设置', tone: 'primary' },
      { key: 'copy-error', label: '复制错误', desc: '用于反馈或排查', tone: 'neutral' },
      { key: 'follow-up', label: '基于要求再创作', desc: '回到创建页调整提示词', tone: 'warning' },
    ]
  }
  if (t.status === 'completed') {
    const actions: NextAction[] = []
    if (availableFiles.value.length > 0) {
      actions.push({ key: 'download-zip', label: '下载全部', desc: `${availableFiles.value.length} 个文件打包`, tone: 'primary' })
    }
    if (t.type === 'article') {
      actions.push({ key: 'preview', label: '预览 HTML', desc: '检查公众号排版', tone: 'neutral' })
    }
    if (resultText.value) {
      actions.push({ key: 'copy-result', label: '复制内容', desc: '带走正文或任务要求', tone: 'neutral' })
    }
    actions.push({ key: 'published', label: t.published ? '取消发布标记' : '标记已发布', desc: '同步内容状态', tone: 'warning' })
    actions.push({ key: 'share', label: '分享结果', desc: '通过微信菜单转发', tone: 'neutral' })
    actions.push({ key: 'follow-up', label: '基于结果再创作', desc: '复用产出生成新任务', tone: 'primary' })
    return actions
  }
  return []
})

function stageIcon(stageStatus: string): string {
  switch (stageStatus) {
    case 'completed': return 'OK'
    case 'running': return 'RUN'
    case 'failed': return 'ERR'
    case 'warning': return '!'
    default: return 'WAIT'
  }
}

function stageLabel(stage: string): string {
  return progressStageLabel[stage] ?? stage
}

function onPreviewImage(index: number) {
  if (imageUrls.value.length > 0) {
    uni.previewImage({
      current: imageUrls.value[index],
      urls: imageUrls.value,
    })
  }
}

function formatNumber(value?: number | null): string {
  return value == null ? '0' : value.toLocaleString()
}

function formatFileSize(bytes?: number): string {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function copyText(text: string, toast = '已复制') {
  uni.setClipboardData({
    data: text,
    success() {
      uni.showToast({ title: toast, icon: 'none' })
    },
  })
}

function copyLogs() {
  if (logs.value.length === 0) return
  copyText(logs.value.join('\n'), '已复制执行日志')
}

function copyResultText() {
  if (!resultText.value) {
    uni.showToast({ title: '暂无可复制内容', icon: 'none' })
    return
  }
  copyText(resultText.value, '结果内容已复制')
}

function createFollowUpTask() {
  const t = task.value
  if (!t) return
  const source = resultText.value || t.prompt || ''
  const trimmed = source.length > 700 ? `${source.slice(0, 700)}...` : source
  const prompt = t.status === 'completed'
    ? `基于以下产出继续创作一个升级版本：\n${trimmed}`
    : `基于原任务要求重新创作，并修正失败原因：\n${trimmed}\n\n失败信息：${errorMessage.value}`
  const params = [
    `type=${encodeURIComponent(t.type)}`,
    `project_id=${encodeURIComponent(t.project_id)}`,
    `prompt=${encodeURIComponent(prompt)}`,
    `execution_profile=${encodeURIComponent(t.execution_profile)}`,
  ]
  uni.navigateTo({ url: `/pages/tasks/create?${params.join('&')}` })
}

function runNextAction(key: string) {
  switch (key) {
    case 'download-zip':
      downloadZip()
      break
    case 'preview':
      void onPreviewArticle()
      break
    case 'copy-result':
      copyResultText()
      break
    case 'copy-logs':
      copyLogs()
      break
    case 'copy-error':
      copyText(errorMessage.value, '错误信息已复制')
      break
    case 'published':
      void onTogglePublished()
      break
    case 'share':
      onShare()
      break
    case 'retry':
      void onRetry()
      break
    case 'cancel':
      void onCancel()
      break
    case 'follow-up':
      createFollowUpTask()
      break
  }
}

function downloadByUrl(url: string, fileName: string, openAfterDownload = false) {
  uni.showLoading({ title: '下载中' })
  uni.downloadFile({
    url,
    header: tasksApi.downloadHeaders(),
    success(res) {
      if (res.statusCode !== 200) {
        uni.showToast({ title: '下载失败', icon: 'none' })
        return
      }
      if (openAfterDownload) {
        uni.openDocument({
          filePath: res.tempFilePath,
          showMenu: true,
          fail() {
            uni.showToast({ title: '无法预览，请用右上角菜单转发或保存', icon: 'none' })
          },
        })
        return
      }
      uni.saveFile({
        tempFilePath: res.tempFilePath,
        success() {
          uni.showToast({ title: '已保存', icon: 'success' })
        },
        fail() {
          uni.showToast({ title: `${fileName} 已下载`, icon: 'success' })
        },
      })
    },
    fail() {
      uni.showToast({ title: '下载失败', icon: 'none' })
    },
    complete() {
      uni.hideLoading()
    },
  })
}

function previewFile(file: TaskFile) {
  if (file.mime_type?.startsWith('image/')) {
    const imageFileUrls = taskFiles.value
      .filter((item) => item.mime_type?.startsWith('image/'))
      .map((item) => item.url)
    uni.previewImage({ current: file.url, urls: imageFileUrls })
    return
  }
  downloadByUrl(tasksApi.fileDownloadUrl(taskId.value, file.id), file.file_name, true)
}

function downloadFile(file: TaskFile) {
  downloadByUrl(tasksApi.fileDownloadUrl(taskId.value, file.id), file.file_name)
}

function downloadZip() {
  downloadByUrl(tasksApi.zipDownloadUrl(taskId.value), `task_${taskId.value}_files.zip`)
}

async function onPreviewArticle() {
  if (!taskId.value) return
  // Open the overlay first (better perceived latency), then fetch HTML.
  previewVisible.value = true
  previewLoading.value = true
  previewHtml.value = ''
  try {
    const html = await tasksApi.fetchPreviewHTML(taskId.value)
    previewHtml.value = html || ''
  } catch (err) {
    previewHtml.value = ''
    const msg = (err as Error)?.message || '预览加载失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    previewLoading.value = false
  }
}

function closePreview() {
  previewVisible.value = false
}

function copyPreviewHtml() {
  if (!previewHtml.value) return
  copyText(previewHtml.value, '预览 HTML 已复制')
}

function goToProjects() {
  uni.switchTab({ url: '/pages/projects/index', fail: () => {
    uni.navigateTo({ url: '/pages/projects/index' })
  } })
}

async function loadProject() {
  const pid = task.value?.project_id
  if (!pid) return
  try {
    const detail = await projectsApi.get(pid)
    project.value = detail.project
  } catch (err) {
    console.error('Failed to load project:', err)
    project.value = null
  }
}

async function loadCompletedDetails() {
  if (!taskId.value || task.value?.status !== 'completed') return
  try {
    taskFiles.value = await tasksApi.getFiles(taskId.value)
  } catch (err) {
    console.error('Failed to load task files:', err)
  }
  if (task.value?.type === 'seednote') {
    try {
      seednoteAnalytics.value = await tasksApi.getSeednoteAnalytics(taskId.value)
    } catch (err) {
      console.error('Failed to load seednote analytics:', err)
      seednoteAnalytics.value = null
    }
  }
}

// Auto-scroll logs to bottom when new entries arrive AND follow is on.
watch(
  () => logs.value.length,
  () => {
    if (!followLogs.value) return
    nextTick(() => {
      // Toggle to force scroll-view to re-evaluate scroll-top (uni quirk).
      logScrollTop.value = logScrollTop.value === 1 ? 99999 : 1
      nextTick(() => {
        logScrollTop.value = 99999
      })
    })
  },
)

watch(
  () => task.value?.status,
  (newStatus) => {
    void loadCompletedDetails()
    if (newStatus && ['completed', 'failed', 'cancelled'].includes(newStatus)) {
      void loadProject()
    }
  },
)

watch(
  () => task.value?.project_id,
  () => {
    void loadProject()
  },
  { immediate: false },
)

async function onCancel() {
  const t = task.value
  if (!t) return
  const refundHint = '用户取消不退还固定任务费；已经成功交付的图片、视频等增值操作也不退款。'

  uni.showModal({
    title: '确定取消此任务？',
    content: `${refundHint}此操作不可撤销。`,
    confirmText: '确定取消',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (res.confirm) {
        actionLoading.value = true
        try {
          await tasksApi.cancel(taskId.value)
          uni.showToast({ title: '任务已取消', icon: 'success' })
          // Polling will pick up the new status
        } catch {
          uni.showToast({ title: '取消失败', icon: 'none' })
        } finally {
          actionLoading.value = false
        }
      }
    },
  })
}

async function onDelete() {
  uni.showModal({
    title: '确定删除此任务？',
    content: '删除后任务及所有关联文件将被永久移除，此操作不可撤销。',
    confirmText: '确定删除',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (res.confirm) {
        actionLoading.value = true
        try {
          await tasksApi.delete(taskId.value)
          uni.showToast({ title: '任务已删除', icon: 'success' })
          stopPolling()
          setTimeout(() => {
            uni.navigateBack({ fail: () => {
              uni.switchTab({ url: '/pages/tasks/index' })
            } })
          }, 400)
        } catch {
          uni.showToast({ title: '删除失败', icon: 'none' })
        } finally {
          actionLoading.value = false
        }
      }
    },
  })
}

async function onTogglePublished() {
  if (!task.value) return
  actionLoading.value = true
  try {
    const newPublished = !task.value.published
    await tasksApi.markPublished(taskId.value, newPublished)
    if (task.value) {
      task.value.published = newPublished
    }
    uni.showToast({
      title: newPublished ? '已标记为已发布' : '已取消发布',
      icon: 'success',
    })
  } catch {
    uni.showToast({ title: '操作失败', icon: 'none' })
  } finally {
    actionLoading.value = false
  }
}

function onShare() {
  // Trigger WeChat share
  uni.showShareMenu({ withShareTicket: true })
  uni.showToast({ title: '请点击右上角分享', icon: 'none' })
}

async function onRetry() {
  if (!task.value) return
  actionLoading.value = true
  try {
    // Server clones full task configuration (style/author/ecommerce/image
    // model/watermark/goal mode...) — preserves everything, unlike the old
    // client-side create() which only forwarded a subset of fields.
    const newTask = await tasksApi.retry(task.value.id)

    uni.showToast({ title: '已重新创建任务，全部设置已保留', icon: 'success' })
    setTimeout(() => {
      uni.redirectTo({ url: `/pages/tasks/detail?id=${newTask.id}` })
    }, 500)
  } catch (err: unknown) {
    const e = err as { statusCode?: number; code?: number }
    if (e?.statusCode === 402 || e?.code === 40200) {
      uni.showToast({ title: '积分不足，无法重试', icon: 'none' })
    } else {
      uni.showToast({ title: (err as Error)?.message || '重试失败', icon: 'none' })
    }
  } finally {
    actionLoading.value = false
  }
}

onLoad((query) => {
  if (query?.id) {
    taskId.value = query.id
    startPolling(query.id).finally(() => {
      initialLoading.value = false
      void loadCompletedDetails()
      void loadProject()
    })
  } else {
    initialLoading.value = false
  }
})

onUnload(() => {
  stopPolling()
})

// Expose share message
onShareAppMessage(() => ({
  title: task.value?.title || '任务详情',
  path: `/pages/tasks/detail?id=${taskId.value}`,
}))
</script>

<style lang="scss" scoped>
.task-detail {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 240rpx;

  &__header {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    margin-bottom: $ab-space-sm;
    box-shadow: $ab-shadow-sm;
  }

  &__badges {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__title {
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    display: block;
    margin-bottom: $ab-space-sm;
    line-height: 1.3;
  }

  &__project {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    margin-bottom: $ab-space-sm;

    &-avatar {
      width: 32rpx;
      height: 32rpx;
      border-radius: 50%;
      background-color: $ab-divider;

      &--fallback {
        display: flex;
        align-items: center;
        justify-content: center;
        font-size: 18rpx;
        color: $ab-text-secondary;
        background-color: $ab-divider;
      }
    }

    &-name {
      font-size: $ab-text-xs;
      color: $ab-text-secondary;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  &__times {
    display: flex;
    flex-direction: column;
    gap: 4rpx;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__section {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    margin-bottom: $ab-space-sm;
    box-shadow: $ab-shadow-sm;

    &--error {
      border-left: 6rpx solid $ab-danger;
    }
  }

  &__billing-lock {
    display: flex;
    flex-direction: column;
    gap: 8rpx;
    margin-bottom: $ab-space-sm;
    padding: $ab-space-md;
    border-radius: $ab-radius-md;
    background-color: rgba(239, 68, 68, 0.1);
    border: 1rpx solid rgba(239, 68, 68, 0.35);
  }

  &__billing-title {
    font-size: $ab-text-sm;
    font-weight: $ab-font-semibold;
    color: #b91c1c;
  }

  &__billing-text {
    font-size: $ab-text-xs;
    color: #991b1b;
    line-height: 1.5;
  }

  &__progress-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-xs;
  }

  &__progress-title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__progress-percent {
    font-size: $ab-text-md;
    font-weight: $ab-font-bold;
    color: $ab-primary;
  }

  &__progress-desc {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    margin-top: $ab-space-xs;
    display: block;
  }

  &__stage-chip {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__polling-indicator {
    font-size: $ab-text-xs;
    color: $ab-primary;
    margin-top: $ab-space-xs;
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
  }

  &__error-text {
    font-size: $ab-text-sm;
    color: $ab-danger;
    line-height: 1.5;
    word-break: break-all;
  }

  &__warnings {
    margin-top: $ab-space-md;
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
  }

  &__warning {
    display: flex;
    align-items: flex-start;
    gap: $ab-space-xs;
    padding: $ab-space-sm;
    border-radius: $ab-radius-sm;
    background-color: $ab-warning-bg;
    border-left: 4rpx solid $ab-warning;

    &-icon {
      font-size: 24rpx;
      flex-shrink: 0;
    }

    &-text {
      font-size: $ab-text-xs;
      color: $ab-text;
      line-height: 1.5;
      flex: 1;
    }
  }

  &__goal-banner {
    display: flex;
    align-items: flex-start;
    gap: $ab-space-sm;
    background-color: $ab-warning-bg;
    border: 2rpx solid rgba($ab-warning, 0.4);
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    margin-bottom: $ab-space-sm;

    &-icon {
      font-size: 36rpx;
      flex-shrink: 0;
    }

    &-title {
      display: block;
      font-size: $ab-text-base;
      font-weight: $ab-font-semibold;
      color: $ab-warning;
      margin-bottom: 4rpx;
    }

    &-text {
      display: block;
      font-size: $ab-text-sm;
      color: $ab-text;
      line-height: 1.5;
      margin-bottom: 4rpx;
    }

    &-hint {
      display: block;
      font-size: $ab-text-xs;
      color: $ab-text-secondary;
      line-height: 1.5;
    }
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

    &-row {
      display: flex;
      gap: $ab-space-sm;
    }
  }
}

// Section title
.section-title {
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  display: block;
  margin-bottom: $ab-space-md;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-md;

  .section-title {
    margin-bottom: 0;
  }
}

.section-actions {
  display: flex;
  align-items: center;
  gap: $ab-space-md;
}

.section-action {
  font-size: $ab-text-sm;
  color: $ab-primary;
  font-weight: $ab-font-medium;

  &--muted {
    color: $ab-text-tertiary;
  }

  &--disabled {
    color: $ab-text-tertiary;
    opacity: 0.5;
  }
}

.billing-details {
  &__total {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: $ab-space-md;
    padding-bottom: $ab-space-md;
    border-bottom: 2rpx solid $ab-divider;
  }

  &__total-label {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
  }

  &__total-value {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }
}

.agent-profile-details {
  &__grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: $ab-space-md;
  }

  &__item {
    min-width: 0;

    &--wide {
      grid-column: 1 / -1;
    }
  }

  &__label,
  &__value {
    display: block;
  }

  &__label {
    color: $ab-text-tertiary;
    font-size: $ab-text-xs;
  }

  &__value {
    margin-top: 6rpx;
    color: $ab-text;
    font-size: $ab-text-sm;
    overflow-wrap: anywhere;

    &--model {
      font-family: monospace;
      font-size: $ab-text-xs;
    }
  }
}

.billing-detail {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: $ab-space-md;
  padding: $ab-space-md 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    padding-bottom: 0;
    border-bottom: none;
  }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__label,
  &__identity,
  &__pricing {
    display: block;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;
  }

  &__identity,
  &__pricing {
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    line-height: 1.4;
    color: $ab-text-tertiary;
    overflow-wrap: anywhere;
  }

  &__amount {
    flex-shrink: 0;
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;

    &--reversal {
      color: $ab-success;
    }
  }
}

.next-actions {
  border: 2rpx solid $ab-border;

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: $ab-space-sm;
  }
}

.next-action {
  padding: $ab-space-sm;
  border-radius: $ab-radius-sm;
  border: 2rpx solid $ab-border;
  background-color: $ab-background;

  &--primary {
    border-color: rgba($ab-primary, 0.35);
    background-color: $ab-primary-bg;
  }

  &--warning {
    border-color: rgba($ab-warning, 0.35);
    background-color: $ab-warning-bg;
  }

  &--danger {
    border-color: rgba($ab-danger, 0.35);
    background-color: $ab-danger-bg;
  }

  &__label {
    display: block;
    font-size: $ab-text-sm;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__desc {
    display: block;
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    line-height: 1.4;
    color: $ab-text-secondary;
  }
}

// Polling dot animation
.polling-dot {
  display: inline-block;
  width: 12rpx;
  height: 12rpx;
  border-radius: 50%;
  background-color: $ab-primary;
  animation: polling-pulse 1.5s ease-in-out infinite;
}

@keyframes polling-pulse {
  0%, 100% { opacity: 0.4; transform: scale(0.8); }
  50% { opacity: 1; transform: scale(1.2); }
}

// Workflow grid
.workflow-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: $ab-space-sm;
  margin-top: $ab-space-md;
}

.workflow-stage {
  display: flex;
  align-items: center;
  gap: $ab-space-xs;
  padding: $ab-space-sm;
  border-radius: $ab-radius-sm;
  background-color: $ab-background;
  border: 2rpx solid $ab-divider;

  &__icon {
    font-size: 28rpx;
    flex-shrink: 0;
  }

  &__label {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &--completed {
    border-color: rgba($ab-success, 0.3);
    background-color: $ab-success-bg;
  }

  &--running {
    border-color: rgba($ab-info, 0.3);
    background-color: $ab-info-bg;

    .workflow-stage__label {
      color: $ab-info;
      font-weight: $ab-font-medium;
    }
  }

  &--failed {
    border-color: rgba($ab-danger, 0.3);
    background-color: $ab-danger-bg;
  }

  &--warning {
    border-color: rgba($ab-warning, 0.3);
    background-color: $ab-warning-bg;
  }
}

// Image grid
.image-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: $ab-space-xs;

  &__item {
    width: 100%;
    aspect-ratio: 1;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
  }
}

.file-row {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__name,
  &__meta {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__name {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__meta {
    margin-top: 4rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Content preview
.content-preview {
  max-height: 600rpx;
  overflow: hidden;
  position: relative;
  font-size: $ab-text-sm;
  line-height: 1.8;
  color: $ab-text;

  &--collapsed {
    max-height: 300rpx;

    &::after {
      content: '';
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 100rpx;
      background: linear-gradient(transparent, $ab-surface);
    }
  }
}

.content-expand-btn {
  display: block;
  text-align: center;
  font-size: $ab-text-sm;
  color: $ab-primary;
  padding: $ab-space-sm 0;
  margin-top: $ab-space-xs;
}

// Log scroll
.log-scroll {
  height: 400rpx;
  background-color: #1F2937;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm;
  overflow: hidden;
}

.log-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;

  &__text {
    font-size: $ab-text-xs;
    color: rgba(255, 255, 255, 0.4);
  }
}

.log-entry {
  padding: 6rpx 0;
  border-bottom: 1rpx solid rgba(255, 255, 255, 0.06);

  &__text {
    font-size: 22rpx;
    color: rgba(255, 255, 255, 0.7);
    font-family: monospace;
    line-height: 1.5;
    word-break: break-all;
  }

  &--latest &__text {
    color: rgba(255, 255, 255, 1);
  }
}

// Quality review
.review-card {
  &__score {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: 4rpx;
    margin-bottom: $ab-space-md;
  }

  &__score-number {
    font-size: 96rpx;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    line-height: 1;
  }

  &__score-max {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }

  &__readiness {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-md;
  }

  &__readiness-label {
    font-size: $ab-text-base;
    color: $ab-text-secondary;
  }

  &__section {
    margin-top: $ab-space-md;
    padding-top: $ab-space-md;
    border-top: 2rpx solid $ab-divider;
  }

  &__section-title {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;
    margin-bottom: $ab-space-xs;
    display: block;
  }

  &__item {
    font-size: $ab-text-xs;
    line-height: 1.6;
    display: block;
    margin-bottom: 4rpx;
    padding-left: $ab-space-sm;

    &--positive { color: $ab-success; }
    &--negative { color: $ab-warning; }
  }
}

.analytics-card {
  &__title {
    display: block;
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
    line-height: 1.5;
  }

  &__link {
    display: block;
    margin-top: $ab-space-xs;
    font-size: $ab-text-xs;
    color: $ab-primary;
    word-break: break-all;
  }

  &__cover-wrap {
    margin-top: $ab-space-sm;
  }

  &__cover {
    width: 100%;
    height: 260rpx;
    border-radius: $ab-radius-sm;
    background-color: $ab-divider;
  }

  &__meta {
    display: flex;
    flex-direction: column;
    gap: 4rpx;
    margin-top: $ab-space-sm;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__error {
    display: block;
    margin-top: $ab-space-sm;
    padding: $ab-space-sm;
    border-radius: $ab-radius-sm;
    background-color: $ab-warning-bg;
    color: $ab-warning;
    font-size: $ab-text-xs;
    line-height: 1.5;
  }

  &__empty {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: $ab-space-sm;
  margin-top: $ab-space-sm;
}

.metric-tile {
  padding: $ab-space-sm;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;

  &__label {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__value {
    display: block;
    margin-top: 4rpx;
    font-size: $ab-text-lg;
    color: $ab-text;
    font-weight: $ab-font-semibold;
  }

  &__delta {
    display: block;
    margin-top: 2rpx;
    font-size: $ab-text-xs;
    color: $ab-success;
  }
}

// Article HTML preview overlay
.preview-overlay {
  position: fixed;
  inset: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 100;
  display: flex;
  align-items: flex-end;
  justify-content: center;

  &__panel {
    width: 100%;
    max-height: 85vh;
    background-color: $ab-surface;
    border-radius: $ab-radius-md $ab-radius-md 0 0;
    display: flex;
    flex-direction: column;
    padding-bottom: env(safe-area-inset-bottom);
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: $ab-space-md;
    border-bottom: 2rpx solid $ab-divider;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__actions {
    display: flex;
    align-items: center;
    gap: $ab-space-md;
  }

  &__action {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }

  &__body {
    flex: 1;
    padding: $ab-space-md;
    overflow-y: auto;
  }

  &__loading,
  &__empty {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: $ab-space-lg 0;
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }
}
</style>
