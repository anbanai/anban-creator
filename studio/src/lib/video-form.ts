import type { VideoDefaults, VideoReferenceAsset, VideoTaskConfig } from '@/types'

export const VIDEO_PROJECT_DEFAULT_VALUE = '__project_default__'

export function readVideoSelectValue(value: string | null | undefined) {
  return value?.trim() || VIDEO_PROJECT_DEFAULT_VALUE
}

export function writeVideoSelectValue(value: string | null) {
  if (!value || value === VIDEO_PROJECT_DEFAULT_VALUE) return undefined
  return value
}

export function buildVideoFormConfig(defaults?: VideoDefaults, current?: VideoTaskConfig): VideoTaskConfig {
  return {
    ...(defaults ?? {}),
    ...(current ?? {}),
    references: current?.references ?? [],
  }
}

function cleanString(value: string | undefined) {
  if (!value) return undefined
  const trimmed = value.trim()
  if (!trimmed || trimmed === VIDEO_PROJECT_DEFAULT_VALUE) return undefined
  return trimmed
}

function cleanReferences(references: VideoReferenceAsset[] | undefined) {
  if (!references?.length) return undefined
  const next = references.filter((ref) => {
    if (ref.type === 'text') return Boolean(ref.text?.trim())
    return Boolean(ref.url?.trim())
  })
  return next.length > 0 ? next : undefined
}

export function normalizeVideoConfigForSubmit(config: VideoTaskConfig | undefined) {
  if (!config) return undefined

  const next: VideoTaskConfig = {}
  const purpose = cleanString(config.purpose)
  const modelKey = cleanString(config.model_key)
  const resolution = cleanString(config.resolution)
  const ratio = cleanString(config.ratio)
  const references = cleanReferences(config.references)

  if (purpose) next.purpose = purpose as VideoTaskConfig['purpose']
  if (modelKey) next.model_key = modelKey
  if (resolution) next.resolution = resolution
  if (ratio) next.ratio = ratio
  if (typeof config.duration === 'number' && config.duration > 0) next.duration = config.duration
  if (typeof config.watermark === 'boolean') next.watermark = config.watermark
  if (typeof config.preflight === 'boolean') next.preflight = config.preflight
  if (references) next.references = references

  return Object.keys(next).length > 0 ? next : undefined
}
