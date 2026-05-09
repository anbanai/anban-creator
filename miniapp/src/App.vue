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
