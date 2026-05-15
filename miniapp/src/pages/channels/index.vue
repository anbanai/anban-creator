<template>
  <view class="page channels-page">
    <!-- Platform filter tabs -->
    <view class="channels-page__tabs">
      <AbTabs v-model="activePlatform" :tabs="platformTabs" />
    </view>

    <!-- Loading skeleton -->
    <view v-if="loading && channels.length === 0" class="channels-page__skeleton">
      <view v-for="i in 3" :key="i" class="skeleton-card">
        <view class="skeleton-card__header">
          <view class="skeleton-card__avatar" />
          <view class="skeleton-card__lines">
            <view class="skeleton-card__line skeleton-card__line--title" />
            <view class="skeleton-card__line skeleton-card__line--sub" />
          </view>
        </view>
        <view class="skeleton-card__footer">
          <view class="skeleton-card__line skeleton-card__line--stats" />
        </view>
      </view>
    </view>

    <!-- Channel list -->
    <view v-else-if="filteredActive.length > 0" class="channels-page__list">
      <view
        v-for="channel in filteredActive"
        :key="channel.id"
        class="channel-item"
        @tap="goToDetail(channel.id)"
      >
        <view class="channel-item__body" :style="{ borderLeftColor: platformColor(channel.platform) }">
          <view class="channel-item__header">
            <view class="channel-item__avatar">
              <image
                v-if="channel.avatar_url"
                :src="channel.avatar_url"
                class="channel-item__avatar-img"
                mode="aspectFill"
              />
              <PlatformAvatar v-else :platform="channel.platform" :size="48" />
            </view>
            <view class="channel-item__info">
              <text class="channel-item__name">{{ channel.name }}</text>
              <text class="channel-item__platform">
                {{ platformLabel(channel.platform) }}
                <text v-if="channel.positioning"> · {{ channel.positioning }}</text>
              </text>
            </view>
          </view>

          <!-- Stats row -->
          <view class="channel-item__stats" v-if="channel.stats">
            <text class="channel-item__stat channel-item__stat--success">
              {{ channel.stats.completed_tasks }} 完成
            </text>
            <text class="channel-item__stat channel-item__stat--danger" v-if="channel.stats.failed_tasks > 0">
              {{ channel.stats.failed_tasks }} 失败
            </text>
            <text class="channel-item__stat channel-item__stat--rate" v-if="channel.stats.success_rate != null">
              {{ channel.stats.success_rate }}% 成功
            </text>
            <text class="channel-item__stat channel-item__stat--time" v-if="channel.stats.last_activity_at">
              最近活跃: {{ relativeTime(channel.stats.last_activity_at) }}
            </text>
          </view>

          <!-- Action buttons -->
          <view class="channel-item__actions">
            <text class="channel-item__action" @tap.stop="goToDetail(channel.id)">编辑</text>
            <text class="channel-item__action channel-item__action--warn" @tap.stop="onArchive(channel)">归档</text>
          </view>
        </view>
      </view>
    </view>

    <!-- Archived channels -->
    <view v-if="filteredArchived.length > 0" class="channels-page__archived">
      <view class="channels-page__archived-header" @tap="showArchived = !showArchived">
        <text class="channels-page__archived-title">
          已归档 ({{ filteredArchived.length }})
        </text>
        <text class="channels-page__archived-arrow">{{ showArchived ? '收起' : '展开' }}</text>
      </view>
      <view v-if="showArchived" class="channels-page__list">
        <view
          v-for="channel in filteredArchived"
          :key="channel.id"
          class="channel-item channel-item--archived"
          @tap="goToDetail(channel.id)"
        >
          <view class="channel-item__body">
            <view class="channel-item__header">
              <view class="channel-item__avatar">
                <image
                  v-if="channel.avatar_url"
                  :src="channel.avatar_url"
                  class="channel-item__avatar-img"
                  mode="aspectFill"
                />
                <PlatformAvatar v-else :platform="channel.platform" :size="48" />
              </view>
              <view class="channel-item__info">
                <text class="channel-item__name">{{ channel.name }}</text>
                <text class="channel-item__platform">{{ platformLabel(channel.platform) }}</text>
              </view>
            </view>
            <view class="channel-item__actions">
              <text class="channel-item__action" @tap.stop="onRestore(channel)">恢复</text>
              <text class="channel-item__action channel-item__action--danger" @tap.stop="onDelete(channel)">删除</text>
            </view>
          </view>
        </view>
      </view>
    </view>

    <!-- Empty state -->
    <AbEmpty
      v-if="!loading && filteredActive.length === 0 && filteredArchived.length === 0"
      title="还没有账号"
      description="创建第一个账号开始创作吧"
      action-text="新建账号"
      @action="goToDetail()"
    />

    <!-- Load more indicator -->
    <view v-if="loading" class="channels-page__loading">
      <AbLoading size="sm" text="加载中" />
    </view>
    <text v-if="!hasMore && channels.length > 0" class="channels-page__end">
      — 已经到底了 —
    </text>

    <!-- Fixed bottom button -->
    <view v-if="filteredActive.length > 0 || filteredArchived.length > 0" class="channels-page__fab">
      <AbButton type="primary" block size="lg" @click="goToDetail()">
        + 新建账号
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import type { Channel, ChannelStats } from '@/types'
import { channelsApi } from '@/api/channels'
import { contentTypeLabel } from '@/utils/labels'
import { relativeTime } from '@/utils/format'
import AbTabs from '@/components/common/AbTabs.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import PlatformAvatar from '@/components/business/PlatformAvatar.vue'

const platformTabs = [
  { key: '', label: '全部' },
  { key: 'seednote', label: '种草笔记' },
  { key: 'article', label: '公众号' },
  { key: 'xls', label: '小绿书' },
]

const activePlatform = ref('')
const channels = ref<Channel[]>([])
const statsMap = ref<Record<string, ChannelStats>>({})
const loading = ref(false)
const refreshing = ref(false)
const hasMore = ref(true)
const showArchived = ref(false)
const page = ref(0)
const pageSize = 20

const filteredActive = computed(() => {
  if (!activePlatform.value) return channels.value.filter((c) => c.status === 'active')
  return channels.value.filter((c) => c.status === 'active' && c.platform === activePlatform.value)
})

const filteredArchived = computed(() => {
  if (!activePlatform.value) return channels.value.filter((c) => c.status === 'archived')
  return channels.value.filter((c) => c.status === 'archived' && c.platform === activePlatform.value)
})

function platformLabel(platform: string): string {
  return contentTypeLabel[platform] || platform
}

function platformColor(platform: string): string {
  const map: Record<string, string> = {
    seednote: '#FF2442',
    article: '#07C160',
    xls: '#07C160',
  }
  return map[platform] || '#6B7280'
}

async function fetchChannels(reset = false) {
  if (reset) {
    page.value = 0
    hasMore.value = true
  }

  if (!hasMore.value && !reset) return

  if (reset) {
    refreshing.value = true
  } else {
    loading.value = true
  }

  try {
    const res = await channelsApi.list({
      status: undefined,
      platform: activePlatform.value || undefined,
    })

    // Backend returns full list; simulate pagination in client
    // For a real paginated API, use offset/limit params
    if (Array.isArray(res)) {
      if (reset) {
        channels.value = res
      } else {
        channels.value = [...channels.value, ...res]
      }
      hasMore.value = false // full list loaded
    } else if (res && 'items' in res) {
      const items = (res as any).items || []
      if (reset) {
        channels.value = items
      } else {
        channels.value = [...channels.value, ...items]
      }
      hasMore.value = items.length >= pageSize
    }

    // Load stats for active channels
    await loadStats()
  } catch (err) {
    console.error('Failed to load channels:', err)
    uni.showToast({ title: '加载失败', icon: 'none' })
    if (reset) channels.value = []
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function loadStats() {
  const activeIds = channels.value
    .filter((c) => c.status === 'active')
    .map((c) => c.id)

  if (activeIds.length === 0) return

  try {
    statsMap.value = await channelsApi.stats(activeIds)
    // Attach stats to channels
    channels.value = channels.value.map((c) => ({
      ...c,
      stats: statsMap.value[c.id] || c.stats,
    }))
  } catch (err) {
    // Stats are non-critical, ignore errors
    console.error('Failed to load stats:', err)
  }
}

function goToDetail(id?: string) {
  const url = id ? `/pages/channels/detail?id=${id}` : '/pages/channels/detail'
  uni.navigateTo({ url })
}

function onArchive(channel: Channel) {
  uni.showModal({
    title: '归档账号',
    content: `确定归档「${channel.name}」吗？归档后可随时恢复。`,
    success: async (res) => {
      if (res.confirm) {
        try {
          await channelsApi.archive(channel.id)
          channel.status = 'archived'
          uni.showToast({ title: '已归档', icon: 'success' })
        } catch (err) {
          uni.showToast({ title: '归档失败', icon: 'none' })
        }
      }
    },
  })
}

function onRestore(channel: Channel) {
  try {
    channelsApi.restore(channel.id)
    channel.status = 'active'
    uni.showToast({ title: '已恢复', icon: 'success' })
  } catch (err) {
    uni.showToast({ title: '恢复失败', icon: 'none' })
  }
}

function onDelete(channel: Channel) {
  uni.showModal({
    title: '删除账号',
    content: `确定删除「${channel.name}」吗？此操作不可恢复。`,
    confirmColor: '#DC2626',
    success: async (res) => {
      if (res.confirm) {
        try {
          await channelsApi.delete(channel.id)
          channels.value = channels.value.filter((c) => c.id !== channel.id)
          uni.showToast({ title: '已删除', icon: 'success' })
        } catch (err) {
          uni.showToast({ title: '删除失败', icon: 'none' })
        }
      }
    },
  })
}

// Watch platform filter change
watch(activePlatform, () => {
  fetchChannels(true)
})

// Pull-down refresh
onPullDownRefresh(async () => {
  await fetchChannels(true)
  uni.stopPullDownRefresh()
})

// Reach bottom load more
onReachBottom(() => {
  if (!loading.value && hasMore.value) {
    fetchChannels(false)
  }
})
</script>

<style lang="scss" scoped>
.channels-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding-bottom: 160rpx;

  &__tabs {
    background-color: $ab-surface;
    position: sticky;
    top: 0;
    z-index: 10;
  }

  &__list {
    padding: $ab-space-md;
  }

  &__skeleton {
    padding: $ab-space-md;
  }

  &__loading {
    display: flex;
    justify-content: center;
    padding: $ab-space-lg 0;
  }

  &__end {
    display: block;
    text-align: center;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    padding: $ab-space-md 0;
  }

  &__archived {
    margin-top: $ab-space-sm;
    border-top: 2rpx solid $ab-divider;

    &-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: $ab-space-sm $ab-space-md;
      background-color: $ab-divider;
      cursor: pointer;
    }

    &-title {
      font-size: $ab-text-sm;
      color: $ab-text-secondary;
      font-weight: $ab-font-medium;
    }

    &-arrow {
      font-size: $ab-text-xs;
      color: $ab-text-tertiary;
    }
  }

  &__fab {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    padding: $ab-space-md $ab-space-lg;
    padding-bottom: calc(#{$ab-space-md} + env(safe-area-inset-bottom));
    background-color: $ab-surface;
    box-shadow: 0 -4rpx 12rpx rgba(0, 0, 0, 0.06);
    z-index: 20;
  }
}

// Skeleton cards
.skeleton-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;

  &__header {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__avatar {
    width: 96rpx;
    height: 96rpx;
    border-radius: 50%;
    background-color: $ab-divider;
  }

  &__lines {
    flex: 1;
  }

  &__line {
    height: 28rpx;
    background-color: $ab-divider;
    border-radius: 8rpx;
    margin-bottom: $ab-space-xs;

    &--title {
      width: 60%;
    }

    &--sub {
      width: 80%;
      height: 22rpx;
    }

    &--stats {
      width: 50%;
      height: 22rpx;
    }
  }

  &__footer {
    margin-top: $ab-space-xs;
  }
}

// Channel item
.channel-item {
  margin-bottom: $ab-space-sm;

  &__body {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md;
    border-left: 6rpx solid transparent;
    box-shadow: $ab-shadow-sm;
  }

  &--archived &__body {
    opacity: 0.7;
  }

  &__header {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__avatar {
    width: 96rpx;
    height: 96rpx;
    border-radius: 50%;
    overflow: hidden;
    flex-shrink: 0;
    background-color: $ab-divider;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  &__avatar-img {
    width: 100%;
    height: 100%;
  }

  &__info {
    flex: 1;
    min-width: 0;
  }

  &__name {
    font-size: $ab-text-md;
    font-weight: $ab-font-medium;
    color: $ab-text;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__platform {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    display: block;
    margin-top: 4rpx;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__stats {
    display: flex;
    flex-wrap: wrap;
    gap: $ab-space-sm;
    margin-bottom: $ab-space-sm;
  }

  &__stat {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;

    &--success { color: $ab-success; }
    &--danger { color: $ab-danger; }
    &--rate { color: $ab-info; }
    &--time { color: $ab-text-tertiary; }
  }

  &__actions {
    display: flex;
    justify-content: flex-end;
    gap: $ab-space-md;
    padding-top: $ab-space-xs;
    border-top: 2rpx solid $ab-divider;
  }

  &__action {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    padding: 8rpx $ab-space-md;
    border-radius: $ab-radius-sm;

    &--warn { color: $ab-warning; }
    &--danger { color: $ab-danger; }
  }
}
</style>
