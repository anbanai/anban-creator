import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/Select'
import { ReferenceImageUpload } from '@/components/channels/ReferenceImageUpload'
import { SignedImage } from '@/components/ui/SignedImage'
import type { ResourceEntry } from '@/types/resource'

// PersonaBlock — 公众号「人设」统一区块，被模板编辑器与公众号频道编辑器共用，
// 保证两处 UI 完全一致。把「名称（署名）」+「写作风格」+「人设头像」聚合在一起，
// 并提供「从人设库导入」一键填充（选中人设 → 名字取人设中文名、写作风格取其简介）。
//
// 名称即署名：保存后落到 author_name/author（频道为 author），经 get_channel_profile
// 作为发布作者名下发；写作风格落到 author_style_intro（template_writing_style）。
// readOnly=true 时渲染为只读摘要（频道绑定模板、人设随模板同步时使用）。
interface PersonaBlockProps {
  authorName: string
  onAuthorName: (v: string) => void
  authorStyleIntro: string
  onAuthorStyleIntro: (v: string) => void
  authorAvatarUrl: string
  onAuthorAvatarUrl: (v: string) => void
  readOnly?: boolean
}

export function PersonaBlock({
  authorName,
  onAuthorName,
  authorStyleIntro,
  onAuthorStyleIntro,
  authorAvatarUrl,
  onAuthorAvatarUrl,
  readOnly = false,
}: PersonaBlockProps) {
  // 人设库（writers）用于一键导入：entry.name = 人设中文名（如 "Dan Koe"），
  // entry.description = 风格简介，entry.category_cn = 分类。
  const { data: writerResources } = useQuery({
    queryKey: queryKeys.resources.writers,
    queryFn: () => api.resources.list('writers'),
    staleTime: Infinity,
  })
  const writers = (writerResources?.items || []) as ResourceEntry[]
  // writer entry 的 name 即人设中文名；按名字排序稳定展示。
  const sortedWriters = [...writers].sort((a, b) => (a.name || '').localeCompare(b.name || ''))
  const writerLabel = (w: ResourceEntry) =>
    w.category_cn ? `${w.name}（${w.category_cn}）` : w.name

  // 只读摘要：频道绑定模板时，人设随模板同步展示。
  if (readOnly) {
    const hasAny = authorName || authorStyleIntro || authorAvatarUrl
    if (!hasAny) {
      return (
        <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
          <div className="flex items-center justify-between">
            <Label className="text-sm font-medium">人设</Label>
            <Badge variant="secondary" className="text-[10px]">随模板同步</Badge>
          </div>
          <p className="text-xs text-muted-foreground">所选模板未设置人设。</p>
        </div>
      )
    }
    return (
      <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
        <div className="flex items-center justify-between">
          <Label className="text-sm font-medium">人设</Label>
          <Badge variant="secondary" className="text-[10px]">随模板同步·改模板自动更新</Badge>
        </div>
        <div className="flex items-start gap-3">
          {authorAvatarUrl && (
            <SignedImage
              src={authorAvatarUrl}
              alt="人设头像"
              className="h-12 w-12 shrink-0 rounded-full object-cover"
              showLoading={false}
            />
          )}
          <div className="min-w-0 flex-1 space-y-1">
            {authorName && (
              <p className="text-sm font-medium text-foreground">{authorName}</p>
            )}
            {authorStyleIntro && (
              <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">
                {authorStyleIntro}
              </p>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
      <div className="flex items-center justify-between">
        <Label className="text-sm font-medium">人设</Label>
        <span className="text-xs text-muted-foreground">名称即署名 · 写作风格供 AI 模仿</span>
      </div>
      <div className="flex items-stretch gap-3">
        <div className="shrink-0">
          <ReferenceImageUpload
            value={authorAvatarUrl}
            onChange={onAuthorAvatarUrl}
            purpose="reference"
          />
          <p className="mt-1 text-center text-[11px] text-muted-foreground">人设头像（可选）</p>
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <Input
            value={authorName}
            onChange={(e) => onAuthorName(e.target.value)}
            placeholder="名称（署名），例如：Dan Koe"
            maxLength={100}
          />
          <Textarea
            value={authorStyleIntro}
            onChange={(e) => onAuthorStyleIntro(e.target.value)}
            placeholder="写作风格：例如犀利、接地气、像朋友聊天；多用短句和反问；爱用具体数字和案例"
            maxLength={1024}
            className="resize-none"
            rows={3}
          />
        </div>
      </div>
      {sortedWriters.length > 0 && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">从人设库导入</span>
          <Select
            value=""
            onValueChange={(v) => {
              const w = sortedWriters.find((x) => x.name === v || x.english_name === v)
              if (!w) return
              if (w.name) onAuthorName(w.name)
              if (w.description) onAuthorStyleIntro(w.description)
            }}
          >
            <SelectTrigger className="h-7 w-full max-w-xs text-xs">
              <SelectValue placeholder="选择人设，自动填入名称与写作风格" />
            </SelectTrigger>
            <SelectContent>
              {sortedWriters.map((w) => (
                <SelectItem
                  key={w.english_name || w.name}
                  value={w.name || w.english_name || ''}
                  label={writerLabel(w)}
                >
                  {writerLabel(w)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}
    </div>
  )
}
