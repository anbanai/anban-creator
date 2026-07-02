import axios from 'axios'
import { isDesktop } from '@/lib/tauri'
import type { ApiResponse } from '@/types'

const TOKEN_KEY = 'anban_creator_token'
const REFRESH_TOKEN_KEY = 'anban_creator_refresh_token'
const USER_KEY = 'anban_creator_user'
// Mirror of the desktop-configured cloud API base. The Tauri shell seeds this
// via a webview initialization script before the SPA boots, so resolution stays
// synchronous (the very first request — login — fires before any IPC could
// resolve). The settings UI updates it through setApiBase().
const API_BASE_STORAGE_KEY = 'anban_creator_api_base'

/**
 * Resolve the axios baseURL at module load, with no async/IPC so the first
 * request never races configuration:
 *   1. Explicit build-time env (self-hosted / docker) wins outright.
 *   2. Desktop-configured base mirrored into localStorage by the Tauri shell.
 *   3. Web dev proxy (same-origin /api/v1).
 *   4. Desktop without any configured base: the production cloud server.
 */
export const DEFAULT_CLOUD_API_BASE = 'https://api.anbanai.com/api/v1'

function resolveApiBase(): string {
  if (import.meta.env.VITE_API_BASE_URL) return import.meta.env.VITE_API_BASE_URL
  // Only the desktop shell seeds localStorage.anban_creator_api_base. Honoring it
  // on the plain web/self-hosted build would let any writer of that key (XSS, a
  // malicious browser extension, or a shared machine) silently redirect every
  // authenticated request — including /auth/refresh — to an attacker-controlled
  // origin and exfiltrate the JWT + refresh token from the Authorization header.
  // Gate it behind isDesktop(); the web build always uses the same-origin proxy.
  if (isDesktop() && typeof localStorage !== 'undefined') {
    const stored = localStorage.getItem(API_BASE_STORAGE_KEY)
    if (stored) return stored
  }
  if (!isDesktop()) return '/api/v1'
  return DEFAULT_CLOUD_API_BASE
}

export const http = axios.create({
  baseURL: resolveApiBase(),
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Separate instance for token refresh — bypasses the 401 response interceptor
// to prevent deadlock when the refresh token itself is expired.
const refreshHttp = axios.create({
  baseURL: resolveApiBase(),
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

/**
 * Update the cloud API base at runtime (settings UI). Applies to both axios
 * instances and mirrors into localStorage so it survives reloads. No-op for the
 * value currently in effect.
 */
export function applyApiBase(base: string): void {
  const normalized = base.replace(/\/+$/, '')
  if (typeof localStorage !== 'undefined') localStorage.setItem(API_BASE_STORAGE_KEY, normalized)
  http.defaults.baseURL = normalized
  refreshHttp.defaults.baseURL = normalized
}

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
    const resp = (err as { response?: { data?: { msg?: string; error?: string } } }).response
    if (resp?.data?.msg) return resp.data.msg
    if (resp?.data?.error) return resp.data.error
  }
  if (err instanceof Error) return err.message || fallback
  return fallback
}

export default http
