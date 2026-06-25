import { useEffect, useState } from 'react'
import { FileText, Loader2, Package } from 'lucide-react'
import { api } from '@/lib/api'
import type { TaskFile } from '@/types'
import { FilePreviewGallery } from '@/components/FilePreview'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'

// E-commerce delivery gallery: groups generated images by module prefix
// (main_/detail_/cover_/share_/sku_) and provides an on-demand viewer for the
// agent's text deliverables (product-bible.md, copywriting.md, asset-plan.md,
// compliance-report.md, manifest.json). Filename prefixes match the agent's
// file-naming contract in claudecode/agents/ecommerce.md.

const MODULE_GROUPS = [
  { prefix: 'main_', label: '主图套', key: 'main' },
  { prefix: 'detail_', label: '详情页（商详）', key: 'detail' },
  { prefix: 'cover_', label: '封面 / Banner', key: 'cover' },
  { prefix: 'share_', label: '分享图', key: 'share' },
  { prefix: 'sku_', label: 'SKU 变体图', key: 'sku' },
] as const

const DOC_EXTENSIONS = ['.md', '.json', '.txt']

function moduleKeyOf(name: string): string | null {
  const lower = name.toLowerCase()
  for (const g of MODULE_GROUPS) {
    if (lower.startsWith(g.prefix)) return g.key
  }
  return null
}

function isDoc(name: string): boolean {
  const lower = name.toLowerCase()
  return DOC_EXTENSIONS.some((ext) => lower.endsWith(ext))
}

export function EcommerceFilesGallery({ files, taskId }: { files: TaskFile[]; taskId: string }) {
  const images = files.filter((f) => f.mime_type?.startsWith('image/'))
  const docs = files.filter((f) => isDoc(f.file_name))

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

  if (grouped.length === 0 && docs.length === 0) return null

  return (
    <div className="space-y-4">
      {grouped.map((grp) => (
        <div key={grp.key}>
          <div className="mb-1.5 flex items-center gap-2">
            <Package className="h-3.5 w-3.5 text-muted-foreground" />
            <h3 className="text-xs font-medium text-foreground">{grp.label}</h3>
            <span className="text-[10px] text-muted-foreground">{grp.files.length}</span>
          </div>
          <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
            <FilePreviewGallery files={grp.files} taskId={taskId} inlineItemClassName="shrink-0 snap-start" />
          </div>
        </div>
      ))}

      {docs.length > 0 && <EcommerceDocsViewer docs={docs} taskId={taskId} />}
    </div>
  )
}

// On-demand text deliverable viewer. Fetches the selected file's bytes via the
// download endpoint and renders pretty-printed JSON or raw text. Markdown is
// shown verbatim (monospace) — readable without a renderer dependency.
function EcommerceDocsViewer({ docs, taskId }: { docs: TaskFile[]; taskId: string }) {
  const [selectedId, setSelectedId] = useState<string>(docs[0]?.id ?? '')
  const [content, setContent] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const selected = docs.find((d) => d.id === selectedId) ?? docs[0]

  useEffect(() => {
    if (!selected) return
    let cancelled = false
    setLoading(true)
    setError('')
    api.tasks
      .downloadFileBlob(taskId, selected.id)
      .then((blob) => blob.text())
      .then((text) => {
        if (cancelled) return
        let display = text
        if (selected.file_name.toLowerCase().endsWith('.json')) {
          try {
            display = JSON.stringify(JSON.parse(text), null, 2)
          } catch {
            /* keep raw if not valid JSON */
          }
        }
        setContent(display)
      })
      .catch(() => {
        if (!cancelled) setError('加载文档失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [selectedId, taskId, selected])

  if (!selected) return null

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <FileText className="h-3.5 w-3.5 text-muted-foreground" />
        <h3 className="text-xs font-medium text-foreground">交付文档</h3>
      </div>
      <Select value={selectedId} onValueChange={(v) => { if (v) setSelectedId(v) }}>
        <SelectTrigger className="w-full sm:w-80">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {docs.map((d) => (
            <SelectItem key={d.id} value={d.id}>
              {d.file_name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="max-h-96 overflow-auto rounded-md border border-border bg-muted/30 p-3">
        {loading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" /> 加载中…
          </div>
        ) : error ? (
          <p className="text-xs text-destructive">{error}</p>
        ) : (
          <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-foreground">{content}</pre>
        )}
      </div>
    </div>
  )
}
