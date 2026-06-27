<script setup lang="ts">
import { onLaunch, onShow } from '@dcloudio/uni-app'
import { useAuthStore } from '@/stores/auth'

onLaunch(() => {
  const authStore = useAuthStore()

  // Sync auth state when API layer refreshes tokens
  uni.$on('auth:token-refreshed', (data: { token: string; refresh_token: string; user: any }) => {
    authStore.saveToStorage(data)
  })
  uni.$on('auth:token-expired', () => {
    authStore.clearStorage()
  })

  authStore.silentLogin()
})

onShow(() => {
  const authStore = useAuthStore()
  if (authStore.isAuthenticated && !authStore.isBootstrapping) {
    authStore.fetchUser().catch(() => {})
  }
})
</script>

<style>
/* ---- Theme tokens (light defaults) ----
 * Neutrals / surfaces / text / tints / shadows as CSS custom properties so the
 * whole app follows the system color scheme. Defined on `page` (mp-weixin root)
 * and `:root` (H5 html). Accent colors stay compile-time SCSS hex (see uni.scss).
 */
page,
:root {
  --ab-background: #F9FAFB;
  --ab-surface: #FFFFFF;
  --ab-text: #111827;
  --ab-text-secondary: #6B7280;
  --ab-text-tertiary: #9CA3AF;
  --ab-border: #E5E7EB;
  --ab-divider: #F3F4F6;
  --ab-primary-bg: #EEF2FF;
  --ab-success-bg: #ECFDF5;
  --ab-danger-bg: #FEF2F2;
  --ab-warning-bg: #FFFBEB;
  --ab-info-bg: #EFF6FF;
  --ab-shadow-sm: 0 1rpx 2rpx rgba(0, 0, 0, 0.05);
  --ab-shadow-md: 0 4rpx 12rpx rgba(0, 0, 0, 0.08);
  --ab-shadow-lg: 0 8rpx 24rpx rgba(0, 0, 0, 0.12);
}

/* ---- Dark theme (follows OS via prefers-color-scheme, Zinc palette) ---- */
@media (prefers-color-scheme: dark) {
  page,
  :root {
    --ab-background: #0E0E11;
    --ab-surface: #18181B;
    --ab-text: #F4F4F5;
    --ab-text-secondary: #A1A1AA;
    --ab-text-tertiary: #71717A;
    --ab-border: #27272A;
    --ab-divider: #1F1F23;
    --ab-primary-bg: #1E1B4B;
    --ab-success-bg: #052E1B;
    --ab-danger-bg: #2A1212;
    --ab-warning-bg: #2A1C08;
    --ab-info-bg: #0C1A2E;
    --ab-shadow-sm: 0 1rpx 2rpx rgba(0, 0, 0, 0.4);
    --ab-shadow-md: 0 4rpx 12rpx rgba(0, 0, 0, 0.5);
    --ab-shadow-lg: 0 8rpx 24rpx rgba(0, 0, 0, 0.6);
  }
}

page {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC',
    'Hiragino Sans GB', 'Microsoft YaHei', 'Helvetica Neue', sans-serif;
  font-size: $ab-text-base;
  color: $ab-text;
  background-color: $ab-background;
  -webkit-font-smoothing: antialiased;
}

view, text {
  box-sizing: border-box;
}

.page {
  min-height: 100vh;
  padding: $ab-space-md;
  /* Subtle content fade-in on navigation (opacity-only: transform would break
   * position:fixed children like bottom action bars). */
  animation: ab-page-in 0.26s ease-out;
}

@keyframes ab-page-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-lg 0 $ab-space-sm;
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
}

.section-header .more {
  font-size: $ab-text-sm;
  color: $ab-text-tertiary;
  font-weight: $ab-font-normal;
}

.card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.primary-button {
  background-color: $ab-primary;
  color: #ffffff;
  border: none;
  border-radius: $ab-radius-md;
  padding: $ab-space-sm $ab-space-lg;
  font-size: $ab-text-md;
  font-weight: $ab-font-medium;
  text-align: center;
  line-height: 1.5;

  &::after {
    border: none;
  }

  &:active {
    background-color: $ab-primary-light;
  }

  &.disabled {
    opacity: 0.5;
    pointer-events: none;
  }
}

.fixed-bottom {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  padding: $ab-space-sm $ab-space-md;
  padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom));
  background-color: $ab-surface;
  box-shadow: $ab-shadow-md;
}
</style>
