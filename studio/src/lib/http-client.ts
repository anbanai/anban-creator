import axios from 'axios'
import type { ApiResponse } from '@/types'

const TOKEN_KEY = 'anban_creator_token'
const REFRESH_TOKEN_KEY = 'anban_creator_refresh_token'
const USER_KEY = 'anban_creator_user'
function resolveApiBase(): string {
  if (import.meta.env.VITE_API_BASE_URL) return import.meta.env.VITE_API_BASE_URL
  return '/api/v1'
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

/** Keep backend diagnostics out of toast/error surfaces. */
export function sanitizeUserFacingErrorMessage(message: unknown, fallback: string): string {
  if (typeof message !== 'string') return fallback
  const raw = message.trim()
  if (!raw) return fallback
  const lower = raw.toLowerCase()

  if (isBillingMachineMessage(raw)) return fallback

  if (isGeneratedImageDownloadError(raw, lower)) {
    return '图片已生成，但保存到作品库失败，请稍后重试'
  }
  if (
    lower.includes('content policy') ||
    lower.includes('content_filter') ||
    raw.includes('敏感') ||
    raw.includes('审核') ||
    raw.includes('不合规')
  ) {
    return '提示词可能不符合内容安全要求，请调整后重试'
  }
  if (
    lower.includes('api key') ||
    lower.includes('base url') ||
    lower.includes('endpoint') ||
    raw.includes('配置文件') ||
    raw.includes('配置异常')
  ) {
    return '图像能力配置异常，请联系管理员'
  }
  if (lower.includes('rate limit') || lower.includes('timeout')) {
    return '图片服务繁忙，请稍后重试'
  }

  const internalMarkers = [
    '{"level"',
    '"error"',
    '<br/>',
    'revisedprompt',
    'http://',
    'https://',
    '[openai]',
    '[gemini]',
    '[volcengine]',
    'stack trace',
    'panic:',
  ]
  if (internalMarkers.some((marker) => lower.includes(marker))) return fallback

  const firstLine = raw.split(/\r?\n/, 1)[0]?.trim() || fallback
  return firstLine.length > 120 ? fallback : firstLine
}

function isGeneratedImageDownloadError(raw: string, lower: string): boolean {
  return (
    lower.includes('url_download_error') ||
    lower.includes('revisedprompt') ||
    raw.includes('图片接口返回了 URL') ||
    raw.includes('下载图片失败')
  )
}

function isBillingMachineMessage(message: string): boolean {
  return /\bbilling_[a-z0-9_]+\b/i.test(message)
}

interface ApiErrorBody {
  code?: number
  msg?: string
  error?: string
}

const billingErrorMessages: Partial<Record<number, string>> = {
  40201: '账户存在欠费，请先充值结清',
  40202: '积分余额不足，请先充值后继续',
  40203: '积分余额不足，请先充值后继续',
  40401: '服务计费配置异常，请联系管理员',
  40402: '服务计费配置异常，请联系管理员',
}

function getApiErrorBody(err: unknown): ApiErrorBody | undefined {
  if (!err || typeof err !== 'object' || !('response' in err)) return undefined
  const data = (err as { response?: { data?: unknown } }).response?.data
  return data && typeof data === 'object' ? data as ApiErrorBody : undefined
}

export function getApiErrorCode(err: unknown): number | undefined {
  const code = getApiErrorBody(err)?.code
  return typeof code === 'number' ? code : undefined
}

function isNetworkError(err: unknown): boolean {
  if (!axios.isAxiosError(err) || err.response) return false
  return err.code === 'ERR_NETWORK' || err.message === 'Network Error'
}

/** Extract user-friendly error message from an unknown error */
export function getApiErrorMessage(err: unknown, fallback: string): string {
  const body = getApiErrorBody(err)
  const code = getApiErrorCode(err)
  if (code !== undefined) {
    const localized = billingErrorMessages[code]
    if (localized) return localized
    if (code === 50000 || code === 50001) return fallback
  }

  const responseMessage = typeof body?.msg === 'string' && body.msg.trim() ? body.msg : body?.error
  if (responseMessage) return sanitizeUserFacingErrorMessage(responseMessage, fallback)

  if (isNetworkError(err)) return '网络连接失败，请检查网络后重试'
  if (err instanceof Error) return sanitizeUserFacingErrorMessage(err.message, fallback)
  return fallback
}

export default http
