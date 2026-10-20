import type { InputAttachment } from '@/types/input-attachment'
import type { LocalLifeAsset, LocalLifeInput } from '@/types'

export function localLifeAssetsFromAttachments(attachments: InputAttachment[]): LocalLifeAsset[] {
  return attachments
    .filter((attachment): attachment is InputAttachment & { type: LocalLifeAsset['type'] } => attachment.type === 'image' || attachment.type === 'video')
    .map((attachment) => ({
      type: attachment.type,
      url: attachment.url,
      file_name: attachment.file_name,
      mime_type: attachment.content_type,
      file_size: attachment.size,
    }))
}

export function initialLocalLifeInput(brief = '', input?: Partial<LocalLifeInput>): LocalLifeInput {
  return {
    mix_type: input?.mix_type ?? 'general-store',
    brief: input?.brief ?? brief,
    merchant_facts: input?.merchant_facts ?? '',
    source_assets: input?.source_assets ?? [],
    variant_count: input?.variant_count ?? 3,
    target_duration_seconds: input?.target_duration_seconds ?? 30,
    aspect_ratio: input?.aspect_ratio ?? '9:16',
    voice_id: input?.voice_id ?? '',
    bgm_asset_id: input?.bgm_asset_id ?? '',
  }
}

export function buildLocalLifeInputForSubmit(brief: string | undefined, input?: Partial<LocalLifeInput>, attachments: InputAttachment[] = []): LocalLifeInput {
  const next = initialLocalLifeInput(brief ?? '', input)
  const uploadedAssets = localLifeAssetsFromAttachments(attachments)
  return {
    ...next,
    brief: next.brief.trim(),
    merchant_facts: next.merchant_facts?.trim() || undefined,
    source_assets: uploadedAssets.length > 0 ? uploadedAssets : next.source_assets,
    variant_count: Math.max(1, Math.min(10, Number(next.variant_count) || 3)),
    target_duration_seconds: Math.max(10, Math.min(60, Number(next.target_duration_seconds) || 30)),
    voice_id: next.voice_id?.trim() || undefined,
    bgm_asset_id: next.bgm_asset_id?.trim() || undefined,
  }
}
