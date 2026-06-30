import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
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

function fallbackWriter(key: string): ResourceEntry | null {
  const trimmed = key.trim()
  if (!trimmed) return null
  return {
    name: trimmed,
    english_name: trimmed,
    display_name: trimmed,
    category: 'writers',
    description: '自定义或已下架的写作风格 key',
  }
}

function writerInitial(w?: ResourceEntry | null): string {
  const label = w ? writerLabel(w) : ''
  return (label || '?').trim().slice(0, 1).toUpperCase()
}

function WriterAvatar({ writer, size = 'default' }: { writer?: ResourceEntry | null; size?: 'sm' | 'default' }) {
  const label = writer ? writerLabel(writer) : '未选择'
  return (
    <Avatar size={size} aria-label={`写作风格头像 ${label}`} className="bg-primary/10">
      <AvatarFallback className="bg-primary/10 text-primary">
        {writerInitial(writer)}
      </AvatarFallback>
    </Avatar>
  )
}

function WriterSummary({ writer, compact = false }: { writer?: ResourceEntry | null; compact?: boolean }) {
  if (!writer) {
    return (
      <div className="min-w-0 flex-1 text-left">
        <p className="truncate text-sm font-medium text-muted-foreground">选择写作风格</p>
        {!compact && <p className="truncate text-xs text-muted-foreground">决定文章的表达方式，不影响发布署名</p>}
      </div>
    )
  }

  const label = writerLabel(writer)
  const subtitleParts = [
    writer.english_name && writer.english_name !== label ? writer.english_name : '',
    !compact ? writer.description : '',
  ].filter(Boolean)

  return (
    <div className="min-w-0 flex-1 text-left">
      <div className="flex min-w-0 items-center gap-2">
        <p className="truncate text-sm font-medium text-foreground">{label}</p>
        {writer.category_cn && (
          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
            {writer.category_cn}
          </span>
        )}
      </div>
      {subtitleParts.length > 0 && (
        <p className="truncate text-xs text-muted-foreground">
          {subtitleParts.join(' · ')}
        </p>
      )}
    </div>
  )
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

  const selectedWriter = writers.find((w) => writerValue(w) === writer) ?? fallbackWriter(writer)

  if (readOnly) {
    return (
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
          <div className="flex items-center justify-between">
            <Label className="text-sm font-medium">发布署名</Label>
            <Badge variant="secondary" className="text-[10px]">只读</Badge>
          </div>
          <p className="text-sm font-medium text-foreground">{author || '未设置发布署名'}</p>
        </div>
        <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
          <div className="flex items-center justify-between">
            <Label className="text-sm font-medium">写作风格</Label>
            <Badge variant="secondary" className="text-[10px]">只读</Badge>
          </div>
          <div className="flex items-center gap-2">
            <WriterAvatar writer={selectedWriter} size="sm" />
            <WriterSummary writer={selectedWriter} compact />
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <section className="space-y-2 rounded-lg border border-input p-3">
        <div className="flex items-center justify-between">
          <Label htmlFor="persona-author-name" className="text-sm font-medium">
            发布署名
          </Label>
          <span className="text-xs text-muted-foreground">公众号 author</span>
        </div>
        <Input
          id="persona-author-name"
          aria-label="公众号发布署名"
          value={author}
          onChange={(e) => onAuthor(e.target.value)}
          placeholder="例如：李雷、某某实验室"
          maxLength={100}
        />
      </section>

      <section className="space-y-2 rounded-lg border border-input p-3">
        <div className="flex items-center justify-between">
          <Label className="text-sm font-medium">写作风格</Label>
          <span className="text-xs text-muted-foreground">头像/昵称仅用于选择展示</span>
        </div>
        <Select value={writer || ''} onValueChange={(value) => onWriter(value || '')}>
          <SelectTrigger className="h-auto min-h-12 w-full whitespace-normal px-2 py-2">
            <SelectValue>
              <div className="flex min-w-0 items-center gap-2">
                <WriterAvatar writer={selectedWriter} size="sm" />
                <WriterSummary writer={selectedWriter} compact />
              </div>
            </SelectValue>
          </SelectTrigger>
          <SelectContent className="min-w-80">
            {writers.map((w) => (
              <SelectItem key={writerValue(w)} value={writerValue(w)} label={writerLabel(w)} className="py-2">
                <div className="flex min-w-0 items-start gap-2">
                  <WriterAvatar writer={w} />
                  <WriterSummary writer={w} />
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </section>
    </div>
  )
}
