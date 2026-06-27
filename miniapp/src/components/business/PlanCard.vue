<template>
  <view class="plan-card" @tap="$emit('tap')">
    <view class="plan-card__header">
      <PlatformAvatar :platform="plan.type" :size="36" />
      <view class="plan-card__info">
        <text class="plan-card__title">{{ displayTitle }}</text>
        <text class="plan-card__meta">{{ platformLabel }} · {{ cronHuman }}</text>
        <text v-if="projectName" class="plan-card__project">{{ projectName }}</text>
      </view>
      <text :class="['plan-card__status', `status-${plan.status}`]">
        {{ statusLabel }}
      </text>
    </view>
    <text class="plan-card__next" v-if="plan.status === 'active' && plan.next_run_at">
      下次执行: {{ formatDateTimeCN(plan.next_run_at) }}
    </text>
    <view class="plan-card__actions">
      <text class="plan-card__action" v-if="plan.status === 'active'" @tap.stop="$emit('pause')">
        暂停
      </text>
      <text class="plan-card__action" v-if="plan.status === 'paused'" @tap.stop="$emit('resume')">
        恢复
      </text>
      <text class="plan-card__action" @tap.stop="$emit('edit')">
        编辑
      </text>
      <text class="plan-card__action plan-card__action--danger" @tap.stop="$emit('delete')">
        删除
      </text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Plan, Project } from '@/types'
import { planStatusLabel, contentTypeLabel, cronToHuman } from '@/utils/labels'
import { formatDateTimeCN } from '@/utils/format'
import PlatformAvatar from './PlatformAvatar.vue'

const props = defineProps<{
  plan: Plan
  project?: Project
}>()

defineEmits<{
  tap: []
  pause: []
  resume: []
  edit: []
  delete: []
}>()

// Mirror studio: fall back to "<type>计划" when no prompt; otherwise show prompt.
const displayTitle = computed(() => {
  if (props.plan.prompt?.trim()) return props.plan.prompt
  return `${contentTypeLabel[props.plan.type] || props.plan.type}计划`
})

const platformLabel = computed(() => contentTypeLabel[props.plan.type] || props.plan.type)
const statusLabel = computed(() => planStatusLabel[props.plan.status])
const cronHuman = computed(() => cronToHuman(props.plan.cron_expr))
const projectName = computed(() => props.project?.name || '')
</script>

<style lang="scss" scoped>
.plan-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;

  &__header {
    display: flex;
    align-items: flex-start;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-xs;
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
    display: block;
    margin-top: 2rpx;
  }

  &__project {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    display: block;
    margin-top: 2rpx;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__status {
    font-size: $ab-text-xs;
    padding: 4rpx 12rpx;
    border-radius: $ab-radius-full;
    flex-shrink: 0;

    &.status-active {
      background-color: $ab-info-bg;
      color: $ab-info;
    }
    &.status-paused {
      background-color: $ab-warning-bg;
      color: $ab-warning;
    }
    &.status-completed {
      background-color: $ab-success-bg;
      color: $ab-success;
    }
  }

  &__next {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-xs;
    padding-left: 56rpx;
  }

  &__actions {
    display: flex;
    gap: $ab-space-lg;
    padding-left: 56rpx;
    margin-top: $ab-space-xs;
    border-top: 2rpx solid $ab-divider;
    padding-top: $ab-space-xs;
  }

  &__action {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    padding: 4rpx 0;

    &--danger {
      color: $ab-danger;
    }
  }
}
</style>
