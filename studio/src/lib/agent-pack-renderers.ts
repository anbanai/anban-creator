const customRendererKeys = new Set([
  'article',
  'seednote',
  'moments',
  'ecommerce',
  'montage',
  'hypit',
])

export function registeredAgentPackForm(renderer?: string): string | null {
  if (!renderer?.startsWith('custom:')) return null
  const key = renderer.slice('custom:'.length)
  return customRendererKeys.has(key) ? key : null
}
