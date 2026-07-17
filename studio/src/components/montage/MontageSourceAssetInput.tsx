import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import type { InputAttachment, InputAttachmentType } from '@/types/input-attachment'
import type { MontageAsset, MontageAssetType } from '@/types'

interface MontageSourceAssetInputProps {
  value: MontageAsset[]
  onChange: (value: MontageAsset[]) => void
  onUploadingChange?: (uploading: boolean) => void
}

type AdapterAttachment = InputAttachment & {
  __montageAsset?: MontageAsset
}

const attachmentTypeByAssetType: Record<MontageAssetType, InputAttachmentType> = {
  image_url: 'image',
  video_url: 'video',
  audio_url: 'audio',
  document_url: 'document',
  text: 'text',
}

const assetTypeByAttachmentType: Record<InputAttachmentType, MontageAssetType> = {
  image: 'image_url',
  video: 'video_url',
  audio: 'audio_url',
  document: 'document_url',
  text: 'text',
}

function toAttachment(asset: MontageAsset): AdapterAttachment {
  return {
    type: attachmentTypeByAssetType[asset.type],
    url: asset.url,
    file_name: asset.file_name,
    content_type: asset.mime_type,
    size: asset.file_size,
    __montageAsset: asset,
  }
}

function toMontageAsset(attachment: AdapterAttachment): MontageAsset {
  const asset: MontageAsset = {
    ...(attachment.__montageAsset || {}),
    type: assetTypeByAttachmentType[attachment.type],
  }
  if (attachment.url !== undefined) asset.url = attachment.url
  if (attachment.file_name !== undefined) asset.file_name = attachment.file_name
  if (attachment.content_type !== undefined) asset.mime_type = attachment.content_type
  if (attachment.size !== undefined) asset.file_size = attachment.size
  return asset
}

export function MontageSourceAssetInput({ value, onChange, onUploadingChange }: MontageSourceAssetInputProps) {
  return (
    <ReferenceMaterialInput
      value={value.map(toAttachment)}
      onChange={(attachments) => onChange(attachments.map((attachment) => toMontageAsset(attachment as AdapterAttachment)))}
      allowedTypes={['image', 'video', 'audio', 'document', 'text']}
      maxCount={20}
      compact
      hint="支持图片、视频、音频、文本和文档素材。"
      uploadPurpose="montage_asset"
      onUploadingChange={onUploadingChange}
    />
  )
}
