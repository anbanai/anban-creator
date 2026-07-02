import type { ApiResponse } from '@/types'

const API_BASE_URL = '/api/v1'
const TOKEN_KEY = 'anban_creator_token'
const REFRESH_TOKEN_KEY = 'anban_creator_refresh_token'
const USER_KEY = 'anban_creator_user'
const DEFAULT_TIMEOUT = 30000

let isRefreshing = false
let refreshSubscribers: Array<(token: string) => void> = []

function onTokenRefreshed(token: string) {
  refreshSubscribers.forEach((cb) => cb(token))
  refreshSubscribers = []
}

function doRefreshToken(): Promise<string> {
  return new Promise((resolve, reject) => {
    const refreshToken = uni.getStorageSync(REFRESH_TOKEN_KEY)
    if (!refreshToken) {
      reject(new Error('no refresh token'))
      return
    }

    uni.request({
      url: `${API_BASE_URL}/auth/refresh`,
      method: 'POST',
      header: { 'Content-Type': 'application/json' },
      data: { refresh_token: refreshToken },
      success(res) {
        if (res.statusCode === 200 && res.data) {
          const body = res.data as ApiResponse
          if (body.code === 0) {
            const authData = body.data as {
              token: string
              refresh_token: string
              user: unknown
            }
            uni.setStorageSync(TOKEN_KEY, authData.token)
            uni.setStorageSync(REFRESH_TOKEN_KEY, authData.refresh_token)
            uni.setStorageSync(USER_KEY, JSON.stringify(authData.user))
            uni.$emit('auth:token-refreshed', authData)
            resolve(authData.token)
            return
          }
        }
        reject(new Error('refresh failed'))
      },
      fail(err) {
        reject(err)
      },
    })
  })
}

function clearAuthStorage() {
  uni.removeStorageSync(TOKEN_KEY)
  uni.removeStorageSync(REFRESH_TOKEN_KEY)
  uni.removeStorageSync(USER_KEY)
  uni.$emit('auth:token-expired')
}

function addAuthHeader(header: Record<string, string>): Record<string, string> {
  const token = uni.getStorageSync(TOKEN_KEY)
  if (token) {
    header.Authorization = `Bearer ${token}`
  }
  return header
}

function buildUrl(path: string, data?: Record<string, unknown>): string {
  const url = `${API_BASE_URL}${path}`
  if (!data) return url

  const qs = Object.entries(data)
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
    .join('&')

  return qs ? `${url}?${qs}` : url
}

function request<T>(
  method: string,
  path: string,
  data?: any,
  customTimeout?: number,
): Promise<T> {
  const isGetLike = method === 'GET' || method === 'DELETE'
  const url = isGetLike ? buildUrl(path, data) : `${API_BASE_URL}${path}`

  const header = addAuthHeader({
    'Content-Type': 'application/json',
  })

  return new Promise<T>((resolve, reject) => {
    const task = uni.request({
      url,
      method: method as any,
      data: isGetLike ? undefined : data,
      header,
      timeout: customTimeout ?? DEFAULT_TIMEOUT,
      success(res) {
        if (res.statusCode === 200 && res.data) {
          const body = res.data as ApiResponse
          if (body.code === 0) {
            resolve(body.data as T)
            return
          }
          const err = new Error(body.msg || 'request failed') as Error & {
            code: number
            statusCode: number
          }
          err.code = body.code
          err.statusCode = res.statusCode
          reject(err)
          return
        }

        if (res.statusCode === 401) {
          const originalReq = { method, path, data, header: { ...header }, customTimeout }

          if (isRefreshing) {
            refreshSubscribers.push((newToken: string) => {
              originalReq.header.Authorization = `Bearer ${newToken}`
              request<T>(
                originalReq.method,
                originalReq.path,
                originalReq.data,
                originalReq.customTimeout,
              ).then(resolve).catch(reject)
            })
            return
          }

          isRefreshing = true

          doRefreshToken()
            .then((newToken) => {
              onTokenRefreshed(newToken)
              originalReq.header.Authorization = `Bearer ${newToken}`
              request<T>(
                originalReq.method,
                originalReq.path,
                originalReq.data,
                originalReq.customTimeout,
              ).then(resolve).catch(reject)
            })
            .catch(() => {
              clearAuthStorage()
              refreshSubscribers.forEach((cb) => cb(''))
              refreshSubscribers = []
              reject(new Error('登录已过期，请重新登录'))
            })
            .finally(() => {
              isRefreshing = false
            })
          return
        }

        const err = new Error(`请求失败 (${res.statusCode})`) as Error & {
          statusCode: number
          data: any
        }
        err.statusCode = res.statusCode
        err.data = res.data
        reject(err)
      },
      fail(err) {
        reject(new Error(err.errMsg || '网络请求失败'))
      },
    })

    ;(request as any)._lastTask = task
  })
}

export function get<T>(url: string, params?: Record<string, any>): Promise<T> {
  return request<T>('GET', url, params)
}

export function post<T>(url: string, data?: any, customTimeout?: number): Promise<T> {
  return request<T>('POST', url, data, customTimeout)
}

export function put<T>(url: string, data?: any): Promise<T> {
  return request<T>('PUT', url, data)
}

export function patch<T>(url: string, data?: any): Promise<T> {
  return request<T>('PATCH', url, data)
}

export function del<T>(url: string): Promise<T> {
  return request<T>('DELETE', url)
}

export function getApiErrorMessage(err: unknown, fallback: string): string {
  if (err && typeof err === 'object') {
    if ('errMsg' in err) return (err as { errMsg: string }).errMsg || fallback
    if ('message' in err) {
      const msg = (err as { message: string }).message
      if (msg) return msg
    }
    if ('response' in err) {
      const resp = (err as { response?: { data?: { msg?: string } } }).response
      if (resp?.data?.msg) return resp.data.msg
    }
  }
  if (typeof err === 'string') return err
  return fallback
}
