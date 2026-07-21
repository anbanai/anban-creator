import { useState } from 'react'
import { FileText, Package, Download } from 'lucide-react'
import { api } from '@/lib/api'
import type { TaskFile } from '@/types'
import { FilePreviewGallery } from '@/components/FilePreview'
import { Button } from '@/components/common/button'

// E-commerce delivery gallery: groups generated images by module prefix
// (main_/detail_/cover_/share_/sku_) and renders the agent's text deliverables
// (product-bible.md, copywriting.md, asset-plan.md, compliance-report.md,
// manifest.json, ...) through the SAME FilePreviewGallery used by seednote /
// article tasks — so markdown renders, copy / download / modal navigation all
// work identically to the rest of the app. No bespoke doc viewer here.
// Filename prefixes match the agent's file-naming contract in
// plugins/anban/agents/ecommerce.md.

const MODULE_GROUPS = [
  { prefix: 'main_', label: '主图套', key: 'main' },
  { prefix: 'detail_', label: '详情页（商详）', key: 'detail' },
  { prefix: 'cover_', label: '封面 / Banner', key: 'cover' },
  { prefix: 'share_', label: '分享图', key: 'share' },
  { prefix: 'sku_', label: 'SKU 变体图', key: 'sku' },
] as const

function moduleKeyOf(name: string): string | null {
  const lower = name.toLowerCase()
  for (const g of MODULE_GROUPS) {
    if (lower.startsWith(g.prefix)) return g.key
  }
  return null
}

export function EcommerceFilesGallery({
  files,
  taskId,
}: {
  files: TaskFile[]
  taskId: string
}) {
  const images = files.filter((f) => f.mime_type?.startsWith('image/'))
  const nonImages = files.filter((f) => !f.mime_type?.startsWith('image/'))
  const [downloading, setDownloading] = useState(false)

  // 整包下载：复用通用 zip 端点（与 TaskDetailPage 一致）。叶子组件不引 toast，
  // 失败静默（画廊本身是 best-effort 视图）。
  const handleDownloadAll = async () => {
    setDownloading(true)
    try {
      const blob = await api.tasks.downloadZipBlob(taskId)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `ecommerce_${taskId}.zip`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      // Best-effort download in a leaf component (no toast here). Log so a
      // session-expiry / 401 / network failure is traceable in devtools rather
      // than the button silently stopping its spinner.
      console.error('[EcommerceFilesGallery] download zip failed:', err)
    } finally {
      setDownloading(false)
    }
  }

  // Bucket images by module prefix (preserving catalog order), collect the rest.
  const bucket: Record<string, TaskFile[]> = {}
  const other: TaskFile[] = []
  for (const img of images) {
    const k = moduleKeyOf(img.file_name)
    if (k) (bucket[k] ??= []).push(img)
    else other.push(img)
  }
  const grouped: { key: string; label: string; files: TaskFile[] }[] = MODULE_GROUPS
    .filter((g) => bucket[g.key]?.length)
    .map((g) => ({ key: g.key, label: g.label, files: bucket[g.key] }))
  if (other.length) grouped.push({ key: 'other', label: '其他图片', files: other })

  if (grouped.length === 0 && nonImages.length === 0) return null

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={handleDownloadAll} loading={downloading} disabled={files.length === 0}>
          <Download className="h-3.5 w-3.5" />
          整包下载
        </Button>
      </div>
      {grouped.map((grp) => (
        <div key={grp.key}>
          <div className="mb-1.5 flex items-center gap-2">
            <Package className="h-3.5 w-3.5 text-muted-foreground" />
            <h3 className="text-xs font-medium text-foreground">{grp.label}</h3>
            <span className="text-[10px] text-muted-foreground">{grp.files.length}</span>
          </div>
          <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
            <FilePreviewGallery
              files={grp.files}
              taskId={taskId}
              inlineItemClassName="shrink-0 snap-start"
            />
          </div>
        </div>
      ))}

      {/* Text deliverables — rendered via the standard FilePreviewGallery so the
          experience (markdown rendering, copy, download, modal navigation) is
          identical to seednote / article tasks. */}
      {nonImages.length > 0 && (
        <div>
          <div className="mb-1.5 flex items-center gap-2">
            <FileText className="h-3.5 w-3.5 text-muted-foreground" />
            <h3 className="text-xs font-medium text-foreground">交付文档</h3>
            <span className="text-[10px] text-muted-foreground">{nonImages.length}</span>
          </div>
          <div className="space-y-2">
            <FilePreviewGallery
              files={nonImages}
              taskId={taskId}
            />
          </div>
        </div>
      )}
    </div>
  )
}
