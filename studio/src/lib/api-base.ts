/** Browser requests go directly to the independently deployed API. */
export function resolveApiBase(): string {
  return (import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/+$/, '')
}

export function apiURL(path: string): string {
  return `${resolveApiBase()}/${path.replace(/^\/+/, '')}`
}
