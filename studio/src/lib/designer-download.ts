import http from '@/lib/http-client'
import { isInternalStorageUrl, normalizeStorageUrl } from '@/lib/storage-url'
import { toast } from 'sonner'

export async function downloadDesignerImage(url: string, index: number) {
  try {
    const normalized = normalizeStorageUrl(url)
    let blob: Blob
    if (isInternalStorageUrl(normalized)) {
      blob = (await http.get<Blob>(normalized, { responseType: 'blob' })).data
    } else {
      const response = await fetch(normalized)
      if (!response.ok) throw new Error('download failed')
      blob = await response.blob()
    }
    const extension = ({ 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp' } as Record<string, string>)[blob.type] ?? 'png'
    const objectUrl = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = objectUrl
    anchor.download = `designer-${index + 1}.${extension}`
    anchor.click()
    setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
  } catch {
    toast.error('图片下载失败，请稍后重试')
  }
}
