import type { VideoInput, VideoReferenceAsset } from '@/types'

export const VIDEO_PROJECT_DEFAULT_VALUE = '__project_default__'

export function readVideoSelectValue(value: string | null | undefined) {
  return value?.trim() || VIDEO_PROJECT_DEFAULT_VALUE
}

export function writeVideoSelectValue(value: string | null | undefined) {
  if (!value || value === VIDEO_PROJECT_DEFAULT_VALUE) return undefined
  return value
}

function cleanString(value: string | undefined) {
  const trimmed = value?.trim()
  return trimmed || undefined
}

function cleanReferences(references: VideoReferenceAsset[] | undefined) {
  if (!references?.length) return undefined
  const next = references.filter((ref) => {
    if (ref.type === 'text') return Boolean(ref.text?.trim())
    return Boolean(ref.url?.trim() || ref.task_file_id?.trim())
  }).map((ref) => ({
    ...ref,
    reference_role: cleanString(ref.reference_role),
    task_file_id: cleanString(ref.task_file_id),
    text: cleanString(ref.text),
    url: cleanString(ref.url),
  }))
  return next.length > 0 ? next : undefined
}

export function buildVideoInputForSubmit(prompt: string | undefined, input: VideoInput | undefined) {
  const brief = cleanString(input?.brief) || cleanString(prompt)
  const references = cleanReferences(input?.references)
  const ratio = writeVideoSelectValue(input?.hard_constraints?.ratio ?? undefined)
  const duration = input?.hard_constraints?.duration
  const watermark = input?.hard_constraints?.watermark
  const hardConstraints: VideoInput['hard_constraints'] = {}
  if (ratio) hardConstraints.ratio = ratio
  if (typeof duration === 'number' && duration > 0) hardConstraints.duration = duration
  if (typeof watermark === 'boolean') hardConstraints.watermark = watermark

  const next: VideoInput = {}
  if (brief) next.brief = brief
  if (references) next.references = references
  if (Object.keys(hardConstraints).length > 0) next.hard_constraints = hardConstraints

  return Object.keys(next).length > 0 ? next : undefined
}

export function initialVideoInput(prompt?: string, current?: VideoInput): VideoInput {
  return {
    brief: current?.brief ?? prompt ?? '',
    references: current?.references ?? [],
    hard_constraints: {
      ratio: current?.hard_constraints?.ratio,
      duration: current?.hard_constraints?.duration,
      watermark: current?.hard_constraints?.watermark,
    },
  }
}
