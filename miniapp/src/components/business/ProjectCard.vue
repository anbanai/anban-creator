<template>
  <view class="project-card" @tap="$emit('tap')">
    <view class="project-card__header">
      <PlatformAvatar :platform="project.platform" />
      <view class="project-card__info">
        <text class="project-card__name">{{ project.name }}</text>
        <text class="project-card__positioning" v-if="positioning">{{ positioning }}</text>
      </view>
    </view>
    <view class="project-card__stats" v-if="stats">
      <text class="stats-item success">OK {{ stats.completed_tasks }}</text>
      <text class="stats-item danger">ERR {{ stats.failed_tasks }}</text>
      <text class="stats-item">{{ stats.success_rate }}%成功</text>
    </view>
    <text class="project-card__activity" v-if="stats?.last_activity_at">
      最近活跃: {{ stats.last_activity_at }}
    </text>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Project, ProjectStats } from '@/types'
import PlatformAvatar from './PlatformAvatar.vue'

const props = defineProps<{
  project: Project
  stats?: ProjectStats | null
}>()

defineEmits<{
  tap: []
}>()

const positioning = computed(() => props.project.instructions || props.project.positioning || '')
</script>

<style lang="scss" scoped>
.project-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
  border-left: 6rpx solid $ab-border;

  &__header {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__info {
    flex: 1;
    min-width: 0;
  }

  &__name {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__positioning {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    display: block;
    margin-top: 2rpx;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__stats {
    display: flex;
    gap: $ab-space-md;
    margin-bottom: $ab-space-xs;
  }

  &__activity {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.stats-item {
  font-size: $ab-text-xs;
  color: $ab-text-secondary;

  &.success { color: $ab-success; }
  &.danger { color: $ab-danger; }
}
</style>
