import type { ProfileDimension, ProjectProfile } from '@/types'

export const PROJECT_PROFILE_DIMENSIONS = ['identity', 'style', 'audience', 'platforms', 'preferences', 'memory'] as const

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

export function normalizeProfileDimension(value: unknown): ProfileDimension {
  const raw = isObject(value) ? value : {}
  return {
    content: isObject(raw.content) ? raw.content : {},
    sources: stringArray(raw.sources),
    evidence: stringArray(raw.evidence),
    missing_fields: stringArray(raw.missing_fields),
  }
}

export function normalizeProjectProfile(value: unknown): ProjectProfile | null {
  if (!isObject(value)) return null
  const rawDimensions = isObject(value.dimensions) ? value.dimensions : {}
  const dimensions = Object.fromEntries(PROJECT_PROFILE_DIMENSIONS.map((name) => [name, normalizeProfileDimension(rawDimensions[name])])) as ProjectProfile['dimensions']
  const initializationStatus = value.initialization_status
  return {
    schema_version: typeof value.schema_version === 'number' ? value.schema_version : 1,
    status: value.status === 'confirmed' ? 'confirmed' : 'draft',
    initialization_status: initializationStatus === 'queued' || initializationStatus === 'running' || initializationStatus === 'ready' || initializationStatus === 'failed'
      ? initializationStatus
      : 'not_started',
    ...(typeof value.last_error === 'string' ? { last_error: value.last_error } : {}),
    version: typeof value.version === 'number' ? value.version : 0,
    ...(typeof value.analysis_task_id === 'string' ? { analysis_task_id: value.analysis_task_id } : {}),
    ...(typeof value.application_source === 'string' ? { application_source: value.application_source } : {}),
    dimensions,
    analysis_limits: stringArray(value.analysis_limits),
    follow_up_questions: stringArray(value.follow_up_questions),
  }
}
