<template>
  <view class="me-page">
    <!-- User Card -->
    <view class="user-card" @tap="goSettings">
      <image
        v-if="authStore.user?.avatar"
        class="user-card__avatar"
        :src="authStore.user.avatar"
        mode="aspectFill"
      />
      <view v-else class="user-card__avatar user-card__avatar--empty">
        <text class="user-card__avatar-text">{{ avatarLetter }}</text>
      </view>
      <view class="user-card__info">
        <text class="user-card__name">{{ authStore.user?.nickname || '未登录' }}</text>
        <AbBadge :variant="tierBadgeVariant" size="sm">{{ tierLabel }}</AbBadge>
      </view>
      <text class="user-card__arrow">&#8250;</text>
    </view>

    <!-- Credits Balance Card -->
    <view class="balance-card" @tap="goCredits">
      <view class="balance-card__left">
        <text class="balance-card__icon">&#128176;</text>
        <text class="balance-card__value">{{ displayBalance }}</text>
        <text class="balance-card__unit">积分</text>
      </view>
      <view class="balance-card__right" @tap.stop="showRecharge = true">
        <text class="balance-card__recharge">充值</text>
        <text class="balance-card__recharge-arrow">&#8250;</text>
      </view>
    </view>

    <!-- Section: Creative Tools -->
    <view class="section">
      <text class="section__title">创作工具</text>
      <AbCard :padding="0">
        <view
          v-for="item in creativeTools"
          :key="item.path"
          class="list-item"
          @tap="navigateTo(item.path)"
        >
          <text class="list-item__icon">{{ item.icon }}</text>
          <text class="list-item__title">{{ item.title }}</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
      </AbCard>
    </view>

    <!-- Section: Data Center -->
    <view class="section">
      <text class="section__title">数据中心</text>
      <AbCard :padding="0">
        <view
          v-for="item in dataItems"
          :key="item.path"
          class="list-item"
          @tap="navigateTo(item.path)"
        >
          <text class="list-item__icon">{{ item.icon }}</text>
          <text class="list-item__title">{{ item.title }}</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
      </AbCard>
    </view>

    <!-- Section: Other -->
    <view class="section">
      <text class="section__title">其他</text>
      <AbCard :padding="0">
        <view
          v-for="item in otherItems"
          :key="item.title"
          class="list-item"
          @tap="item.action?.()"
        >
          <text class="list-item__icon">{{ item.icon }}</text>
          <text class="list-item__title">{{ item.title }}</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
      </AbCard>
    </view>

    <!-- Bottom spacer for tab bar -->
    <view class="bottom-spacer" />

    <!-- Recharge Popup -->
    <view v-if="showRecharge" class="popup-mask" @tap="showRecharge = false">
      <view class="popup-sheet" @tap.stop>
        <view class="popup-sheet__header">
          <text class="popup-sheet__title">充值积分</text>
          <text class="popup-sheet__close" @tap="showRecharge = false">&#10005;</text>
        </view>

        <view class="recharge-tiers">
          <view
            v-for="tier in rechargeTiers"
            :key="tier.price"
            class="recharge-tier"
            :class="{ 'recharge-tier--active': selectedTier === tier.price }"
            @tap="selectedTier = tier.price"
          >
            <text class="recharge-tier__price">{{ tier.price }}元</text>
            <text class="recharge-tier__credits">{{ tier.credits.toLocaleString() }} 积分</text>
            <text v-if="tier.bonus" class="recharge-tier__bonus">多送{{ tier.bonus.toLocaleString() }}</text>
          </view>
        </view>

        <view class="recharge-divider">
          <view class="recharge-divider__line" />
          <text class="recharge-divider__text">或联系客服充值</text>
          <view class="recharge-divider__line" />
        </view>

        <view class="recharge-qr">
          <view class="recharge-qr__placeholder">
            <text class="recharge-qr__icon">&#128247;</text>
            <text class="recharge-qr__label">客服二维码</text>
            <text class="recharge-qr__hint">长按识别添加客服</text>
          </view>
        </view>

        <view class="recharge-footer">
          <AbButton type="primary" size="lg" block :disabled="!selectedTier" @click="handleRecharge">
            确认充值
          </AbButton>
        </view>
      </view>
    </view>

    <!-- Feedback Popup -->
    <view v-if="showFeedback" class="popup-mask" @tap="showFeedback = false">
      <view class="popup-sheet" @tap.stop>
        <view class="popup-sheet__header">
          <text class="popup-sheet__title">意见反馈</text>
          <text class="popup-sheet__close" @tap="showFeedback = false">&#10005;</text>
        </view>

        <view class="feedback-form">
          <view class="feedback-form__field">
            <text class="feedback-form__label">类型</text>
            <view class="feedback-form__types">
              <view
                class="feedback-type"
                :class="{ 'feedback-type--active': feedbackType === 'bug' }"
                @tap="feedbackType = 'bug'"
              >
                <text>&#128027; Bug</text>
              </view>
              <view
                class="feedback-type"
                :class="{ 'feedback-type--active': feedbackType === 'suggestion' }"
                @tap="feedbackType = 'suggestion'"
              >
                <text>&#128161; 建议</text>
              </view>
            </view>
          </view>

          <view class="feedback-form__field">
            <text class="feedback-form__label">内容</text>
            <AbTextarea
              v-model="feedbackContent"
              placeholder="描述你遇到的问题..."
              :maxlength="1000"
              :rows="4"
            />
          </view>

          <AbButton
            type="primary"
            size="lg"
            block
            :loading="feedbackLoading"
            :disabled="!feedbackContent.trim()"
            @click="submitFeedback"
          >
            提交反馈
          </AbButton>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { onShareAppMessage } from '@dcloudio/uni-app'
import { useAuthStore } from '@/stores/auth'
import { creditsApi } from '@/api/credits'
import { post } from '@/api/index'
import { tierLabels } from '@/utils/labels'
import AbBadge from '@/components/common/AbBadge.vue'
import AbCard from '@/components/common/AbCard.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'

const authStore = useAuthStore()

// --- User info ---
const avatarLetter = computed(() => {
  const name = authStore.user?.nickname || ''
  return name ? name.charAt(0).toUpperCase() : '?'
})

const tierLabel = computed(() => {
  return tierLabels[authStore.user?.tier || 'free'] || '免费版'
})

const tierBadgeVariant = computed(() => {
  switch (authStore.user?.tier) {
    case 'pro': return 'info'
    case 'enterprise': return 'warning'
    default: return 'neutral'
  }
})

// --- Credits ---
const creditsBalance = ref(0)

const displayBalance = computed(() => {
  return creditsBalance.value.toLocaleString()
})

async function loadCredits() {
  try {
    const res = await creditsApi.balance()
    creditsBalance.value = res.balance
  } catch {
    // Silent fail on Me page
  }
}

// --- Navigation menus ---
interface MenuItem {
  icon: string
  title: string
  path?: string
  action?: () => void
}

const creativeTools: MenuItem[] = [
  { icon: '🔬', title: '爆文拆解', path: '/pages/workshop/index?tab=viral-analysis' },
  { icon: '🎨', title: '海报制作', path: '/pages/workshop/index?tab=poster' },
  { icon: '📋', title: '爆款复刻', path: '/pages/workshop/index?tab=clone' },
  { icon: '📑', title: '模板库', path: '/pages/templates/index' },
]

const dataItems: MenuItem[] = [
  { icon: '📊', title: '时间轴', path: '/pages/timeline/index' },
  { icon: '📈', title: '用量统计', path: '/pages/usage/index' },
  { icon: '💳', title: '积分明细', path: '/pages/credits/transactions' },
]

const showFeedback = ref(false)
const showRecharge = ref(false)

const otherItems: MenuItem[] = [
  { icon: '⚙️', title: '设置', path: '/pages/settings/index' },
  { icon: '💬', title: '意见反馈', action: () => { showFeedback.value = true } },
  { icon: '📤', title: '分享给好友', action: () => { /* handled by onShareAppMessage */ } },
]

function navigateTo(path: string) {
  uni.navigateTo({ url: path })
}

function goSettings() {
  uni.navigateTo({ url: '/pages/settings/index' })
}

function goCredits() {
  uni.navigateTo({ url: '/pages/credits/transactions' })
}

// --- Share ---
onShareAppMessage(() => {
  const inviteCode = authStore.user?.invite_code || ''
  return {
    title: 'Anban 智能创作助手 - AI 驱动的内容创作平台',
    path: `/pages/index/index?invite=${inviteCode}`,
  }
})

// --- Recharge ---
interface RechargeTier {
  price: number
  credits: number
  bonus?: number
}

const rechargeTiers: RechargeTier[] = [
  { price: 10, credits: 10000 },
  { price: 50, credits: 55000, bonus: 5000 },
  { price: 100, credits: 120000, bonus: 20000 },
]

const selectedTier = ref<number | null>(null)

function handleRecharge() {
  if (!selectedTier.value) return
  const tier = rechargeTiers.find(t => t.price === selectedTier.value)
  if (!tier) return
  // Recharge is handled via customer service QR code
  uni.showToast({ title: '请联系客服完成充值', icon: 'none' })
}

// --- Feedback ---
const feedbackType = ref<'bug' | 'suggestion'>('bug')
const feedbackContent = ref('')
const feedbackLoading = ref(false)

async function submitFeedback() {
  if (!feedbackContent.value.trim() || feedbackLoading.value) return
  feedbackLoading.value = true
  try {
    await post('/feedback', {
      type: feedbackType.value,
      content: feedbackContent.value.trim(),
    })
    uni.showToast({ title: '感谢您的反馈！', icon: 'success' })
    showFeedback.value = false
    feedbackContent.value = ''
    feedbackType.value = 'bug'
  } catch (err: any) {
    const msg = err?.message || '提交失败，请重试'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    feedbackLoading.value = false
  }
}

// --- Lifecycle ---
onMounted(loadCredits)
</script>

<style lang="scss" scoped>
.me-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

// User card
.user-card {
  display: flex;
  align-items: center;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg;
  padding: $ab-space-lg;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;

  &__avatar {
    width: 128rpx;
    height: 128rpx;
    border-radius: 50%;
    flex-shrink: 0;
    background-color: $ab-primary-bg;

    &--empty {
      display: flex;
      align-items: center;
      justify-content: center;
    }
  }

  &__avatar-text {
    font-size: 48rpx;
    font-weight: $ab-font-semibold;
    color: $ab-primary;
  }

  &__info {
    flex: 1;
    margin-left: $ab-space-md;
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
  }

  &__name {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__arrow {
    font-size: 40rpx;
    color: $ab-text-tertiary;
    flex-shrink: 0;
  }
}

// Balance card
.balance-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md $ab-space-lg;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-sm;

  &__left {
    display: flex;
    align-items: baseline;
    gap: $ab-space-xs;
  }

  &__icon {
    font-size: $ab-text-lg;
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

  &__right {
    display: flex;
    align-items: center;
    gap: 4rpx;
  }

  &__recharge {
    font-size: $ab-text-base;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }

  &__recharge-arrow {
    font-size: 32rpx;
    color: $ab-primary;
  }
}

// Section
.section {
  margin-bottom: $ab-space-md;

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text-secondary;
    margin-bottom: $ab-space-sm;
    padding-left: $ab-space-xs;
  }
}

// List items (inside AbCard with padding=0)
.list-item {
  display: flex;
  align-items: center;
  padding: $ab-space-md $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__icon {
    font-size: 40rpx;
    width: 48rpx;
    flex-shrink: 0;
  }

  &__title {
    flex: 1;
    font-size: $ab-text-base;
    color: $ab-text;
    margin-left: $ab-space-sm;
  }

  &__arrow {
    font-size: 36rpx;
    color: $ab-text-tertiary;
    flex-shrink: 0;
  }
}

// Bottom spacer
.bottom-spacer {
  height: 120rpx;
}

// Popup mask + sheet
.popup-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 999;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.popup-sheet {
  width: 100%;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg $ab-radius-lg 0 0;
  padding: $ab-space-md;
  padding-bottom: calc($ab-space-md + env(safe-area-inset-bottom));
  max-height: 80vh;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-lg;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__close {
    font-size: 36rpx;
    color: $ab-text-tertiary;
    padding: $ab-space-xs;
  }
}

// Recharge tiers
.recharge-tiers {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-lg;
}

.recharge-tier {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md $ab-space-sm;
  border: 4rpx solid $ab-border;
  border-radius: $ab-radius-md;
  background-color: $ab-surface;
  transition: all 0.2s ease;
  position: relative;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__price {
    font-size: $ab-text-md;
    font-weight: $ab-font-bold;
    color: $ab-text;
    margin-bottom: 4rpx;
  }

  &__credits {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: 4rpx;
  }

  &__bonus {
    font-size: $ab-text-xs;
    color: $ab-danger;
    background-color: $ab-danger-bg;
    padding: 2rpx 12rpx;
    border-radius: $ab-radius-full;
    position: absolute;
    top: -2rpx;
    right: -2rpx;
  }
}

.recharge-divider {
  display: flex;
  align-items: center;
  gap: $ab-space-md;
  margin-bottom: $ab-space-lg;

  &__line {
    flex: 1;
    height: 2rpx;
    background-color: $ab-divider;
  }

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    white-space: nowrap;
  }
}

.recharge-qr {
  margin-bottom: $ab-space-lg;

  &__placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    width: 320rpx;
    height: 320rpx;
    margin: 0 auto;
    border: 4rpx dashed $ab-border;
    border-radius: $ab-radius-md;
    background-color: $ab-background;
  }

  &__icon {
    font-size: 80rpx;
    margin-bottom: $ab-space-xs;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: 4rpx;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.recharge-footer {
  padding-top: $ab-space-sm;
}

// Feedback form
.feedback-form {
  display: flex;
  flex-direction: column;
  gap: $ab-space-md;

  &__field {
    display: flex;
    flex-direction: column;
    gap: $ab-space-sm;
  }

  &__label {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__types {
    display: flex;
    gap: $ab-space-sm;
  }
}

.feedback-type {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: $ab-space-sm $ab-space-md;
  border: 4rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  font-size: $ab-text-base;
  color: $ab-text-secondary;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
    color: $ab-primary;
  }
}
</style>
