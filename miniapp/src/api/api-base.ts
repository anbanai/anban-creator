const DEFAULT_API_BASE_URL = 'https://api.creator.anbanai.com/api/v1'

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '')
}

export function getApiBaseUrl(): string {
  const envBase = import.meta.env?.VITE_API_BASE_URL?.trim()
  return trimTrailingSlash(envBase || DEFAULT_API_BASE_URL)
}

export function apiUrl(path: string): string {
  if (/^https?:\/\//i.test(path)) return path
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${getApiBaseUrl()}${normalizedPath}`
}

export function uploadUrl(path: string): string {
  return apiUrl(path)
}
