<template>
  <view class="plans-page">
    <!-- Loading -->
    <AbLoading v-if="loading && plans.length === 0" text="加载中..." />

    <!-- Error bar -->
    <view v-if="errorMsg && !loading" class="error-bar">
      <text class="error-bar__text">{{ errorMsg }}</text>
      <text class="error-bar__retry" @tap="loadPlans">重试</text>
    </view>

    <!-- Plan list -->
    <view v-if="plans.length > 0" class="plans-list">
      <PlanCard
        v-for="plan in plans"
        :key="plan.id"
        :plan="plan"
        @tap="onPlanTap(plan)"
        @pause="onPause(plan)"
        @resume="onResume(plan)"
        @edit="onEdit(plan)"
      />
    </view>

    <!-- Empty state -->
    <AbEmpty
      v-if="!loading && plans.length === 0 && !errorMsg"
      title="还没有计划"
      description="创建定时计划，自动执行创作任务"
      action-text="新建计划"
      @action="goCreate"
    />

    <!-- No more indicator -->
    <view v-if="!hasMore && plans.length > 0" class="no-more">
      <text class="no-more__text">-- 已经到底了 --</text>
    </view>

    <!-- Load more spinner -->
    <view v-if="loading && plans.length > 0" class="load-more">
      <AbLoading size="sm" />
    </view>

    <!-- Fixed bottom button -->
    <view class="bottom-bar" v-if="plans.length > 0">
      <AbButton type="primary" block @click="goCreate">
        ＋ 新建计划
      </AbButton>
    </view>

    <!-- Bottom spacer for tab bar -->
    <view class="bottom-spacer" />
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import { plansApi } from '@/api/plans'
import PlanCard from '@/components/business/PlanCard.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbLoading from '@/components/common/AbLoading.vue'
import type { Plan } from '@/types'

const plans = ref<Plan[]>([])
const loading = ref(false)
const hasMore = ref(true)
const offset = ref(0)
const pageSize = 20
const errorMsg = ref('')

async function fetchPlans(reset = false) {
  if (reset) {
    offset.value = 0
    hasMore.value = true
  }

  if (!hasMore.value && !reset) return

  if (reset) {
    errorMsg.value = ''
  } else {
    loading.value = true
  }

  if (reset) loading.value = true

  try {
    const res = await plansApi.list()
    const newItems = res.items || []

    if (reset) {
      plans.value = newItems
    } else {
      plans.value = [...plans.value, ...newItems]
    }

    offset.value += newItems.length
    hasMore.value = newItems.length >= pageSize
  } catch (err) {
    console.error('Failed to load plans:', err)
    if (reset) {
      errorMsg.value = '加载失败'
      plans.value = []
    }
  } finally {
    loading.value = false
  }
}

async function loadPlans() {
  await fetchPlans(true)
}

// Pull-down refresh
onPullDownRefresh(async () => {
  await fetchPlans(true)
  uni.stopPullDownRefresh()
})

// Reach bottom for pagination
onReachBottom(async () => {
  if (loading.value || !hasMore.value) return
  await fetchPlans(false)
})

// --- Event handlers ---

function onPlanTap(plan: Plan) {
  // Navigate to tasks list filtered by plan_id
  uni.navigateTo({ url: `/pages/tasks/index?plan_id=${plan.id}` })
}

function onPause(plan: Plan) {
  uni.showModal({
    title: '暂停计划',
    content: `确定要暂停「${plan.title || '未命名计划'}」吗？`,
    success: async (res) => {
      if (!res.confirm) return
      try {
        await plansApi.pause(plan.id)
        // Update local state
        const target = plans.value.find((p) => p.id === plan.id)
        if (target) {
          target.status = 'paused'
        }
        uni.showToast({ title: '已暂停', icon: 'none' })
      } catch (err: any) {
        const msg = err?.message || '暂停失败'
        uni.showToast({ title: msg, icon: 'none' })
      }
    },
  })
}

function onResume(plan: Plan) {
  try {
    plansApi.resume(plan.id)
    const target = plans.value.find((p) => p.id === plan.id)
    if (target) {
      target.status = 'active'
    }
    uni.showToast({ title: '已恢复', icon: 'none' })
  } catch (err: any) {
    const msg = err?.message || '恢复失败'
    uni.showToast({ title: msg, icon: 'none' })
  }
}

function onEdit(plan: Plan) {
  uni.navigateTo({ url: `/pages/plans/create?id=${plan.id}` })
}

function goCreate() {
  uni.navigateTo({ url: '/pages/plans/create' })
}
</script>

<style lang="scss" scoped>
.plans-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

.plans-list {
  padding-top: $ab-space-xs;
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

// No more
.no-more {
  text-align: center;
  padding: $ab-space-lg 0;

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Load more
.load-more {
  display: flex;
  justify-content: center;
  padding: $ab-space-md 0;
}

// Bottom bar
.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-sm $ab-space-md;
  padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom, 0px));
  background-color: $ab-surface;
  box-shadow: $ab-shadow-md;
  z-index: 10;
}

// Bottom spacer for tab bar
.bottom-spacer {
  height: 140rpx;
}
</style>
