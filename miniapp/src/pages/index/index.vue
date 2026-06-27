<template>
  <view class="dashboard">
    <!-- Greeting + Credits Card -->
    <view class="greeting-card">
      <view class="greeting-card__top">
        <text class="greeting-card__greeting">{{ greeting }}{{ nicknameSuffix }}</text>
        <text class="greeting-card__sub">以下是你的内容工作区概览</text>
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
          签到 +{{ dailySignInCredits }}
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
        <StatsCard :value="activePlans" label="活跃计划" :desc="activePlansDesc" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="todayTasks" label="今日任务" :desc="todayTasksDesc" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="successRate" label="成功率" desc="按今日完成与失败计算" />
      </view>
      <view class="stats-grid__item">
        <StatsCard :value="totalTasksSample" label="任务样本" desc="最近 100 条任务" />
      </view>
    </view>

    <!-- Error bar -->
    <view v-if="errorMsg" class="error-bar">
      <text class="error-bar__text">{{ errorMsg }}</text>
      <text class="error-bar__retry" @tap="loadAll">重试</text>
    </view>

    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <!-- Invite card -->
    <view v-if="!pageLoading" class="invite-card">
      <view class="invite-card__main">
        <text class="invite-card__label">我的邀请码</text>
        <text class="invite-card__code">{{ inviteCode || '------' }}</text>
        <text class="invite-card__count">
          已邀请 {{ inviteCountDisplay }} / {{ maxInvites }} 人
        </text>
      </view>
      <AbButton
        type="ghost"
        size="sm"
        :disabled="!inviteCode"
        @click="copyInviteLink"
      >
        复制邀请链接
      </AbButton>
    </view>

    <!-- Charts -->
    <view v-if="!pageLoading" class="charts-row">
      <!-- Trend bar chart (last 30 days) -->
      <view class="chart-card">
        <view class="chart-card__head">
          <text class="chart-card__title">任务趋势</text>
          <text class="chart-card__hint">近 30 天</text>
        </view>
        <TrendBars v-if="trendData.length > 0" :data="trendData" />
        <view v-else class="chart-card__empty">暂无数据</view>
      </view>

      <!-- Status distribution donut -->
      <view class="chart-card">
        <view class="chart-card__head">
          <text class="chart-card__title">状态分布</text>
          <text class="chart-card__hint">近 100 条任务</text>
        </view>
        <StatusDonut
          v-if="statusData.length > 0"
          :completed="statusCounts.completed"
          :failed="statusCounts.failed"
          :cancelled="statusCounts.cancelled"
        />
        <view v-else class="chart-card__empty">暂无数据</view>
      </view>
    </view>

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
        <view class="quick-actions__item" @tap="goTimeline">
          <view class="quick-actions__icon quick-actions__icon--timeline">
            <text>&#128197;</text>
          </view>
          <text class="quick-actions__label">查看时间轴</text>
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
        <view class="quick-actions__item" @tap="goCredits">
          <view class="quick-actions__icon quick-actions__icon--credits">
            <text>&#128176;</text>
          </view>
          <text class="quick-actions__label">积分明细</text>
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
import TrendBars from '@/components/business/TrendBars.vue'
import StatusDonut from '@/components/business/StatusDonut.vue'
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

// --- Credits + pricing (daily sign-in amount) ---
const creditsBalance = ref(0)
const signedInToday = ref(false)
const signInLoading = ref(false)
const creditsAnimating = ref(false)
const dailySignInCredits = ref(10)

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

async function loadPricing() {
  // Best-effort: failures fall back to default reward of 10.
  try {
    const pricing = await creditsApi.pricing()
    if (pricing?.income?.daily_sign_in) {
      dailySignInCredits.value = pricing.income.daily_sign_in
    }
  } catch (err) {
    console.error('Failed to load pricing:', err)
  }
}

async function handleSignIn() {
  if (signInLoading.value || signedInToday.value) return
  signInLoading.value = true
  try {
    const res = await creditsApi.signIn()
    signedInToday.value = true

    // Animate balance +reward
    const oldBalance = creditsBalance.value
    creditsBalance.value = oldBalance + (res.reward || dailySignInCredits.value)
    creditsAnimating.value = true
    setTimeout(() => {
      creditsAnimating.value = false
    }, 600)

    uni.showToast({
      title: `签到成功 +${res.reward || dailySignInCredits.value}积分`,
      icon: 'none',
    })
  } catch (err: any) {
    const msg = err?.message || '签到失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    signInLoading.value = false
  }
}

// --- Stats (mirrors studio DashboardPage exact set) ---
const activePlans = ref(0)
const todayTasks = ref(0)
const successRate = ref('--')
const totalTasksSample = ref(0)
const completedToday = ref(0)
const failedToday = ref(0)

// Today-bound counts for the "今日任务" description.
const activePlansDesc = computed(() => '当前启用的自动调度')
const todayTasksDesc = computed(() => `${completedToday.value} 已完成，${failedToday.value} 失败`)

// --- Charts data ---
interface TrendPoint { date: string; count: number }
const trendData = ref<TrendPoint[]>([])
const statusCounts = ref({ completed: 0, failed: 0, cancelled: 0 })
const statusData = computed(() => {
  const c = statusCounts.value
  return [
    { name: '已完成', value: c.completed },
    { name: '失败', value: c.failed },
    { name: '已取消', value: c.cancelled },
  ].filter((d) => d.value > 0)
})

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
    totalTasksSample.value = tasks.length

    const todayTasksList = tasks.filter(
      (t) => t.created_at && t.created_at.startsWith(todayStr),
    )
    todayTasks.value = todayTasksList.length
    completedToday.value = todayTasksList.filter((t) => t.status === 'completed').length
    failedToday.value = todayTasksList.filter((t) => t.status === 'failed').length

    // Success rate: today-based, completed / (completed + failed)
    const finishedToday = completedToday.value + failedToday.value
    if (finishedToday > 0) {
      successRate.value = `${Math.round((completedToday.value / finishedToday) * 100)}%`
    } else {
      successRate.value = '--'
    }

    // --- Trend (last 30 days, all-task sample) ---
    buildTrend(tasks)

    // --- Status distribution (all-task sample) ---
    statusCounts.value = {
      completed: tasks.filter((t) => t.status === 'completed').length,
      failed: tasks.filter((t) => t.status === 'failed').length,
      cancelled: tasks.filter((t) => t.status === 'cancelled').length,
    }
  } catch (err) {
    console.error('Failed to load stats:', err)
  }
}

function buildTrend(tasks: Task[]) {
  const now = new Date()
  const buckets: Record<string, number> = {}
  const keys: string[] = []
  for (let i = 29; i >= 0; i--) {
    const d = new Date(now)
    d.setDate(now.getDate() - i)
    // Use a YYYY-MM-DD key (local) for grouping, matching created_at prefix.
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    buckets[key] = 0
    keys.push(key)
  }
  for (const t of tasks) {
    if (!t.created_at) continue
    const key = t.created_at.slice(0, 10)
    if (key in buckets) buckets[key]++
  }
  trendData.value = keys.map((k) => {
    const d = new Date(k + 'T00:00:00')
    return {
      date: `${d.getMonth() + 1}/${d.getDate()}`,
      count: buckets[k],
    }
  })
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

// --- Invite (mirrors studio invite card; no dedicated endpoint,
//     derived from user.invite_code) ---
const inviteCode = computed(() => authStore.user?.invite_code || '')
const inviteCountDisplay = computed(() => authStore.user?.invite_count ?? 0)
const maxInvites = computed(() => authStore.user?.max_invites ?? 3)

function copyInviteLink() {
  if (!inviteCode.value) return
  // Mini-program has no public web origin; build an H5 register deep-link.
  // The host is the server's web frontend if configured; otherwise a placeholder.
  const host = 'https://anbanwriter.com'
  const link = `${host}/register?invite=${inviteCode.value}`
  uni.setClipboardData({
    data: link,
    success: () => {
      uni.showToast({ title: '邀请链接已复制', icon: 'none' })
    },
    fail: () => {
      uni.showToast({ title: '复制失败', icon: 'none' })
    },
  })
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
      loadPricing(),
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
  uni.navigateTo({ url: '/pages/credits/index' })
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

function goTimeline() {
  uni.navigateTo({ url: '/pages/timeline/index' })
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
    display: block;
  }

  &__sub {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 6rpx;
    display: block;
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

  &__item {
    display: flex;
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

// Invite card
.invite-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg;
  padding: $ab-space-md $ab-space-lg;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__code {
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    letter-spacing: 4rpx;
    margin-top: 4rpx;
  }

  &__count {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 4rpx;
  }
}

// Charts row
.charts-row {
  display: grid;
  grid-template-columns: 1fr;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-md;
}

.chart-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-lg;
  padding: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__empty {
    height: 320rpx;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
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
  grid-template-columns: 1fr 1fr 1fr;
  gap: $ab-space-sm;

  &__item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: $ab-space-xs;
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    padding: $ab-space-md $ab-space-sm;
    box-shadow: $ab-shadow-sm;
  }

  &__icon {
    width: 72rpx;
    height: 72rpx;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 36rpx;

    &--task { background-color: $ab-primary-bg; }
    &--plan { background-color: $ab-success-bg; }
    &--timeline { background-color: $ab-info-bg; }
    &--viral { background-color: $ab-warning-bg; }
    &--poster { background-color: #F3E8FF; }
    &--credits { background-color: #FEF9C3; }
  }

  &__label {
    font-size: $ab-text-xs;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }
}

// Bottom spacer
.bottom-spacer {
  height: 120rpx;
}
</style>
