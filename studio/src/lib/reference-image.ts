import type { UploadToOSSResult } from '@/lib/direct-upload'
import type { ReferenceImageSelection, ReferenceImageValue } from '@/types/asset'

export function referenceSelectionFromUpload(
  upload: Pick<UploadToOSSResult, 'uploadSessionId'>,
): ReferenceImageSelection {
  return { upload_session_id: upload.uploadSessionId }
}

export function referenceSelectionFromValue(
  value: ReferenceImageValue | null | undefined,
): ReferenceImageSelection | null {
  if (!value) return null
  if (typeof value.asset_id === 'string') return { asset_id: value.asset_id }
  return { upload_session_id: value.upload_session_id }
}
