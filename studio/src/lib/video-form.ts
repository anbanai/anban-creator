import type { VideoDefaults, VideoPurpose, VideoReferenceAsset, VideoTaskConfig, VideoWorkflow } from '@/types'

export const VIDEO_PROJECT_DEFAULT_VALUE = '__project_default__'
export type VideoFormConfig = VideoTaskConfig & { workflow: VideoWorkflow }

export function readVideoSelectValue(value: string | null | undefined) {
  return value?.trim() || VIDEO_PROJECT_DEFAULT_VALUE
}

export function writeVideoSelectValue(value: string | null) {
  if (!value || value === VIDEO_PROJECT_DEFAULT_VALUE) return undefined
  return value
}

export function defaultVideoPurposeForCreativeType(creativeType: string | null | undefined): VideoPurpose {
  return creativeType === 'high_efficiency_joke' ? 'promotion' : 'planting'
}

export function buildVideoFormConfig(defaults?: VideoDefaults, current?: VideoTaskConfig): VideoFormConfig {
  const workflow = current?.workflow === 'editor' || defaults?.workflow === 'editor' ? 'editor' : 'creator'
  const creativeType = current?.creative_type ?? defaults?.creative_type ?? 'personal_ip'
  const purpose = current?.purpose ?? defaults?.purpose ?? defaultVideoPurposeForCreativeType(creativeType)
  const productionMode = current?.production_mode ?? defaults?.production_mode ?? 'guided'
  return {
    ...(defaults ?? {}),
    ...(current ?? {}),
    workflow,
    production_mode: productionMode,
    creative_type: creativeType,
    purpose,
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

export function normalizeVideoConfigForSubmit(config: VideoTaskConfig | undefined, defaults?: VideoDefaults) {
  if (!config) return undefined

  const next: VideoTaskConfig = {}
  const scenarioKey = cleanString(config.scenario_key)
  const productionMode = cleanString(config.production_mode)
  const purpose = cleanString(config.purpose)
  const creativeType = cleanString(config.creative_type)
  const resolvedCreativeType = creativeType || defaults?.creative_type || 'personal_ip'
  const resolvedPurpose = purpose || (creativeType ? defaultVideoPurposeForCreativeType(creativeType) : defaults?.purpose || defaultVideoPurposeForCreativeType(resolvedCreativeType))
  const subjectProfile = cleanString(config.subject_profile)
  const audience = cleanString(config.audience)
  const singleMessage = cleanString(config.single_message)
  const modelKey = cleanString(config.model_key)
  const resolution = cleanString(config.resolution)
  const ratio = cleanString(config.ratio)
  const references = cleanReferences(config.references)
  const workflow = config.workflow === 'editor' ? 'editor' : 'creator'

  next.workflow = workflow
  if (scenarioKey) next.scenario_key = scenarioKey
  if (productionMode) next.production_mode = productionMode as VideoTaskConfig['production_mode']
  if (resolvedPurpose && resolvedPurpose !== defaults?.purpose) next.purpose = resolvedPurpose as VideoTaskConfig['purpose']
  if (resolvedCreativeType && resolvedCreativeType !== defaults?.creative_type) next.creative_type = resolvedCreativeType as VideoTaskConfig['creative_type']
  if (subjectProfile && subjectProfile !== defaults?.subject_profile) next.subject_profile = subjectProfile
  if (audience && audience !== defaults?.audience) next.audience = audience
  if (singleMessage && singleMessage !== defaults?.single_message) next.single_message = singleMessage
  if (modelKey && modelKey !== defaults?.model_key) next.model_key = modelKey
  if (resolution && resolution !== defaults?.resolution) next.resolution = resolution
  if (ratio && ratio !== defaults?.ratio) next.ratio = ratio
  if (typeof config.duration === 'number' && config.duration > 0 && config.duration !== defaults?.duration) next.duration = config.duration
  if (typeof config.watermark === 'boolean' && config.watermark !== defaults?.watermark) next.watermark = config.watermark
  if (typeof config.preflight === 'boolean' && config.preflight !== defaults?.preflight) next.preflight = config.preflight
  if (typeof config.retake_budget === 'number' && config.retake_budget > 0) next.retake_budget = config.retake_budget
  if (config.delivery_targets?.length) next.delivery_targets = config.delivery_targets
  if (references) next.references = references

  return Object.keys(next).length > 0 ? next : undefined
}
