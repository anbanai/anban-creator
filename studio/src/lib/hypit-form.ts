import type { HypitInput, HypitDefaults, HypitLimits } from '@/types'
export function initialHypitInput(brief = '', input?: Partial<HypitInput>, defaults?: HypitDefaults): HypitInput {
  const preferences = { ...defaults?.preferences, ...input?.preferences }
  if (preferences.duration_seconds === 0) preferences.duration_seconds = undefined
  return { brief: input?.brief ?? brief, reference: input?.reference, source_assets: input?.source_assets ?? [], preferences }
}
export function hypitReadinessError(input: HypitInput | undefined, limits: HypitLimits, remix = false): string {
  if (!remix && (!input?.reference || !['video', 'video_url'].includes(input.reference.type) || !(input.reference.url?.trim() || input.reference.task_file_id))) return '请上传主参考视频或填写主参考视频链接'
  if (input?.reference?.type === 'video_url') {
    try { if (!['http:', 'https:'].includes(new URL(input.reference.url ?? '').protocol)) return '主参考视频链接必须为 HTTP 或 HTTPS 地址' } catch { return '请输入有效的主参考视频链接' }
  }
  const assets = [input?.reference, ...(input?.source_assets ?? [])].filter(x => x != null)
  if ((input?.source_assets?.length ?? 0) > limits.max_assets) return `补充素材不能超过 ${limits.max_assets} 个`
  if (assets.some(a => (a.file_size ?? 0) > limits.max_asset_bytes)) return '单个素材超过服务器大小限制'
  if (assets.reduce((n,a) => n + (a.file_size ?? 0), 0) > limits.max_input_bytes) return '素材总大小超过服务器限制'
  const duration = input?.preferences?.duration_seconds
  if (duration !== undefined && (!Number.isInteger(duration) || duration < 1)) return '时长必须为正整数秒'
  if ((duration ?? 0) > limits.max_duration_seconds) return `时长不能超过 ${limits.max_duration_seconds} 秒`
  return ''
}

// The API treats an omitted duration as inheritance, and zero as an explicit
// follow-source override. Keep that sentinel out of the editable form.
export function buildHypitInputForSubmit(brief: string, input?: Partial<HypitInput>, defaults?: HypitDefaults): HypitInput {
  const preferences = { ...input?.preferences }
  if (preferences.duration_seconds === undefined && (defaults?.preferences?.duration_seconds ?? 0) > 0) {
    preferences.duration_seconds = 0
  }
  return { ...input, brief, preferences }
}
