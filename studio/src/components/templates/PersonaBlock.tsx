import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/Select'
import type { ResourceEntry } from '@/types/resource'

interface PersonaBlockProps {
  author: string
  onAuthor: (v: string) => void
  writer: string
  onWriter: (v: string) => void
  readOnly?: boolean
}

function writerValue(w: ResourceEntry): string {
  return w.english_name || w.name || ''
}

function writerLabel(w: ResourceEntry): string {
  if (w.display_name) return w.display_name
  if (w.name && w.category_cn) return `${w.name}（${w.category_cn}）`
  return w.name || w.english_name || ''
}

export function PersonaBlock({
  author,
  onAuthor,
  writer,
  onWriter,
  readOnly = false,
}: PersonaBlockProps) {
  const { data: writerResources } = useQuery({
    queryKey: queryKeys.resources.writers,
    queryFn: () => api.resources.list('writers'),
    staleTime: Infinity,
  })

  const writers = useMemo(() => {
    const items = (writerResources?.items || []) as ResourceEntry[]
    return [...items]
      .filter((w) => writerValue(w))
      .sort((a, b) => writerLabel(a).localeCompare(writerLabel(b), 'zh-Hans-CN'))
  }, [writerResources])

  const selectedWriter = writers.find((w) => writerValue(w) === writer)

  if (readOnly) {
    return (
      <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
        <div className="flex items-center justify-between">
          <Label className="text-sm font-medium">发布署名 · 写作风格</Label>
          <Badge variant="secondary" className="text-[10px]">随模板同步</Badge>
        </div>
        <div className="space-y-1">
          <p className="text-sm font-medium text-foreground">{author || '未设置发布署名'}</p>
          <p className="text-sm text-muted-foreground">
            {selectedWriter ? writerLabel(selectedWriter) : writer || '未设置写作风格'}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3 rounded-lg border border-dashed border-input p-3">
      <div className="flex items-center justify-between gap-2">
        <Label className="text-sm font-medium">发布署名 · 写作风格</Label>
        <span className="shrink-0 text-xs text-muted-foreground">头像/昵称仅用于 Studio 选择展示</span>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1">
          <Label htmlFor="persona-author-name" className="text-xs font-medium text-muted-foreground">
            公众号发布署名
          </Label>
          <Input
            id="persona-author-name"
            value={author}
            onChange={(e) => onAuthor(e.target.value)}
            placeholder="例如：李雷、某某实验室"
            maxLength={100}
          />
        </div>

        <div className="space-y-1">
          <Label className="text-xs font-medium text-muted-foreground">
            写作风格
          </Label>
          <Select value={writer || ''} onValueChange={(value) => onWriter(value || '')}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder="选择写作风格，例如 Dan Koe" />
            </SelectTrigger>
            <SelectContent>
              {writers.map((w) => (
                <SelectItem key={writerValue(w)} value={writerValue(w)} label={writerLabel(w)}>
                  <div className="flex items-center gap-2">
                    <span>{writerLabel(w)}</span>
                    {w.category_cn && (
                      <span className="text-xs text-muted-foreground">{w.category_cn}</span>
                    )}
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  )
}
