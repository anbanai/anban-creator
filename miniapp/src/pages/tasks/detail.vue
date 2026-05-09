<template>
  <view class="page task-detail">
    <AbLoading v-if="initialLoading" text="加载中" />

    <view v-else-if="task">
      <!-- Header card -->
      <view class="task-detail__header">
        <view class="task-detail__badges">
          <PlatformAvatar :platform="task.type" :size="36" />
          <AbBadge :variant="statusBadgeVariant">{{ statusLabel }}</AbBadge>
        </view>
        <text class="task-detail__title">{{ task.title || '未命名任务' }}</text>
        <view class="task-detail__times">
          <text class="task-detail__time">创建: {{ formatDateTimeCN(task.created_at) }}</text>
          <text v-if="task.started_at" class="task-detail__time">
            开始: {{ formatFullDateTimeCN(task.started_at) }}
          </text>
          <text v-if="task.completed_at" class="task-detail__time">
            完成: {{ formatFullDateTimeCN(task.completed_at) }}
          </text>
        </view>
      </view>

      <!-- Progress section (running/pending) -->
      <view
        v-if="task.status === 'running' || task.status === 'pending'"
        class="task-detail__section"
      >
        <text class="section-title">执行进度</text>
        <AbProgress
          :percent="task.progress ?? 0"
          :show-text="true"
          height="16rpx"
          :color="task.status === 'running' ? '#4F46E5' : '#D97706'"
        />
        <text v-if="progressMessage" class="task-detail__progress-msg">
          当前步骤: {{ progressMessage }}
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
      </view>

      <!-- Error message -->
      <view v-if="task.status === 'failed'" class="task-detail__section task-detail__section--error">
        <text class="section-title">错误信息</text>
        <text class="task-detail__error-text">{{ task.error_message || '任务执行失败' }}</text>
      </view>

      <!-- Generated images gallery -->
      <view
        v-if="imageUrls.length > 0"
        class="task-detail__section"
      >
        <text class="section-title">生成结果</text>
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
      <view v-if="logs.length > 0" class="task-detail__section">
        <text class="section-title">执行日志</text>
        <scroll-view
          class="log-scroll"
          scroll-y
          :scroll-top="logScrollTop"
          :scroll-with-animation="true"
        >
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

        <!-- Failed: retry -->
        <AbButton
          v-if="task.status === 'failed'"
          type="primary"
          size="lg"
          block
          :loading="actionLoading"
          @click="onRetry"
        >
          重新执行
        </AbButton>

        <!-- Completed: published + share -->
        <template v-if="task.status === 'completed'">
          <view class="task-detail__bottom-row">
            <AbButton
              type="ghost"
              size="md"
              :loading="actionLoading"
              @click="onTogglePublished"
              style="flex: 1;"
            >
              {{ task.published ? '取消发布标记' : '标记已发布' }}
            </AbButton>
            <AbButton
              type="primary"
              size="md"
              style="flex: 1;"
              @click="onShare"
            >
              分享结果
            </AbButton>
          </view>
        </template>

        <!-- Cancelled: retry -->
        <AbButton
          v-if="task.status === 'cancelled'"
          type="primary"
          size="lg"
          block
          :loading="actionLoading"
          @click="onRetry"
        >
          重新执行
        </AbButton>
      </view>
    </view>

    <!-- Task not found -->
    <AbEmpty v-else title="任务不存在" />
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { onLoad, onShareAppMessage } from '@dcloudio/uni-app'
import type { Task, WorkflowStage, WorkflowReview } from '@/types'
import { tasksApi } from '@/api/tasks'
import { taskStatusLabel } from '@/utils/labels'
import { formatDateTimeCN, formatFullDateTimeCN, sanitizeHtml } from '@/utils/format'
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
const logScrollTop = ref(0)

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

const statusLabel = computed(() => taskStatusLabel[task.value?.status as any] || '')

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

const workflowStages = computed<WorkflowStage[]>(() => {
  const ws = task.value?.workflow_status
  if (!ws || typeof ws === 'string') return []
  return (ws as any).stages || []
})

const review = computed<WorkflowReview | null>(() => {
  const ws = task.value?.workflow_status
  if (!ws || typeof ws === 'string') return null
  return (ws as any).review || null
})

const imageUrls = computed(() => {
  if (!task.value?.result?.files) return []
  return task.value.result.files
    .filter((f) => f.mime_type?.startsWith('image/'))
    .map((f) => f.url)
})

function stageIcon(stageStatus: string): string {
  switch (stageStatus) {
    case 'completed': return '✅'
    case 'running': return '🔄'
    case 'failed': return '❌'
    case 'warning': return '⚠️'
    default: return '⏳'
  }
}

function onPreviewImage(index: number) {
  if (imageUrls.value.length > 0) {
    uni.previewImage({
      current: imageUrls.value[index],
      urls: imageUrls.value,
    })
  }
}

// Auto-scroll logs to bottom when new entries arrive
watch(
  () => logs.value.length,
  () => {
    nextTick(() => {
      // Use a very large scrollTop to ensure it scrolls to bottom
      logScrollTop.value = 99999
    })
  },
)

async function onCancel() {
  uni.showModal({
    title: '取消任务',
    content: '确定取消该任务吗？',
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
    const newTask = await tasksApi.create({
      type: task.value.type,
      channel_id: task.value.channel_id,
      prompt: task.value.prompt,
      image_ratio: task.value.image_ratio || undefined,
      generate_video: task.value.generate_video || undefined,
    })

    uni.showToast({ title: '已重新创建任务', icon: 'success' })
    setTimeout(() => {
      uni.redirectTo({ url: `/pages/tasks/detail?id=${newTask.id}` })
    }, 500)
  } catch {
    uni.showToast({ title: '重试失败', icon: 'none' })
  } finally {
    actionLoading.value = false
  }
}

// Right-top "more" menu via navigation bar
onLoad((query) => {
  if (query?.id) {
    taskId.value = query.id
    startPolling(query.id).finally(() => {
      initialLoading.value = false
    })
  } else {
    initialLoading.value = false
  }
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
  padding-bottom: 200rpx;

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

  &__progress-msg {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-top: $ab-space-sm;
    display: block;
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
</style>
