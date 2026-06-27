<template>
  <view class="page projects-page">
    <!-- Platform filter tabs -->
    <view class="projects-page__tabs">
      <AbTabs v-model="activePlatform" :tabs="platformTabs" />
    </view>

    <!-- Loading skeleton -->
    <view v-if="loading && projects.length === 0" class="projects-page__skeleton">
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

    <!-- Project list -->
    <view v-else-if="filteredActive.length > 0" class="projects-page__list">
      <view
        v-for="project in filteredActive"
        :key="project.id"
        class="project-item"
        @tap="goToDetail(project.id)"
      >
        <view class="project-item__body" :style="{ borderLeftColor: platformColor(project.platform) }">
          <view class="project-item__header">
            <view class="project-item__avatar">
              <image
                v-if="project.avatar_url"
                :src="project.avatar_url"
                class="project-item__avatar-img"
                mode="aspectFill"
              />
              <PlatformAvatar v-else :platform="project.platform" :size="48" />
            </view>
            <view class="project-item__info">
              <text class="project-item__name">{{ project.name }}</text>
              <text class="project-item__platform">
                {{ platformLabel(project.platform) }}
                <text v-if="project.positioning"> · {{ project.positioning }}</text>
              </text>
            </view>
          </view>

          <!-- Stats row -->
          <view class="project-item__stats" v-if="project.stats">
            <text class="project-item__stat project-item__stat--success">
              {{ project.stats.completed_tasks }} 完成
            </text>
            <text class="project-item__stat project-item__stat--danger" v-if="project.stats.failed_tasks > 0">
              {{ project.stats.failed_tasks }} 失败
            </text>
            <text class="project-item__stat project-item__stat--rate" v-if="project.stats.success_rate != null">
              {{ project.stats.success_rate }}% 成功
            </text>
            <text class="project-item__stat project-item__stat--time" v-if="project.stats.last_activity_at">
              最近活跃: {{ relativeTime(project.stats.last_activity_at) }}
            </text>
          </view>

          <!-- Action buttons -->
          <view class="project-item__actions">
            <text class="project-item__action" @tap.stop="goToDetail(project.id)">编辑</text>
            <text class="project-item__action project-item__action--warn" @tap.stop="onArchive(project)">归档</text>
          </view>
        </view>
      </view>
    </view>

    <!-- Archived projects -->
    <view v-if="filteredArchived.length > 0" class="projects-page__archived">
      <view class="projects-page__archived-header" @tap="showArchived = !showArchived">
        <text class="projects-page__archived-title">
          已归档 ({{ filteredArchived.length }})
        </text>
        <text class="projects-page__archived-arrow">{{ showArchived ? '收起' : '展开' }}</text>
      </view>
      <view v-if="showArchived" class="projects-page__list">
        <view
          v-for="project in filteredArchived"
          :key="project.id"
          class="project-item project-item--archived"
          @tap="goToDetail(project.id)"
        >
          <view class="project-item__body">
            <view class="project-item__header">
              <view class="project-item__avatar">
                <image
                  v-if="project.avatar_url"
                  :src="project.avatar_url"
                  class="project-item__avatar-img"
                  mode="aspectFill"
                />
                <PlatformAvatar v-else :platform="project.platform" :size="48" />
              </view>
              <view class="project-item__info">
                <text class="project-item__name">{{ project.name }}</text>
                <text class="project-item__platform">{{ platformLabel(project.platform) }}</text>
              </view>
            </view>
            <view class="project-item__actions">
              <text class="project-item__action" @tap.stop="onRestore(project)">恢复</text>
              <text class="project-item__action project-item__action--danger" @tap.stop="onDelete(project)">删除</text>
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
    <view v-if="loading" class="projects-page__loading">
      <AbLoading size="sm" text="加载中" />
    </view>
    <text v-if="!hasMore && projects.length > 0" class="projects-page__end">
      — 已经到底了 —
    </text>

    <!-- Fixed bottom button -->
    <view v-if="filteredActive.length > 0 || filteredArchived.length > 0" class="projects-page__fab">
      <AbButton type="primary" block size="lg" @click="goToDetail()">
        + 新建账号
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import type { Project, ProjectStats } from '@/types'
import { projectsApi } from '@/api/projects'
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
  { key: 'ecommerce', label: '电商出图' },
  { key: 'xls', label: '小绿书' },
]

const activePlatform = ref('')
const projects = ref<Project[]>([])
const statsMap = ref<Record<string, ProjectStats>>({})
const loading = ref(false)
const refreshing = ref(false)
const hasMore = ref(true)
const showArchived = ref(false)
const page = ref(0)
const pageSize = 20

const filteredActive = computed(() => {
  if (!activePlatform.value) return projects.value.filter((c) => c.status === 'active')
  return projects.value.filter((c) => c.status === 'active' && c.platform === activePlatform.value)
})

const filteredArchived = computed(() => {
  if (!activePlatform.value) return projects.value.filter((c) => c.status === 'archived')
  return projects.value.filter((c) => c.status === 'archived' && c.platform === activePlatform.value)
})

function platformLabel(platform: string): string {
  return contentTypeLabel[platform] || platform
}

function platformColor(platform: string): string {
  const map: Record<string, string> = {
    seednote: '#FF2442',
    article: '#07C160',
    ecommerce: '#FF6900',
    xls: '#07C160',
  }
  return map[platform] || '#6B7280'
}

async function fetchProjects(reset = false) {
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
    const res = await projectsApi.list({
      status: undefined,
      platform: activePlatform.value || undefined,
    })

    // Backend returns full list; simulate pagination in client
    // For a real paginated API, use offset/limit params
    if (Array.isArray(res)) {
      if (reset) {
        projects.value = res
      } else {
        projects.value = [...projects.value, ...res]
      }
      hasMore.value = false // full list loaded
    } else if (res && 'items' in res) {
      const items = (res as any).items || []
      if (reset) {
        projects.value = items
      } else {
        projects.value = [...projects.value, ...items]
      }
      hasMore.value = items.length >= pageSize
    }

    // Load stats for active projects
    await loadStats()
  } catch (err) {
    console.error('Failed to load projects:', err)
    uni.showToast({ title: '加载失败', icon: 'none' })
    if (reset) projects.value = []
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function loadStats() {
  const activeIds = projects.value
    .filter((c) => c.status === 'active')
    .map((c) => c.id)

  if (activeIds.length === 0) return

  try {
    statsMap.value = await projectsApi.stats(activeIds)
    // Attach stats to projects
    projects.value = projects.value.map((c) => ({
      ...c,
      stats: statsMap.value[c.id] || c.stats,
    }))
  } catch (err) {
    // Stats are non-critical, ignore errors
    console.error('Failed to load stats:', err)
  }
}

function goToDetail(id?: string) {
  const url = id ? `/pages/projects/detail?id=${id}` : '/pages/projects/detail'
  uni.navigateTo({ url })
}

function onArchive(project: Project) {
  uni.showModal({
    title: '归档账号',
    content: `确定归档「${project.name}」吗？归档后可随时恢复。`,
    success: async (res) => {
      if (res.confirm) {
        try {
          await projectsApi.archive(project.id)
          project.status = 'archived'
          uni.showToast({ title: '已归档', icon: 'success' })
        } catch (err) {
          uni.showToast({ title: '归档失败', icon: 'none' })
        }
      }
    },
  })
}

function onRestore(project: Project) {
  try {
    projectsApi.restore(project.id)
    project.status = 'active'
    uni.showToast({ title: '已恢复', icon: 'success' })
  } catch (err) {
    uni.showToast({ title: '恢复失败', icon: 'none' })
  }
}

function onDelete(project: Project) {
  uni.showModal({
    title: '删除账号',
    content: `确定删除「${project.name}」吗？此操作不可恢复。`,
    confirmColor: '#DC2626',
    success: async (res) => {
      if (res.confirm) {
        try {
          await projectsApi.delete(project.id)
          projects.value = projects.value.filter((c) => c.id !== project.id)
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
  fetchProjects(true)
})

// Pull-down refresh
onPullDownRefresh(async () => {
  await fetchProjects(true)
  uni.stopPullDownRefresh()
})

// Reach bottom load more
onReachBottom(() => {
  if (!loading.value && hasMore.value) {
    fetchProjects(false)
  }
})
</script>

<style lang="scss" scoped>
.projects-page {
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

// Project item
.project-item {
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
