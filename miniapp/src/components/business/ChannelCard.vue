<template>
  <view class="channel-card" @tap="$emit('tap')">
    <view class="channel-card__header">
      <PlatformAvatar :platform="channel.platform" />
      <view class="channel-card__info">
        <text class="channel-card__name">{{ channel.name }}</text>
        <text class="channel-card__positioning" v-if="channel.positioning">{{ channel.positioning }}</text>
      </view>
    </view>
    <view class="channel-card__stats" v-if="stats">
      <text class="stats-item success">✅{{ stats.completed_tasks }}</text>
      <text class="stats-item danger">❌{{ stats.failed_tasks }}</text>
      <text class="stats-item">{{ stats.success_rate }}%成功</text>
    </view>
    <text class="channel-card__activity" v-if="stats?.last_activity_at">
      最近活跃: {{ stats.last_activity_at }}
    </text>
  </view>
</template>

<script setup lang="ts">
import type { Channel, ChannelStats } from '@/types'
import PlatformAvatar from './PlatformAvatar.vue'

defineProps<{
  channel: Channel
  stats?: ChannelStats | null
}>()

defineEmits<{
  tap: []
}>()
</script>

<style lang="scss" scoped>
.channel-card {
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
