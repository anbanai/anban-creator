<template>
  <view class="billing-page">
    <view class="wallet-card">
      <view class="wallet-card__main">
        <text class="wallet-card__label">可用余额</text>
        <text class="wallet-card__balance">{{ wallet.balance.toLocaleString() }}</text>
      </view>
      <view class="wallet-card__parts">
        <view><text>现金积分</text><text>{{ wallet.paid.toLocaleString() }}</text></view>
        <view><text>奖励积分</text><text>{{ wallet.promotional.toLocaleString() }}</text></view>
        <view><text>待补欠费</text><text :class="{ debt: wallet.debt > 0 }">{{ wallet.debt.toLocaleString() }}</text></view>
      </view>
    </view>

    <view v-if="wallet.debt > 0" class="debt-notice">
      已接受的任务仍会完成。充值将优先补齐欠费，补齐前不能创建新任务。
    </view>

    <view class="actions">
      <AbButton type="primary" block @click="showRecharge = true">联系客服充值</AbButton>
      <AbButton v-if="referral?.program" type="ghost" block @click="copyInviteLink">复制邀请链接</AbButton>
    </view>

    <view v-if="referral?.program" class="referral">
      好友首次充值至少 {{ referral.program.minimum_topup_credits.toLocaleString() }} 积分后，双方各得
      {{ referral.program.invitee_credits.toLocaleString() }} 奖励积分。
    </view>

    <view class="section-title">钱包流水</view>
    <view v-if="loading" class="empty">加载中...</view>
    <view v-else-if="transactions.length === 0" class="empty">暂无钱包流水</view>
    <view v-else class="transactions">
      <view v-for="entry in transactions" :key="entry.id" class="transaction">
        <view>
          <text class="transaction__title">{{ eventLabels[entry.event_kind] }}</text>
          <text class="transaction__meta">{{ entry.resource_type || entry.source_type || entry.catalog_id || '钱包调整' }}</text>
        </view>
        <text class="transaction__delta" :class="{ positive: delta(entry) > 0, negative: delta(entry) < 0 }">
          {{ delta(entry) > 0 ? '+' : '' }}{{ delta(entry).toLocaleString() }}
        </text>
      </view>
    </view>

    <view v-if="showRecharge" class="popup-mask" @tap="showRecharge = false">
      <view class="popup" @tap.stop>
        <text class="popup__title">联系客服充值</text>
        <text class="popup__text">请向客服提供账号信息，由客服通过充值 API 入账。充值不附赠积分，欠费会优先补齐。</text>
        <AbButton type="ghost" block @click="showRecharge = false">关闭</AbButton>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { billingApi } from '@/api/billing'
import type { BillingReferral, BillingTransaction, BillingWallet, BillingWalletEventKind } from '@/types'
import AbButton from '@/components/common/AbButton.vue'

const wallet = ref<BillingWallet>({ paid: 0, promotional: 0, debt: 0, balance: 0 })
const transactions = ref<BillingTransaction[]>([])
const referral = ref<BillingReferral | null>(null)
const loading = ref(true)
const showRecharge = ref(false)

const eventLabels: Record<BillingWalletEventKind, string> = {
  topup: '充值', promotion: '推广奖励', charge: '固定价扣费', debt_created: '新增欠费',
  debt_repayment: '补缴欠费', reversal: '费用退回', expiry: '奖励到期',
}

function delta(entry: BillingTransaction) {
  return entry.paid_delta + entry.promotional_delta - entry.debt_delta
}

function copyInviteLink() {
  if (!referral.value?.invite_link) return
  uni.setClipboardData({ data: referral.value.invite_link })
}

async function load() {
  loading.value = true
  try {
    const [walletData, transactionData, referralData] = await Promise.all([
      billingApi.wallet(), billingApi.transactions({ offset: 0, limit: 30 }), billingApi.referral(),
    ])
    wallet.value = walletData
    transactions.value = transactionData.items || []
    referral.value = referralData
  } catch (err: any) {
    uni.showToast({ title: err?.message || '钱包加载失败', icon: 'none' })
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style lang="scss" scoped>
.billing-page { min-height: 100vh; padding: 28rpx; background: $ab-background; color: $ab-text; }
.wallet-card { padding: 32rpx; border-radius: $ab-radius-lg; background: $ab-surface; box-shadow: $ab-shadow-sm; }
.wallet-card__main { display: flex; flex-direction: column; gap: 8rpx; }
.wallet-card__label { color: $ab-text-secondary; font-size: $ab-text-sm; }
.wallet-card__balance { font-size: 64rpx; font-weight: 700; }
.wallet-card__parts { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16rpx; margin-top: 28rpx; }
.wallet-card__parts view { display: flex; flex-direction: column; gap: 8rpx; font-size: $ab-text-sm; color: $ab-text-secondary; }
.wallet-card__parts view text:last-child { color: $ab-text; font-weight: 600; }
.wallet-card__parts .debt { color: $ab-warning; }
.debt-notice, .referral { margin-top: 20rpx; padding: 20rpx; border-radius: $ab-radius-md; background: rgba(217, 119, 6, 0.1); font-size: $ab-text-sm; line-height: 1.6; }
.actions { display: flex; flex-direction: column; gap: 16rpx; margin-top: 24rpx; }
.section-title { margin: 36rpx 0 16rpx; font-weight: 600; }
.transactions { overflow: hidden; border-radius: $ab-radius-md; background: $ab-surface; }
.transaction { display: flex; align-items: center; justify-content: space-between; padding: 24rpx; border-bottom: 1rpx solid $ab-divider; }
.transaction__title, .transaction__meta { display: block; }
.transaction__meta { margin-top: 6rpx; color: $ab-text-secondary; font-size: $ab-text-xs; }
.transaction__delta { font-weight: 600; }
.transaction__delta.positive { color: $ab-success; }
.transaction__delta.negative { color: $ab-danger; }
.empty { padding: 60rpx 0; text-align: center; color: $ab-text-secondary; }
.popup-mask { position: fixed; inset: 0; z-index: 50; display: flex; align-items: flex-end; background: rgba(0, 0, 0, 0.45); }
.popup { width: 100%; padding: 32rpx; border-radius: 24rpx 24rpx 0 0; background: $ab-surface; text-align: center; }
.popup__title { display: block; font-weight: 600; }
.popup__text { display: block; margin: 24rpx 0; color: $ab-text-secondary; font-size: $ab-text-sm; line-height: 1.6; }
</style>
