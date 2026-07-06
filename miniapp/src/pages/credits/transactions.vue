<template>
  <view class="transactions-page">
    <!-- Balance Header -->
    <view class="balance-header">
      <text class="balance-header__label">当前余额</text>
      <text class="balance-header__value">{{ displayBalance }}</text>
      <text class="balance-header__unit">积分</text>
    </view>

    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <!-- Empty State -->
    <AbEmpty
      v-else-if="!pageLoading && groupedTransactions.length === 0"
      title="暂无积分记录"
      description="完成签到或创建任务即可获得积分"
    />

    <!-- Transaction List -->
    <view v-else class="transaction-list">
      <view
        v-for="group in groupedTransactions"
        :key="group.date"
        class="transaction-group"
      >
        <text class="transaction-group__date">{{ group.label }}</text>
        <view class="transaction-group__items">
          <view
            v-for="item in group.items"
            :key="item.id"
            class="transaction-item"
          >
            <view class="transaction-item__left">
              <view class="transaction-item__type-badge" :class="getTypeBadgeClass(item.type)">
                <text class="transaction-item__type-icon">{{ getTypeIcon(item.type) }}</text>
              </view>
              <view class="transaction-item__detail">
                <text class="transaction-item__type-label">{{ getTypeLabel(item.type) }}</text>
                <text class="transaction-item__desc">{{ item.description }}</text>
              </view>
            </view>
            <view class="transaction-item__right">
              <text
                class="transaction-item__amount"
                :class="{ 'transaction-item__amount--income': item.amount > 0, 'transaction-item__amount--expense': item.amount < 0 }"
              >
                {{ item.amount > 0 ? '+' : '' }}{{ item.amount.toLocaleString() }}
              </text>
              <text class="transaction-item__balance">余额 {{ item.balance_after.toLocaleString() }}</text>
              <text class="transaction-item__time">{{ formatTime(item.created_at) }}</text>
            </view>
          </view>
        </view>
      </view>

      <!-- Load More -->
      <view v-if="hasMore && !loadingMore" class="load-more" @tap="loadMore">
        <text class="load-more__text">加载更多</text>
      </view>
      <AbLoading v-if="loadingMore" size="sm" text="加载中..." />
      <view v-if="!hasMore && transactions.length > 0" class="load-more">
        <text class="load-more__text load-more__text--end">已经到底了</text>
      </view>
    </view>

    <!-- Bottom spacer -->
    <view class="bottom-spacer" />
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { onPullDownRefresh, onReachBottom } from '@dcloudio/uni-app'
import { creditsApi } from '@/api/credits'
import { transactionTypeLabel } from '@/utils/labels'
import { formatDateTimeCN, formatDateLabelCN } from '@/utils/format'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import type { CreditTransaction } from '@/types'

// --- Balance ---
const balance = ref(0)

const displayBalance = computed(() => {
  return balance.value.toLocaleString()
})

async function loadBalance() {
  try {
    const res = await creditsApi.balance()
    balance.value = res.balance
  } catch {
    // Silent
  }
}

// --- Transactions ---
const PAGE_SIZE = 20
const transactions = ref<CreditTransaction[]>([])
const offset = ref(0)
const hasMore = ref(true)
const pageLoading = ref(true)
const loadingMore = ref(false)

interface TransactionGroup {
  date: string
  label: string
  items: CreditTransaction[]
}

const groupedTransactions = computed<TransactionGroup[]>(() => {
  const groups: TransactionGroup[] = []
  let currentGroup: TransactionGroup | null = null

  for (const tx of transactions.value) {
    const dateStr = tx.created_at ? tx.created_at.slice(0, 10) : 'unknown'
    if (!currentGroup || currentGroup.date !== dateStr) {
      currentGroup = {
        date: dateStr,
        label: tx.created_at ? formatDateLabelCN(dateStr) : '未知',
        items: [],
      }
      groups.push(currentGroup)
    }
    currentGroup.items.push(tx)
  }

  return groups
})

async function loadTransactions(reset = false) {
  if (reset) {
    offset.value = 0
    hasMore.value = true
  }

  try {
    const res = await creditsApi.transactions({
      limit: PAGE_SIZE,
      offset: offset.value,
    })

    const items = res.items || []
    if (reset) {
      transactions.value = items
    } else {
      transactions.value = [...transactions.value, ...items]
    }

    offset.value += items.length
    hasMore.value = items.length >= PAGE_SIZE
  } catch {
    uni.showToast({ title: '加载失败', icon: 'none' })
  }
}

async function loadMore() {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    await loadTransactions(false)
  } finally {
    loadingMore.value = false
  }
}

// --- Initial load ---
async function loadAll() {
  pageLoading.value = true
  try {
    await Promise.all([loadBalance(), loadTransactions(true)])
  } finally {
    pageLoading.value = false
  }
}

onMounted(loadAll)

// --- Pull-down refresh ---
onPullDownRefresh(async () => {
  try {
    await Promise.all([loadBalance(), loadTransactions(true)])
  } finally {
    uni.stopPullDownRefresh()
  }
})

// --- Reach bottom ---
onReachBottom(() => {
  loadMore()
})

// --- Helpers ---
function getTypeLabel(type: string): string {
  return transactionTypeLabel[type as keyof typeof transactionTypeLabel] || type
}

function getTypeIcon(type: string): string {
  switch (type) {
    case 'sign_in': return '签'
    case 'task_deduct': return '任'
    case 'task_refund': return '返'
    case 'admin_grant': return '赠'
    case 'register_bonus': return '注'
    case 'invite_reward': return '邀'
    case 'image_gen': return '图'
    case 'image_understanding': return '识'
    case 'image_upload': return '传'
    case 'article_write': return '文'
    case 'convert': return '转'
    case 'humanize': return '润'
    case 'topic_research': return '搜'
    case 'seo': return '势'
    case 'draft_publish': return '发'
    case 'outline': return '纲'
    case 'viral_analysis': return '析'
    case 'video_gen': return '视'
    case 'video_understanding': return '理'
    case 'poster_generation': return '海'
    default: return '分'
  }
}

function getTypeBadgeClass(type: string): string {
  const incomeTypes = ['sign_in', 'task_refund', 'admin_grant', 'register_bonus', 'invite_reward']
  return incomeTypes.includes(type) ? 'transaction-item__type-badge--income' : 'transaction-item__type-badge--expense'
}

function formatTime(dateStr: string): string {
  return formatDateTimeCN(dateStr)
}
</script>

<style lang="scss" scoped>
.transactions-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: 0 $ab-space-md;
  box-sizing: border-box;
}

// Balance header
.balance-header {
  display: flex;
  align-items: baseline;
  justify-content: center;
  gap: $ab-space-xs;
  padding: $ab-space-lg 0 $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;
  margin-bottom: $ab-space-md;

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-right: $ab-space-xs;
  }

  &__value {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: $ab-primary;
  }

  &__unit {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }
}

// Transaction list
.transaction-list {
  padding-bottom: $ab-space-md;
}

.transaction-group {
  margin-bottom: $ab-space-md;

  &__date {
    display: block;
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text-tertiary;
    padding: $ab-space-xs 0;
  }

  &__items {
    background-color: $ab-surface;
    border-radius: $ab-radius-md;
    box-shadow: $ab-shadow-sm;
    overflow: hidden;
  }
}

// Transaction item
.transaction-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__left {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    flex: 1;
    min-width: 0;
  }

  &__type-badge {
    width: 72rpx;
    height: 72rpx;
    border-radius: $ab-radius-md;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;

    &--income {
      background-color: $ab-success-bg;
    }

    &--expense {
      background-color: $ab-danger-bg;
    }
  }

  &__type-icon {
    font-size: 32rpx;
  }

  &__detail {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4rpx;
  }

  &__type-label {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__desc {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__right {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    flex-shrink: 0;
    margin-left: $ab-space-sm;
  }

  &__amount {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;

    &--income {
      color: $ab-success;
    }

    &--expense {
      color: $ab-danger;
    }
  }

  &__balance {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 2rpx;
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-top: 2rpx;
  }
}

// Load more
.load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md 0;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;

    &--end {
      color: $ab-text-tertiary;
    }
  }
}

// Bottom spacer
.bottom-spacer {
  height: 60rpx;
}
</style>
