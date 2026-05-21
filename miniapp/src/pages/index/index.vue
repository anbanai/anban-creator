<template>
  <view class="dashboard">
    <!-- Greeting + Credits Card -->
    <view class="greeting-card">
      <view class="greeting-card__top">
        <text class="greeting-card__greeting">{{ greeting }}{{ nicknameSuffix }}</text>
      </view>
      <view class="greeting-card__bottom">
        <view class="greeting-card__credits" @tap="goCredits">
          <text class="greeting-card__credits-value" :class="{ 'animate-bounce': creditsAnimating }">
            {{ displayBalance }}
          </text>
          <text class="greeting-card__credits-label">积分</text>
        </view>
        <AbButton
          v-if="!signedInToday"
          type="primary"
          size="sm"
          :loading="signInLoading"
          @click="handleSignIn"
        >
          签到+10
        </AbButton>
        <view v-else class="greeting-card__signed">
          <text class="greeting-card__signed-icon">&#10003;</text>
          <text class="greeting-card__signed-text">今日已签到</text>
        </view>
      </view>
    </view>

    <!-- Stats Grid -->
    <view class="stats-grid">
      <view class="stats-grid__item">
        <StatsCard :value="activePlans" label="活跃计划" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="todayTasks" label="今日任务" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="successRate" label="成功率" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="inviteCount" label="邀请人数" />
      </view>
    </view>

    <!-- Error bar -->
    <view v-if="errorMsg" class="error-bar">
      <text class="error-bar__text">{{ errorMsg }}</text>
      <text class="error-bar__retry" @tap="loadAll">重试</text>
    </view>

    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <!-- Recent Tasks -->
    <view v-if="!pageLoading" class="section">
      <view class="section__header">
        <text class="section__title">最近任务</text>
        <text class="section__more" @tap="goTasks">更多 ›</text>
      </view>

      <view v-if="recentTasks.length > 0">
        <TaskCard
          v-for="task in recentTasks"
          :key="task.id"
          :task="task"
          @tap="goTaskDetail(task.id)"
        />
      </view>
      <AbEmpty
        v-else
        title="还没有任务"
        description="创建第一个任务开始智能创作"
        action-text="新建任务"
        @action="goCreateTask"
      />
    </view>

    <!-- Quick Actions -->
    <view v-if="!pageLoading" class="section">
      <text class="section__title">快捷操作</text>
      <view class="quick-actions">
        <view class="quick-actions__item" @tap="goCreateTask">
          <view class="quick-actions__icon quick-actions__icon--task">
            <text>&#128221;</text>
          </view>
          <text class="quick-actions__label">新建任务</text>
        </view>
        <view class="quick-actions__item" @tap="goCreatePlan">
          <view class="quick-actions__icon quick-actions__icon--plan">
            <text>&#128203;</text>
          </view>
          <text class="quick-actions__label">新建计划</text>
        </view>
        <view class="quick-actions__item" @tap="goWorkshop('viral-analysis')">
          <view class="quick-actions__icon quick-actions__icon--viral">
            <text>&#128300;</text>
          </view>
          <text class="quick-actions__label">爆文拆解</text>
        </view>
        <view class="quick-actions__item" @tap="goWorkshop('poster')">
          <view class="quick-actions__icon quick-actions__icon--poster">
            <text>&#127912;</text>
          </view>
          <text class="quick-actions__label">海报制作</text>
        </view>
      </view>
    </view>

    <!-- Bottom spacer for tab bar -->
    <view class="bottom-spacer" />
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { onPullDownRefresh } from '@dcloudio/uni-app'
import { useAuthStore } from '@/stores/auth'
import { creditsApi } from '@/api/credits'
import { tasksApi } from '@/api/tasks'
import { plansApi } from '@/api/plans'
import { getGreeting } from '@/utils/format'
import TaskCard from '@/components/business/TaskCard.vue'
import StatsCard from '@/components/business/StatsCard.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import type { Task } from '@/types'

const authStore = useAuthStore()

// --- Greeting ---
const greeting = getGreeting()
const nicknameSuffix = computed(() => {
  const name = authStore.user?.nickname || ''
  return name ? `，${name}` : ''
})

// --- Credits ---
const creditsBalance = ref(0)
const signedInToday = ref(false)
const signInLoading = ref(false)
const creditsAnimating = ref(false)

const displayBalance = computed(() => {
  return creditsBalance.value.toLocaleString()
})

async function loadCredits() {
  try {
    const [balanceRes, statusRes] = await Promise.all([
      creditsApi.balance(),
      creditsApi.signInStatus(),
    ])
    creditsBalance.value = balanceRes.balance
    signedInToday.value = statusRes.signed_in_today
  } catch (err) {
    console.error('Failed to load credits:', err)
  }
}

async function handleSignIn() {
  if (signInLoading.value || signedInToday.value) return
  signInLoading.value = true
  try {
    const res = await creditsApi.signIn()
    signedInToday.value = true

    // Animate balance +10
    const oldBalance = creditsBalance.value
    creditsBalance.value = oldBalance + (res.reward || 10)
    creditsAnimating.value = true
    setTimeout(() => {
      creditsAnimating.value = false
    }, 600)

    uni.showToast({ title: '签到成功 +10积分', icon: 'none' })
  } catch (err: any) {
    const msg = err?.message || '签到失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    signInLoading.value = false
  }
}

// --- Stats ---
const activePlans = ref(0)
const todayTasks = ref(0)
const successRate = ref('--')
const inviteCount = ref('0/10')

async function loadStats() {
  try {
    const [plansRes, tasksRes] = await Promise.all([
      plansApi.list(),
      tasksApi.list({ limit: 100 }),
    ])

    // Active plans
    const plans = plansRes.items || []
    activePlans.value = plans.filter((p) => p.status === 'active').length

    // Today's tasks (created today)
    const today = new Date()
    const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
    const tasks = tasksRes.items || []
    todayTasks.value = tasks.filter((t) => t.created_at && t.created_at.startsWith(todayStr)).length

    // Success rate
    const completed = tasks.filter((t) => t.status === 'completed').length
    const failed = tasks.filter((t) => t.status === 'failed').length
    const finished = completed + failed
    if (finished > 0) {
      successRate.value = `${Math.round((completed / finished) * 100)}%`
    } else {
      successRate.value = '--'
    }

    // Invite count from user info
    if (authStore.user) {
      const invited = authStore.user.invite_count ?? 0
      inviteCount.value = `${invited}/10`
    }
  } catch (err) {
    console.error('Failed to load stats:', err)
  }
}

// --- Recent Tasks ---
const recentTasks = ref<Task[]>([])

async function loadRecentTasks() {
  try {
    const res = await tasksApi.list({ limit: 5 })
    recentTasks.value = res.items || []
  } catch (err) {
    console.error('Failed to load recent tasks:', err)
  }
}

// --- Page state ---
const pageLoading = ref(true)
const errorMsg = ref('')

async function loadAll() {
  pageLoading.value = true
  errorMsg.value = ''
  try {
    await Promise.all([
      loadCredits(),
      loadStats(),
      loadRecentTasks(),
    ])
  } catch (err) {
    errorMsg.value = '数据加载失败'
  } finally {
    pageLoading.value = false
  }
}

onMounted(loadAll)

// Pull-down refresh
onPullDownRefresh(async () => {
  try {
    await Promise.all([
      loadCredits(),
      loadStats(),
      loadRecentTasks(),
    ])
  } finally {
    uni.stopPullDownRefresh()
  }
})

// --- Navigation ---
function goCredits() {
  uni.navigateTo({ url: '/pages/credits/transactions' })
}

function goTasks() {
  uni.switchTab({ url: '/pages/tasks/index' })
}

function goTaskDetail(taskId: string) {
  uni.navigateTo({ url: `/pages/tasks/detail?id=${taskId}` })
}

function goCreateTask() {
  uni.navigateTo({ url: '/pages/tasks/create' })
}

function goCreatePlan() {
  uni.navigateTo({ url: '/pages/plans/create' })
}

function goWorkshop(tab: string) {
  uni.navigateTo({ url: `/pages/workshop/index?tab=${tab}` })
}
</script>

<style lang="scss" scoped>
.dashboard {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

// Greeting card
.greeting-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-lg;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__top {
    margin-bottom: $ab-space-md;
  }

  &__greeting {
    font-size: $ab-text-xl;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__bottom {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__credits {
    display: flex;
    align-items: baseline;
    gap: $ab-space-xs;
    cursor: pointer;
  }

  &__credits-value {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    transition: transform 0.3s ease;
  }

  &__credits-label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__signed {
    display: flex;
    align-items: center;
    gap: 6rpx;
    padding: 8rpx $ab-space-md;
    background-color: $ab-divider;
    border-radius: $ab-radius-full;
  }

  &__signed-icon {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
  }

  &__signed-text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.animate-bounce {
  animation: credits-bounce 0.5s ease;
}

@keyframes credits-bounce {
  0% { transform: scale(1); }
  30% { transform: scale(1.25); }
  60% { transform: scale(0.95); }
  100% { transform: scale(1); }
}

// Stats grid
.stats-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-md;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg;
  padding: $ab-space-sm 0;
  box-shadow: $ab-shadow-sm;

  &__item {
    display: flex;
    justify-content: center;
  }
}

// Error bar
.error-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-warning-bg;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;
  margin-bottom: $ab-space-md;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-warning;
    flex: 1;
  }

  &__retry {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
    flex-shrink: 0;
    margin-left: $ab-space-md;
  }
}

// Section
.section {
  margin-bottom: $ab-space-lg;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__more {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }
}

// Quick actions
.quick-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;

  &__item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: $ab-space-xs;
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-lg $ab-space-md;
    box-shadow: $ab-shadow-sm;
  }

  &__icon {
    width: 80rpx;
    height: 80rpx;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 40rpx;

    &--task { background-color: $ab-primary-bg; }
    &--plan { background-color: $ab-success-bg; }
    &--viral { background-color: $ab-warning-bg; }
    &--poster { background-color: $ab-info-bg; }
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }
}

// Bottom spacer
.bottom-spacer {
  height: 120rpx;
}
</style>
