<template>
  <view class="settings-page">
    <!-- User Profile Card -->
    <AbCard>
      <view class="profile">
        <view class="profile__avatar-wrap" @tap="handleChangeAvatar">
          <image
            v-if="authStore.user?.avatar"
            class="profile__avatar"
            :src="authStore.user.avatar"
            mode="aspectFill"
          />
          <view v-else class="profile__avatar profile__avatar--empty">
            <text class="profile__avatar-text">{{ avatarLetter }}</text>
          </view>
          <view class="profile__avatar-badge">
            <text class="profile__avatar-badge-icon">📷</text>
          </view>
        </view>
        <view class="profile__info">
          <text class="profile__name">{{ authStore.user?.nickname || '未设置昵称' }}</text>
          <AbBadge :variant="tierBadgeVariant" size="sm">{{ tierLabel }}</AbBadge>
        </view>
      </view>

      <view class="profile-meta">
        <view class="profile-meta__row">
          <text class="profile-meta__icon">📧</text>
          <text class="profile-meta__value">{{ authStore.user?.email || '未设置邮箱' }}</text>
        </view>
        <view class="profile-meta__row">
          <text class="profile-meta__icon">📅</text>
          <text class="profile-meta__value">{{ registeredDate }}</text>
        </view>
        <view class="profile-meta__row" @tap="copyInviteCode">
          <text class="profile-meta__icon">🎫</text>
          <text class="profile-meta__label">邀请码</text>
          <text class="profile-meta__code">{{ authStore.user?.invite_code || '--' }}</text>
          <view class="profile-meta__copy">
            <text class="profile-meta__copy-text">复制</text>
          </view>
        </view>
        <view v-if="authStore.user" class="profile-meta__row">
          <text class="profile-meta__icon">👥</text>
          <text class="profile-meta__value">已邀请 {{ authStore.user.invite_count ?? 0 }}/{{ authStore.user.max_invites ?? 0 }} 人</text>
        </view>
      </view>
    </AbCard>

    <!-- Account Tier Info -->
    <view class="section">
      <text class="section__title">账号信息</text>
      <AbCard :padding="0">
        <view class="info-row">
          <text class="info-row__label">当前等级</text>
          <text class="info-row__value">{{ tierLabel }}</text>
        </view>
        <view class="info-row">
          <text class="info-row__label">并发任务上限</text>
          <text class="info-row__value">{{ authStore.user?.max_concurrent_limit ?? 2 }} 个</text>
        </view>
        <view class="info-row">
          <text class="info-row__label">配额说明</text>
          <text class="info-row__desc">{{ tierDescription }}</text>
        </view>
      </AbCard>
    </view>

    <!-- Other Settings -->
    <view class="section">
      <text class="section__title">账号与接入</text>
      <AbCard :padding="0">
        <view class="list-item" @tap="navigateTo('/pages/settings/model-config')">
          <text class="list-item__icon">🤖</text>
          <text class="list-item__title">模型配置</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item" @tap="navigateTo('/pages/settings/api-keys')">
          <text class="list-item__icon">🔑</text>
          <text class="list-item__title">平台密钥</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item" @tap="navigateTo('/pages/settings/password')">
          <text class="list-item__icon">🔒</text>
          <text class="list-item__title">{{ authStore.user?.has_password ? '修改密码' : '设置密码' }}</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item" @tap="navigateTo('/pages/connect/claude-code')">
          <text class="list-item__icon">🧩</text>
          <text class="list-item__title">Claude Code 接入</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item" @tap="navigateTo('/pages/connect/openclaw')">
          <text class="list-item__icon">🪄</text>
          <text class="list-item__title">OpenClaw 接入</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item" @tap="showFeedback = true">
          <text class="list-item__icon">💬</text>
          <text class="list-item__title">意见反馈</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
        <view class="list-item">
          <text class="list-item__icon">📜</text>
          <text class="list-item__title">用户协议</text>
          <text class="list-item__arrow">&#8250;</text>
        </view>
      </AbCard>
    </view>

    <!-- Logout -->
    <view class="logout-section">
      <AbButton type="danger" size="lg" block :loading="logoutLoading" @click="handleLogout">
        退出登录
      </AbButton>
    </view>

    <!-- Version -->
    <view class="version">
      <text class="version__text">Anban 智能创作 v1.0.0</text>
    </view>

    <!-- Bottom spacer -->
    <view class="bottom-spacer" />

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
import { useAuthStore } from '@/stores/auth'
import { post } from '@/api/request'
import { tierLabels, tierDescriptions } from '@/utils/labels'
import { formatDateYMD } from '@/utils/format'
import AbCard from '@/components/common/AbCard.vue'
import AbBadge from '@/components/common/AbBadge.vue'
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

const tierDescription = computed(() => {
  return tierDescriptions[authStore.user?.tier || 'free'] || ''
})

const tierBadgeVariant = computed(() => {
  switch (authStore.user?.tier) {
    case 'pro': return 'info'
    case 'enterprise': return 'warning'
    default: return 'neutral'
  }
})

const registeredDate = computed(() => {
  if (!authStore.user?.created_at) return '未知'
  try {
    const d = new Date(authStore.user.created_at)
    return `${formatDateYMD(d)} 注册`
  } catch {
    return '未知'
  }
})

// --- Avatar ---
function handleChangeAvatar() {
  uni.chooseImage({
    count: 1,
    sizeType: ['compressed'],
    sourceType: ['album', 'camera'],
    success: (res) => {
      const tempPath = res.tempFilePaths[0]
      // Upload avatar to server
      uni.uploadFile({
        url: '/api/v1/auth/avatar',
        filePath: tempPath,
        name: 'avatar',
        header: {
          Authorization: `Bearer ${authStore.token}`,
        },
        success: (uploadRes) => {
          try {
            const body = JSON.parse(uploadRes.data)
            if (body.code === 0 && body.data?.avatar) {
              authStore.user!.avatar = body.data.avatar
              uni.setStorageSync('anbanwriter_user', JSON.stringify(authStore.user))
              uni.showToast({ title: '头像已更新', icon: 'success' })
            } else {
              uni.showToast({ title: body.msg || '更新失败', icon: 'none' })
            }
          } catch {
            uni.showToast({ title: '上传失败', icon: 'none' })
          }
        },
        fail: () => {
          uni.showToast({ title: '上传失败', icon: 'none' })
        },
      })
    },
  })
}

// --- Invite code ---
function copyInviteCode() {
  const code = authStore.user?.invite_code
  if (!code) return
  uni.setClipboardData({
    data: code,
    success: () => {
      uni.showToast({ title: '已复制邀请码', icon: 'success' })
    },
  })
}

function navigateTo(url: string) {
  uni.navigateTo({ url })
}

// --- Logout ---
const logoutLoading = ref(false)

function handleLogout() {
  uni.showModal({
    title: '确认退出',
    content: '退出登录后需要重新授权微信登录',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (!res.confirm) return
      logoutLoading.value = true
      try {
        await authStore.logout()
        uni.showToast({ title: '已退出登录', icon: 'success' })
        // Navigate to home and re-trigger login
        setTimeout(() => {
          uni.reLaunch({ url: '/pages/index/index' })
        }, 500)
      } catch {
        uni.showToast({ title: '退出失败', icon: 'none' })
      } finally {
        logoutLoading.value = false
      }
    },
  })
}

// --- Feedback ---
const showFeedback = ref(false)
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

// --- Refresh user data on mount ---
onMounted(async () => {
  try {
    await authStore.fetchUser()
  } catch {
    // Silent fail
  }
})
</script>

<style lang="scss" scoped>
.settings-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

// Profile card
.profile {
  display: flex;
  align-items: center;
  margin-bottom: $ab-space-md;

  &__avatar-wrap {
    position: relative;
    flex-shrink: 0;
  }

  &__avatar {
    width: 160rpx;
    height: 160rpx;
    border-radius: 50%;
    background-color: $ab-primary-bg;

    &--empty {
      display: flex;
      align-items: center;
      justify-content: center;
    }
  }

  &__avatar-text {
    font-size: 56rpx;
    font-weight: $ab-font-semibold;
    color: $ab-primary;
  }

  &__avatar-badge {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 44rpx;
    height: 44rpx;
    border-radius: 50%;
    background-color: $ab-surface;
    border: 4rpx solid $ab-border;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  &__avatar-badge-icon {
    font-size: 22rpx;
  }

  &__info {
    flex: 1;
    margin-left: $ab-space-lg;
    display: flex;
    flex-direction: column;
    gap: $ab-space-xs;
  }

  &__name {
    font-size: $ab-text-xl;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }
}

// Profile meta
.profile-meta {
  border-top: 2rpx solid $ab-divider;
  padding-top: $ab-space-md;

  &__row {
    display: flex;
    align-items: center;
    padding: $ab-space-xs 0;
  }

  &__icon {
    font-size: $ab-text-base;
    width: 40rpx;
    flex-shrink: 0;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-right: $ab-space-xs;
  }

  &__value {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__code {
    font-size: $ab-text-sm;
    color: $ab-text;
    font-weight: $ab-font-medium;
    font-family: monospace;
    margin-right: $ab-space-xs;
  }

  &__copy {
    padding: 4rpx $ab-space-sm;
    border: 2rpx solid $ab-primary;
    border-radius: $ab-radius-full;
  }

  &__copy-text {
    font-size: $ab-text-xs;
    color: $ab-primary;
    font-weight: $ab-font-medium;
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

// Info row (inside AbCard with padding=0)
.info-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: $ab-space-md $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }

  &__label {
    font-size: $ab-text-base;
    color: $ab-text-secondary;
    flex-shrink: 0;
  }

  &__value {
    font-size: $ab-text-base;
    color: $ab-text;
    font-weight: $ab-font-medium;
    flex-shrink: 0;
  }

  &__desc {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    text-align: right;
    max-width: 400rpx;
    line-height: 1.5;
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

// Logout
.logout-section {
  margin-top: $ab-space-lg;
  margin-bottom: $ab-space-md;
}

// Version
.version {
  display: flex;
  justify-content: center;
  padding: $ab-space-md 0;

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

// Bottom spacer
.bottom-spacer {
  height: 60rpx;
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
