import type { PromptAttachment } from '@/types/input-attachment'

export interface MaterialOrdinal { index: number; typeIndex: number; label: string }
const TYPE_LABELS: Record<PromptAttachment['type'], string> = { image: '图', audio: '音频', video: '视频', document: '文档', text: '文本' }
export function materialOrdinals(attachments: readonly PromptAttachment[]): MaterialOrdinal[] {
  const counts = new Map<PromptAttachment['type'], number>()
  return attachments.map((attachment, offset) => {
    const typeIndex = (counts.get(attachment.type) ?? 0) + 1
    counts.set(attachment.type, typeIndex)
    return { index: offset + 1, typeIndex, label: `${TYPE_LABELS[attachment.type]} ${typeIndex}` }
  })
}
export function hasOrdinalMaterialReference(prompt: string): boolean {
  return /(?:第\s*[一二三四五六七八九十百千万零〇两\d]+\s*(?:张|个|份|项|条)?\s*(?:图|图片|照片|素材|附件|文件)|附件\s*[一二三四五六七八九十百千万零〇两\d]+|素材\s*[一二三四五六七八九十百千万零〇两\d]+)/u.test(prompt)
}
