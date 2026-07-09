import type { OpenMontageInput } from '@/types'

export type OpenMontageFormInput = Omit<OpenMontageInput, 'source_assets' | 'delivery_targets'> & {
  source_assets: NonNullable<OpenMontageInput['source_assets']>
  delivery_targets: NonNullable<OpenMontageInput['delivery_targets']>
}

export function initialOpenMontageInput(brief = '', input?: Partial<OpenMontageInput>): OpenMontageFormInput {
  return {
    brief: input?.brief ?? brief,
    pipeline_key: input?.pipeline_key ?? '',
    source_assets: input?.source_assets ?? [],
    preferences: {
      aspect_ratio: input?.preferences?.aspect_ratio ?? '9:16',
      duration_seconds: input?.preferences?.duration_seconds ?? 30,
      style: input?.preferences?.style ?? '',
      music_prompt: input?.preferences?.music_prompt ?? '',
      subtitle_mode: input?.preferences?.subtitle_mode ?? '',
      voiceover_mode: input?.preferences?.voiceover_mode ?? '',
    },
    delivery_targets: input?.delivery_targets ?? [],
    advanced: input?.advanced ?? undefined,
  }
}

export function buildOpenMontageInputForSubmit(brief: string | undefined, input?: Partial<OpenMontageInput>): OpenMontageInput {
  const next = initialOpenMontageInput(brief ?? '', input)
  return {
    ...next,
    brief: (next.brief ?? '').trim(),
    pipeline_key: next.pipeline_key?.trim() || undefined,
    source_assets: next.source_assets ?? [],
    preferences: {
      aspect_ratio: next.preferences?.aspect_ratio || undefined,
      duration_seconds: next.preferences?.duration_seconds,
      style: next.preferences?.style?.trim() || undefined,
      music_prompt: next.preferences?.music_prompt?.trim() || undefined,
      subtitle_mode: next.preferences?.subtitle_mode || undefined,
      voiceover_mode: next.preferences?.voiceover_mode || undefined,
    },
    delivery_targets: next.delivery_targets ?? [],
    advanced: next.advanced,
  }
}
