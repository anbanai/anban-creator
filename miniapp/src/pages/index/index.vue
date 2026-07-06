<template>
  <view class="workbench">
    <view class="workbench-hero">
      <view class="workbench-hero__top">
        <view class="workbench-hero__copy">
          <text class="workbench-hero__kicker">AI 创作工作舱</text>
          <text class="workbench-hero__title">{{ greeting }}{{ nicknameSuffix }}</text>
          <text class="workbench-hero__sub">把账号、计划、任务和工坊产出收拢到一个移动控制台。</text>
        </view>
        <view class="balance-chip" @tap="goCredits">
          <text class="balance-chip__value" :class="{ 'animate-bounce': creditsAnimating }">{{ displayBalance }}</text>
          <text class="balance-chip__label">积分</text>
        </view>
      </view>

      <view class="production-strip">
        <view class="production-strip__cell">
          <text class="production-strip__label">签到</text>
          <AbButton
            v-if="!signedInToday"
            type="primary"
            size="sm"
            :loading="signInLoading"
            @click="handleSignIn"
          >
            +{{ dailySignInCredits }}
          </AbButton>
          <text v-else class="production-strip__value production-strip__value--quiet">已完成</text>
        </view>
        <view class="production-strip__cell">
          <text class="production-strip__label">今日生产</text>
          <text class="production-strip__value">{{ todayTasks }}</text>
          <text class="production-strip__hint">{{ todayTasksDesc }}</text>
        </view>
        <view class="production-strip__cell">
          <text class="production-strip__label">运行计划</text>
          <text class="production-strip__value">{{ activePlans }}</text>
          <text class="production-strip__hint">{{ activePlansDesc }}</text>
        </view>
      </view>
    </view>

    <view v-if="errorMsg" class="error-bar">
      <text class="error-bar__text">{{ errorMsg }}</text>
      <text class="error-bar__retry" @tap="loadAll">重试</text>
    </view>

    <AbLoading v-if="pageLoading" text="加载工作台..." />

    <template v-else>
      <view class="next-card" :class="`next-card--${nextSuggestion.tone}`" @tap="runSuggestion">
        <view class="next-card__rail" />
        <view class="next-card__body">
          <text class="next-card__label">下一步建议</text>
          <text class="next-card__title">{{ nextSuggestion.title }}</text>
          <text class="next-card__desc">{{ nextSuggestion.description }}</text>
        </view>
        <view class="next-card__action">
          <text>{{ nextSuggestion.cta }}</text>
          <text class="next-card__arrow">›</text>
        </view>
      </view>

      <view class="control-row">
        <view class="control-tile control-tile--primary" @tap="goCreateTask">
          <text class="control-tile__icon">＋</text>
          <text class="control-tile__label">新建任务</text>
        </view>
        <view class="control-tile" @tap="goCreatePlan">
          <text class="control-tile__icon">⟳</text>
          <text class="control-tile__label">自动计划</text>
        </view>
        <view class="control-tile" @tap="goTemplates">
          <text class="control-tile__icon">▦</text>
          <text class="control-tile__label">模板资产</text>
        </view>
      </view>

      <view class="section">
        <view class="section__header">
          <view>
            <text class="section__eyebrow">LIVE QUEUE</text>
            <text class="section__title">运行中任务</text>
          </view>
          <text class="section__more" @tap="goTasks">全部</text>
        </view>
        <view v-if="runningTasks.length > 0" class="run-list">
          <view
            v-for="task in runningTasks"
            :key="task.id"
            class="run-row"
            @tap="goTaskDetail(task.id)"
          >
            <view class="run-row__pulse" />
            <view class="run-row__body">
              <text class="run-row__title">{{ task.title || task.prompt || '未命名任务' }}</text>
              <text class="run-row__meta">{{ taskTypeLabel(task.type) }} · {{ taskProgressLabel(task) }}</text>
            </view>
            <text class="run-row__percent">{{ task.progress || task.latest_progress?.percent || 0 }}%</text>
          </view>
        </view>
        <view v-else class="soft-empty">
          <text class="soft-empty__title">当前没有执行中的任务</text>
          <text class="soft-empty__desc">可以从模板、工坊分析或账号直接发起新创作。</text>
        </view>
      </view>

      <view class="section">
        <view class="section__header">
          <view>
            <text class="section__eyebrow">RECENT OUTPUTS</text>
            <text class="section__title">最近产出</text>
          </view>
          <text class="section__more" @tap="goTasks">更多</text>
        </view>
        <view v-if="recentOutputs.length > 0" class="output-list">
          <view
            v-for="task in recentOutputs"
            :key="task.id"
            class="output-row"
            @tap="goTaskDetail(task.id)"
          >
            <view class="output-row__mark" :class="`output-row__mark--${task.type}`">
              <text>{{ taskTypeInitial(task.type) }}</text>
            </view>
            <view class="output-row__body">
              <text class="output-row__title">{{ task.title || task.prompt || '创作结果' }}</text>
              <text class="output-row__meta">{{ taskTypeLabel(task.type) }} · {{ relativeTime(task.completed_at || task.created_at) }}</text>
            </view>
            <text class="output-row__action">查看</text>
          </view>
        </view>
        <view v-else class="soft-empty">
          <text class="soft-empty__title">还没有完成产出</text>
          <text class="soft-empty__desc">完成后的文章、笔记和素材会出现在这里。</text>
        </view>
      </view>

      <view class="section">
        <view class="section__header">
          <view>
            <text class="section__eyebrow">WORKSHOP</text>
            <text class="section__title">创作工坊</text>
          </view>
          <text class="section__more" @tap="goWorkshopIndex">入口</text>
        </view>
        <view class="workshop-grid">
          <view
            v-for="entry in workshopEntries"
            :key="entry.key"
            class="workshop-tile"
            @tap="entry.action"
          >
            <view class="workshop-tile__icon" :class="`workshop-tile__icon--${entry.key}`">
              <text>{{ entry.icon }}</text>
            </view>
            <text class="workshop-tile__title">{{ entry.title }}</text>
            <text class="workshop-tile__desc">{{ entry.desc }}</text>
          </view>
        </view>
      </view>

      <view class="section">
        <view class="section__header">
          <view>
            <text class="section__eyebrow">AUTOPILOT</text>
            <text class="section__title">计划提醒</text>
          </view>
          <text class="section__more" @tap="goPlans">管理</text>
        </view>
        <view v-if="planReminders.length > 0" class="plan-list">
          <view
            v-for="plan in planReminders"
            :key="plan.id"
            class="plan-row"
            @tap="goPlanTasks(plan.id)"
          >
            <view class="plan-row__body">
              <text class="plan-row__title">{{ plan.title || plan.prompt || '自动计划' }}</text>
              <text class="plan-row__meta">{{ taskTypeLabel(plan.type) }} · 下次 {{ formatDateTimeCN(plan.next_run_at) }}</text>
            </view>
            <text class="plan-row__action">任务</text>
          </view>
        </view>
        <view v-else class="soft-empty soft-empty--compact">
          <text class="soft-empty__title">还没有启用计划</text>
          <text class="soft-empty__desc">为账号设置固定节奏，系统会自动生成内容任务。</text>
        </view>
      </view>

      <view v-if="inviteCode" class="invite-card">
        <view class="invite-card__main">
          <text class="invite-card__label">邀请资产</text>
          <text class="invite-card__code">{{ inviteCode }}</text>
          <text class="invite-card__count">已邀请 {{ inviteCountDisplay }} / {{ maxInvites }} 人</text>
        </view>
        <AbButton type="ghost" size="sm" @click="copyInviteLink">复制链接</AbButton>
      </view>
    </template>

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
import { getGreeting, relativeTime, formatDateTimeCN } from '@/utils/format'
import { contentTypeLabel } from '@/utils/labels'
import AbButton from '@/components/common/AbButton.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import type { Plan, Task } from '@/types'

const PLAN_FILTER_KEY = 'anban_task_plan_filter'

const authStore = useAuthStore()

const greeting = getGreeting()
const nicknameSuffix = computed(() => {
  const name = authStore.user?.nickname || ''
  return name ? `，${name}` : ''
})

const creditsBalance = ref(0)
const signedInToday = ref(false)
const signInLoading = ref(false)
const creditsAnimating = ref(false)
const dailySignInCredits = ref(100)

const displayBalance = computed(() => creditsBalance.value.toLocaleString())

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
    creditsBalance.value += res.reward || dailySignInCredits.value
    creditsAnimating.value = true
    setTimeout(() => {
      creditsAnimating.value = false
    }, 600)
    uni.showToast({ title: `签到成功 +${res.reward || dailySignInCredits.value}积分`, icon: 'none' })
  } catch (err: any) {
    uni.showToast({ title: err?.message || '签到失败', icon: 'none' })
  } finally {
    signInLoading.value = false
  }
}

const activePlans = ref(0)
const todayTasks = ref(0)
const completedToday = ref(0)
const failedToday = ref(0)
const allTasks = ref<Task[]>([])
const activePlanItems = ref<Plan[]>([])

const activePlansDesc = computed(() => activePlanItems.value.length > 0 ? '自动调度已接管' : '等待设置节奏')
const todayTasksDesc = computed(() => `${completedToday.value} 完成，${failedToday.value} 失败`)

const runningTasks = computed(() =>
  allTasks.value
    .filter((task) => task.status === 'running' || task.status === 'pending')
    .slice(0, 3),
)

const recentOutputs = computed(() =>
  allTasks.value
    .filter((task) => task.status === 'completed')
    .slice(0, 4),
)

const planReminders = computed(() =>
  [...activePlanItems.value]
    .sort((a, b) => new Date(a.next_run_at || '').getTime() - new Date(b.next_run_at || '').getTime())
    .slice(0, 3),
)

const nextSuggestion = computed(() => {
  if (runningTasks.value.length > 0) {
    return {
      title: '先盯住正在执行的任务',
      description: `${runningTasks.value.length} 个任务正在推进，进入执行舱查看进度、日志和可交付文件。`,
      cta: '查看队列',
      action: 'tasks',
      tone: 'live',
    }
  }
  if (recentOutputs.value.length === 0) {
    return {
      title: '创建第一条可复用产出',
      description: '选择账号后写下创作要求，系统会自动预估积分并把高级设置折叠起来。',
      cta: '新建任务',
      action: 'create-task',
      tone: 'start',
    }
  }
  if (activePlanItems.value.length === 0) {
    return {
      title: '把高频选题交给自动计划',
      description: '把稳定栏目设成计划，让内容生产从单次任务升级为持续节奏。',
      cta: '创建计划',
      action: 'create-plan',
      tone: 'plan',
    }
  }
  return {
    title: '复用最近产出继续放大',
    description: '从工坊拆解爆款、生成海报或沉淀模板，把单次结果变成下一轮任务。',
    cta: '进入工坊',
    action: 'workshop',
    tone: 'reuse',
  }
})

const workshopEntries = [
  {
    key: 'viral',
    icon: '拆',
    title: '爆文拆解',
    desc: '分析链接后直接复刻任务',
    action: () => goWorkshop('viral-analysis'),
  },
  {
    key: 'poster',
    icon: '图',
    title: '海报制作',
    desc: '生成后继续变体',
    action: () => goWorkshop('poster'),
  },
  {
    key: 'designer',
    icon: '设',
    title: '设计师',
    desc: '参考图与局部编辑',
    action: () => uni.navigateTo({ url: '/pages/designer/index' }),
  },
  {
    key: 'template',
    icon: '模',
    title: '模板中心',
    desc: '沉淀为可操作资产',
    action: () => goTemplates(),
  },
]

async function loadStats() {
  try {
    const [plansRes, tasksRes] = await Promise.all([
      plansApi.list(),
      tasksApi.list({ limit: 100 }),
    ])

    const plans = plansRes.items || []
    activePlanItems.value = plans.filter((plan) => plan.status === 'active')
    activePlans.value = activePlanItems.value.length

    const tasks = tasksRes.items || []
    allTasks.value = tasks

    const today = new Date()
    const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
    const todayTasksList = tasks.filter((task) => task.created_at && task.created_at.startsWith(todayStr))
    todayTasks.value = todayTasksList.length
    completedToday.value = todayTasksList.filter((task) => task.status === 'completed').length
    failedToday.value = todayTasksList.filter((task) => task.status === 'failed').length
  } catch (err) {
    console.error('Failed to load stats:', err)
    errorMsg.value = '工作台数据加载失败'
  }
}

const inviteCode = computed(() => authStore.user?.invite_code || '')
const inviteCountDisplay = computed(() => authStore.user?.invite_count ?? 0)
const maxInvites = computed(() => authStore.user?.max_invites ?? 3)

function copyInviteLink() {
  if (!inviteCode.value) return
  const host = 'https://anban-creator.com'
  const link = `${host}/register?invite=${inviteCode.value}`
  uni.setClipboardData({
    data: link,
    success: () => uni.showToast({ title: '邀请链接已复制', icon: 'none' }),
    fail: () => uni.showToast({ title: '复制失败', icon: 'none' }),
  })
}

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
    ])
  } finally {
    pageLoading.value = false
  }
}

onMounted(loadAll)

onPullDownRefresh(async () => {
  try {
    await Promise.all([
      loadCredits(),
      loadStats(),
    ])
  } finally {
    uni.stopPullDownRefresh()
  }
})

function taskTypeLabel(type: string): string {
  return contentTypeLabel[type] || type
}

function taskTypeInitial(type: string): string {
  const label = taskTypeLabel(type)
  return label.slice(0, 1)
}

function taskProgressLabel(task: Task): string {
  if (task.latest_progress?.title) return task.latest_progress.title
  if (task.status === 'pending') return '等待执行'
  return '实时执行中'
}

function runSuggestion() {
  switch (nextSuggestion.value.action) {
    case 'tasks':
      goTasks()
      break
    case 'create-plan':
      goCreatePlan()
      break
    case 'workshop':
      goWorkshopIndex()
      break
    default:
      goCreateTask()
  }
}

function goCredits() {
  uni.navigateTo({ url: '/pages/credits/index' })
}

function goTasks() {
  uni.switchTab({ url: '/pages/tasks/index' })
}

function goPlans() {
  uni.switchTab({ url: '/pages/plans/index' })
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

function goTemplates() {
  uni.navigateTo({ url: '/pages/templates/index' })
}

function goWorkshopIndex() {
  uni.navigateTo({ url: '/pages/workshop/index' })
}

function goWorkshop(page: string) {
  uni.navigateTo({ url: `/pages/workshop/${page}` })
}

function goPlanTasks(planId: string) {
  uni.setStorageSync(PLAN_FILTER_KEY, planId)
  uni.switchTab({ url: '/pages/tasks/index' })
}
</script>

<style lang="scss" scoped>
.workbench {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

.workbench-hero {
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-lg;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__top {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: $ab-space-md;
    margin-bottom: $ab-space-md;
  }

  &__copy {
    flex: 1;
    min-width: 0;
  }

  &__kicker {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-primary;
    font-weight: $ab-font-semibold;
    margin-bottom: 6rpx;
  }

  &__title {
    display: block;
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    line-height: 1.2;
  }

  &__sub {
    display: block;
    margin-top: 8rpx;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.5;
  }
}

.balance-chip {
  flex-shrink: 0;
  min-width: 140rpx;
  padding: $ab-space-sm;
  border-radius: $ab-radius-md;
  background-color: $ab-primary-bg;
  text-align: right;

  &__value {
    display: block;
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-primary;
    line-height: 1.1;
  }

  &__label {
    display: block;
    margin-top: 4rpx;
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
  }
}

.production-strip {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  border-top: 2rpx solid $ab-divider;
  padding-top: $ab-space-sm;
  gap: $ab-space-sm;

  &__cell {
    min-width: 0;
  }

  &__label,
  &__hint {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    line-height: 1.35;
  }

  &__value {
    display: block;
    margin-top: 4rpx;
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;

    &--quiet {
      font-size: $ab-text-sm;
      color: $ab-success;
    }
  }
}

.animate-bounce {
  animation: credits-bounce 0.5s ease;
}

@keyframes credits-bounce {
  0% { transform: scale(1); }
  30% { transform: scale(1.2); }
  60% { transform: scale(0.96); }
  100% { transform: scale(1); }
}

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
    margin-left: $ab-space-md;
  }
}

.next-card {
  position: relative;
  display: flex;
  align-items: stretch;
  gap: $ab-space-md;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-lg;
  padding: $ab-space-md;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-sm;
  overflow: hidden;

  &__rail {
    width: 8rpx;
    border-radius: $ab-radius-full;
    background-color: $ab-primary;
    flex-shrink: 0;
  }

  &--live &__rail { background-color: $ab-warning; }
  &--plan &__rail { background-color: $ab-info; }
  &--reuse &__rail { background-color: $ab-success; }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__label {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-bottom: 6rpx;
  }

  &__title {
    display: block;
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    line-height: 1.35;
  }

  &__desc {
    display: block;
    margin-top: 6rpx;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.5;
  }

  &__action {
    display: flex;
    align-items: center;
    gap: 4rpx;
    align-self: center;
    flex-shrink: 0;
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-semibold;
  }

  &__arrow {
    font-size: $ab-text-lg;
    line-height: 1;
  }
}

.control-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: $ab-space-sm;
  margin-bottom: $ab-space-lg;
}

.control-tile {
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-md $ab-space-sm;
  text-align: center;

  &--primary {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__icon {
    display: block;
    font-size: $ab-text-xl;
    color: $ab-primary;
    line-height: 1;
    margin-bottom: $ab-space-xs;
  }

  &__label {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }
}

.section {
  margin-bottom: $ab-space-lg;

  &__header {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
  }

  &__eyebrow {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-bottom: 4rpx;
  }

  &__title {
    display: block;
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__more {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}

.run-list,
.output-list,
.plan-list {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
}

.run-row,
.output-row,
.plan-row {
  display: flex;
  align-items: center;
  gap: $ab-space-sm;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
}

.run-row {
  &__pulse {
    width: 18rpx;
    height: 18rpx;
    border-radius: 50%;
    background-color: $ab-warning;
    box-shadow: 0 0 0 8rpx $ab-warning-bg;
    flex-shrink: 0;
  }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__title,
  &__meta {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__title {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__meta {
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__percent {
    flex-shrink: 0;
    font-size: $ab-text-sm;
    color: $ab-warning;
    font-weight: $ab-font-semibold;
  }
}

.output-row {
  &__mark {
    width: 64rpx;
    height: 64rpx;
    border-radius: $ab-radius-sm;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    background-color: $ab-primary-bg;
    color: $ab-primary;
    font-size: $ab-text-sm;
    font-weight: $ab-font-semibold;

    &--seednote { background-color: $ab-danger-bg; color: $ab-danger; }
    &--article { background-color: $ab-success-bg; color: $ab-success; }
    &--ecommerce { background-color: $ab-warning-bg; color: $ab-warning; }
  }

  &__body {
    flex: 1;
    min-width: 0;
  }

  &__title,
  &__meta {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__title {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__meta {
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__action {
    flex-shrink: 0;
    font-size: $ab-text-sm;
    color: $ab-primary;
  }
}

.workshop-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: $ab-space-sm;
}

.workshop-tile {
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;

  &__icon {
    width: 56rpx;
    height: 56rpx;
    border-radius: $ab-radius-sm;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: $ab-text-sm;
    font-weight: $ab-font-semibold;
    margin-bottom: $ab-space-sm;
    background-color: $ab-primary-bg;
    color: $ab-primary;

    &--viral { background-color: $ab-danger-bg; color: $ab-danger; }
    &--poster { background-color: $ab-warning-bg; color: $ab-warning; }
    &--designer { background-color: $ab-info-bg; color: $ab-info; }
    &--template { background-color: $ab-success-bg; color: $ab-success; }
  }

  &__title {
    display: block;
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-semibold;
  }

  &__desc {
    display: block;
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    line-height: 1.45;
  }
}

.plan-row {
  &__body {
    flex: 1;
    min-width: 0;
  }

  &__title,
  &__meta {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__title {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__meta {
    margin-top: 6rpx;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__action {
    flex-shrink: 0;
    font-size: $ab-text-sm;
    color: $ab-primary;
  }
}

.soft-empty {
  background-color: $ab-surface;
  border: 2rpx dashed $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-lg $ab-space-md;

  &--compact {
    padding: $ab-space-md;
  }

  &__title {
    display: block;
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
  }

  &__desc {
    display: block;
    margin-top: 6rpx;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.45;
  }
}

.invite-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-lg;

  &__main {
    flex: 1;
    min-width: 0;
  }

  &__label,
  &__count {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__code {
    display: block;
    margin: 4rpx 0;
    font-size: $ab-text-lg;
    color: $ab-text;
    font-weight: $ab-font-semibold;
    letter-spacing: 2rpx;
  }
}

.bottom-spacer {
  height: 120rpx;
}
</style>
