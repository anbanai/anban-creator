<template>
  <view class="task-card" @tap="$emit('tap')">
    <view class="task-card__header">
      <PlatformAvatar :platform="task.type" :size="36" />
      <view class="task-card__info">
        <text class="task-card__title">{{ task.title || '未命名任务' }}</text>
        <text class="task-card__meta">
          {{ platformLabel }}
          <text class="task-card__dot">·</text>
          {{ statusLabel }}
        </text>
      </view>
    </view>

    <!-- Progress bar for running tasks -->
    <view class="task-card__progress" v-if="task.status === 'running'">
      <view class="progress-track">
        <view class="progress-fill" :style="{ width: (task.progress ?? 0) + '%' }" />
      </view>
      <text class="task-card__progress-text">{{ task.progress ?? 0 }}%</text>
    </view>

    <!-- Image thumbnails for completed tasks -->
    <view class="task-card__images" v-if="task.status === 'completed' && thumbnailUrls.length > 0">
      <image
        v-for="(url, i) in thumbnailUrls.slice(0, 3)"
        :key="i"
        :src="url"
        class="task-card__thumbnail"
        mode="aspectFill"
        @tap.stop="onPreviewImage(i)"
      />
    </view>

    <!-- Published status -->
    <view class="task-card__footer" v-if="task.status === 'completed'">
      <text :class="['task-card__published', task.published ? 'published' : 'unpublished']">
        {{ task.published ? '✓ 已发布' : '未发布' }}
      </text>
      <text class="task-card__time">{{ timeText }}</text>
    </view>

    <!-- Error message -->
    <text class="task-card__error" v-if="task.status === 'failed' && task.error_message">
      {{ task.error_message }}
    </text>

    <!-- Time for other states -->
    <text class="task-card__time" v-if="task.status !== 'completed' && task.status !== 'failed'">
      {{ timeText }}
    </text>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Task } from '@/types'
import { taskStatusLabel, contentTypeLabel } from '@/utils/labels'
import { relativeTime } from '@/utils/format'
import PlatformAvatar from './PlatformAvatar.vue'

const props = defineProps<{
  task: Task
  thumbnailUrls?: string[]
}>()

defineEmits<{
  tap: []
}>()

const platformLabel = computed(() => contentTypeLabel[props.task.type] || props.task.type)
const statusLabel = computed(() => taskStatusLabel[props.task.status])

const timeText = computed(() => {
  if (props.task.completed_at) return relativeTime(props.task.completed_at)
  if (props.task.started_at) return relativeTime(props.task.started_at)
  return relativeTime(props.task.created_at)
})

function onPreviewImage(index: number) {
  if (props.thumbnailUrls?.length) {
    uni.previewImage({
      current: props.thumbnailUrls[index],
      urls: props.thumbnailUrls,
    })
  }
}
</script>

<style lang="scss" scoped>
.task-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;

  &__header {
    display: flex;
    align-items: flex-start;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__info {
    flex: 1;
    min-width: 0;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-medium;
    color: $ab-text;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__meta {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    margin-top: 2rpx;
    display: block;
  }

  &__dot {
    margin: 0 4rpx;
  }

  &__progress {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__images {
    display: flex;
    gap: $ab-space-xs;
    margin-bottom: $ab-space-sm;
  }

  &__thumbnail {
    width: 140rpx;
    height: 140rpx;
    border-radius: $ab-radius-sm;
    flex-shrink: 0;
  }

  &__footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__published {
    font-size: $ab-text-xs;
    &.published { color: $ab-success; }
    &.unpublished { color: $ab-text-tertiary; }
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__error {
    font-size: $ab-text-xs;
    color: $ab-danger;
    margin-top: $ab-space-xs;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  &__progress-text {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    flex-shrink: 0;
  }
}

.progress-track {
  flex: 1;
  height: 12rpx;
  background-color: $ab-divider;
  border-radius: $ab-radius-full;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background-color: $ab-primary;
  border-radius: $ab-radius-full;
  transition: width 0.5s ease;
}
</style>
