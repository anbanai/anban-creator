import type { MontageInput } from '@/types'
import type { MontageProjectDefaults } from '@/types/project'

export type MontageFormInput = Omit<MontageInput, 'source_assets' | 'delivery_targets'> & {
  source_assets: NonNullable<MontageInput['source_assets']>
  delivery_targets: NonNullable<MontageInput['delivery_targets']>
}

export function initialMontageInput(
  brief = '',
  input?: Partial<MontageInput>,
  defaults?: MontageProjectDefaults,
): MontageFormInput {
  return {
    brief: input?.brief ?? brief,
    pipeline_key: input?.pipeline_key ?? defaults?.default_pipeline ?? '',
    source_assets: input?.source_assets ?? [],
    preferences: {
      duration_seconds: input?.preferences?.duration_seconds ?? defaults?.preferences?.duration_seconds,
      style: input?.preferences?.style ?? defaults?.preferences?.style ?? '',
      music_prompt: input?.preferences?.music_prompt ?? defaults?.preferences?.music_prompt ?? '',
      subtitle_mode: input?.preferences?.subtitle_mode ?? defaults?.preferences?.subtitle_mode ?? '',
      voiceover_mode: input?.preferences?.voiceover_mode ?? defaults?.preferences?.voiceover_mode ?? '',
    },
    delivery_targets: input?.delivery_targets ?? defaults?.delivery_targets ?? [],
    advanced: input?.advanced ?? undefined,
  }
}

export function buildMontageInputForSubmit(brief: string | undefined, input?: Partial<MontageInput>): MontageInput {
  const next = initialMontageInput(brief ?? '', input)
  return {
    ...next,
    brief: (next.brief ?? '').trim(),
    pipeline_key: next.pipeline_key?.trim() || undefined,
    source_assets: next.source_assets ?? [],
    preferences: {
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
