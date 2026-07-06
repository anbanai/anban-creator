import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { User, AuthResponse } from '@/types'
import { TOKEN_KEY, REFRESH_TOKEN_KEY, USER_KEY, AUTO_REFRESH_TOKEN_THRESHOLD } from '@/utils/constants'
import { apiUrl } from '@/api/api-base'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>('')
  const refreshToken = ref<string>('')
  const user = ref<User | null>(null)
  const isBootstrapping = ref(true)

  const isAuthenticated = computed(() => !!token.value)

  function loadFromStorage() {
    try {
      token.value = uni.getStorageSync(TOKEN_KEY) || ''
      refreshToken.value = uni.getStorageSync(REFRESH_TOKEN_KEY) || ''
      const stored = uni.getStorageSync(USER_KEY)
      user.value = stored ? JSON.parse(stored) : null
    } catch {
      clearStorage()
    }
  }

  function saveToStorage(data: { token: string; refresh_token: string; user: User }) {
    token.value = data.token
    refreshToken.value = data.refresh_token
    user.value = data.user
    uni.setStorageSync(TOKEN_KEY, data.token)
    uni.setStorageSync(REFRESH_TOKEN_KEY, data.refresh_token)
    uni.setStorageSync(USER_KEY, JSON.stringify(data.user))
  }

  function clearStorage() {
    token.value = ''
    refreshToken.value = ''
    user.value = null
    uni.removeStorageSync(TOKEN_KEY)
    uni.removeStorageSync(REFRESH_TOKEN_KEY)
    uni.removeStorageSync(USER_KEY)
  }

  async function silentLogin() {
    isBootstrapping.value = true
    loadFromStorage()

    if (token.value) {
      // Try to refresh user data with existing token
      try {
        await fetchUser()
        isBootstrapping.value = false
        return
      } catch {
        // Token expired, fall through to wx.login
      }
    }

    // WeChat silent login
    try {
      const loginRes = await new Promise<UniApp.LoginRes>((resolve, reject) => {
        uni.login({
          provider: 'weixin',
          success: resolve,
          fail: reject,
        })
      })

      const res = await uni.request({
        url: apiUrl('/auth/wx-login'),
        method: 'POST',
        data: { code: loginRes.code },
        header: { 'Content-Type': 'application/json' },
      })

      const body = res.data as { code: number; data: AuthResponse }
      if (body.code !== 0) throw new Error(body.data?.toString() || '登录失败')

      saveToStorage(body.data)
    } catch (err) {
      console.error('Silent login failed:', err)
      // On non-WeChat platforms (H5 dev), skip login
      // user can still browse but API calls will fail
    } finally {
      isBootstrapping.value = false
    }
  }

  async function fetchUser() {
    const res = await uni.request({
      url: apiUrl('/auth/me'),
      method: 'GET',
      header: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token.value}`,
      },
    })
    const body = res.data as { code: number; data: User }
    if (body.code !== 0) throw new Error('获取用户信息失败')
    user.value = body.data
    uni.setStorageSync(USER_KEY, JSON.stringify(body.data))
  }

  async function refreshAccessToken(): Promise<string> {
    if (!refreshToken.value) {
      throw new Error('No refresh token')
    }

    const res = await uni.request({
      url: apiUrl('/auth/refresh'),
      method: 'POST',
      data: { refresh_token: refreshToken.value },
      header: { 'Content-Type': 'application/json' },
    })

    const body = res.data as { code: number; data: AuthResponse }
    if (body.code !== 0) {
      clearStorage()
      throw new Error('Token refresh failed')
    }

    saveToStorage(body.data)
    return body.data.token
  }

  async function logout() {
    try {
      await uni.request({
        url: apiUrl('/auth/logout'),
        method: 'POST',
        header: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token.value}`,
        },
      })
    } finally {
      clearStorage()
    }
  }

  return {
    token,
    refreshToken,
    user,
    isBootstrapping,
    isAuthenticated,
    loadFromStorage,
    saveToStorage,
    clearStorage,
    silentLogin,
    fetchUser,
    refreshAccessToken,
    logout,
  }
})
