import axios from 'axios'
import type { ApiResponse } from '@/types'

const TOKEN_KEY = 'anbanwriter_token'
const REFRESH_TOKEN_KEY = 'anbanwriter_refresh_token'
const USER_KEY = 'anbanwriter_user'

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || '/api/v1'

export const http = axios.create({
  baseURL: apiBaseUrl,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Separate instance for token refresh — bypasses the 401 response interceptor
// to prevent deadlock when the refresh token itself is expired.
const refreshHttp = axios.create({
  baseURL: apiBaseUrl,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor: attach auth token
http.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: handle 401 with automatic token refresh
let isRefreshing = false
let refreshSubscribers: Array<(token: string) => void> = []

function onTokenRefreshed(token: string) {
  refreshSubscribers.forEach((cb) => cb(token))
  refreshSubscribers = []
}

http.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config

    if (error.response?.status === 401 && !originalRequest._retry) {
      const refreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)

      if (!refreshToken) {
        localStorage.removeItem(TOKEN_KEY)
        localStorage.removeItem(REFRESH_TOKEN_KEY)
        localStorage.removeItem(USER_KEY)
        window.dispatchEvent(new CustomEvent('auth:token-expired'))
        return Promise.reject(error)
      }

      if (isRefreshing) {
        return new Promise((resolve) => {
          refreshSubscribers.push((token: string) => {
            originalRequest.headers.Authorization = `Bearer ${token}`
            resolve(http(originalRequest))
          })
        })
      }

      originalRequest._retry = true
      isRefreshing = true

      try {
        // Direct call to refreshHttp to avoid circular dependency
        const response = await refreshHttp.post('/auth/refresh', { refresh_token: refreshToken })
        const authData = response.data.data as import('@/types').AuthResponse
        const { token: newToken, refresh_token: newRefreshToken, user } = authData

        localStorage.setItem(TOKEN_KEY, newToken)
        localStorage.setItem(REFRESH_TOKEN_KEY, newRefreshToken)
        localStorage.setItem(USER_KEY, JSON.stringify(user))

        onTokenRefreshed(newToken)

        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return http(originalRequest)
      } catch {
        localStorage.removeItem(TOKEN_KEY)
        localStorage.removeItem(REFRESH_TOKEN_KEY)
        localStorage.removeItem(USER_KEY)
        window.dispatchEvent(new CustomEvent('auth:token-expired'))
        return Promise.reject(error)
      } finally {
        isRefreshing = false
      }
    }

    return Promise.reject(error)
  },
)

/** Extract data from ApiResponse<T> envelope */
export async function unwrap<T>(request: Promise<{ data: ApiResponse<T> }>): Promise<T> {
  const response = await request
  return response.data.data
}

/** Extract user-friendly error message from an unknown error */
export function getApiErrorMessage(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const resp = (err as { response?: { data?: { msg?: string } } }).response
    if (resp?.data?.msg) return resp.data.msg
  }
  if (err instanceof Error) return err.message || fallback
  return fallback
}

export default http
