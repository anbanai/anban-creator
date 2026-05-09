export function buildSharePath(path: string, params?: Record<string, string>): string {
  const query = params ? '?' + Object.entries(params).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&') : ''
  return `${path}${query}`
}
