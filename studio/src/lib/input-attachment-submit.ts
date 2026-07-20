import type { InputAttachment } from '@/types/input-attachment'

export interface PrepareReusableInputAttachmentsOptions {
  allowExternalURLs?: boolean
}

function isInternalAttachmentURL(url: string) {
  return url.startsWith('/api/v1/files/') || url.startsWith('/files/')
}

export function prepareReusableInputAttachments(
  attachments: InputAttachment[],
  options: PrepareReusableInputAttachmentsOptions = {},
): { attachments?: InputAttachment[]; error?: string } {
  const prepared: InputAttachment[] = []
  for (const attachment of attachments) {
    const name = attachment.file_name || '未命名附件'
    const hasUploadId = Boolean(attachment.upload_id)
    const hasKey = Boolean(attachment.key)
    const url = attachment.url?.trim() || ''

    if (hasUploadId && !hasKey) {
      return { error: `附件 ${name} 的上传信息不完整，请删除后重新上传` }
    }
    if (!hasUploadId && hasKey) {
      if (!url) {
        return { error: `附件 ${name} 缺少可复用的内部文件地址，请删除后重新上传` }
      }
      if (!isInternalAttachmentURL(url)) {
        return { error: `附件 ${name} 使用外部地址，无法安全克隆，请删除后重新上传` }
      }
      const { key: _derivedKey, ...legacyURLAttachment } = attachment
      prepared.push(legacyURLAttachment)
      continue
    }
    if (url && !isInternalAttachmentURL(url) && !options.allowExternalURLs) {
      return { error: `附件 ${name} 使用外部地址，无法安全克隆，请删除后重新上传` }
    }
    if (!hasUploadId && !url && !attachment.text) {
      return { error: `附件 ${name} 缺少可复用的文件来源，请删除后重新上传` }
    }
    prepared.push(attachment)
  }
  return { attachments: prepared }
}
