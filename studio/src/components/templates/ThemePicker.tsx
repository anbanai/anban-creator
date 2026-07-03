import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/Select'
import { ThemePreview } from '@/components/templates/ThemePreview'
import type { ResourceEntry } from '@/types/resource'
import type { CSSProperties } from 'react'

const NONE = '_none'
const fallbackColors = {
  background: '#f8fafc',
  primary: '#64748b',
  secondary: '#cbd5e1',
}

function themeLabel(theme?: ResourceEntry | null): string {
  if (!theme) return '不设置（用项目默认排版）'
  return theme.description || theme.name
}

function fallbackTheme(key: string): ResourceEntry | null {
  const trimmed = key.trim()
  if (!trimmed) return null
  return {
    name: trimmed,
    category: 'themes',
    description: trimmed,
    mood: '自定义或已下架主题',
    colors: fallbackColors,
  }
}

function ThemeSwatch({ theme }: { theme?: ResourceEntry | null }) {
  const label = theme ? themeLabel(theme) : '未选择'
  const colors = theme?.colors ?? fallbackColors
  const style = {
    '--theme-bg': colors.background || fallbackColors.background,
    '--theme-primary': colors.primary || fallbackColors.primary,
    '--theme-secondary': colors.secondary || fallbackColors.secondary,
  } as CSSProperties

  return (
    <span
      aria-label={`排版风格色彩预览 ${label}`}
      className="relative flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-[var(--theme-bg)] shadow-sm"
      style={style}
    >
      <span className="absolute inset-x-0 bottom-0 h-1/2 bg-[var(--theme-primary)]" />
      <span className="relative size-3 rounded-full border border-background bg-[var(--theme-secondary)] shadow-sm" />
    </span>
  )
}

function ThemeSummary({ theme, compact = false, noneLabel }: { theme?: ResourceEntry | null; compact?: boolean; noneLabel: string }) {
  if (!theme) {
    return (
      <div className="min-w-0 flex-1 text-left">
        <p className="truncate text-sm font-medium text-muted-foreground">{noneLabel}</p>
        {!compact && <p className="truncate text-xs text-muted-foreground">使用项目默认排版配置</p>}
      </div>
    )
  }

  const showBestFor = !compact && !!theme.best_for

  return (
    <div className="min-w-0 flex-1 text-left">
      <p className="truncate text-sm font-medium text-foreground">{themeLabel(theme)}</p>
      {(theme.mood || showBestFor) && (
        <p className="truncate text-xs text-muted-foreground">
          {theme.mood && <span>{theme.mood}</span>}
          {theme.mood && showBestFor && <span aria-hidden="true"> · </span>}
          {showBestFor && <span>{theme.best_for}</span>}
        </p>
      )}
    </div>
  )
}

// ThemePicker — 公众号「排版风格」选择器 + 实时预览，用于公众号项目编辑。
//
// - 选中后下拉触发器显示中文名（用 Base UI SelectValue 的 function child 渲染
//   description||name，不依赖隐式 label 映射，避免显示英文 key）。
// - 实时预览：选了主题即在下方即时渲染 ThemePreview（iframe），无需点按钮。
// - readOnly=true 时只展示当前主题名 + 预览。
interface ThemePickerProps {
  theme: string
  onTheme: (v: string) => void
  readOnly?: boolean
  /** 默认占位文案。 */
  noneLabel?: string
}

export function ThemePicker({
  theme,
  onTheme,
  readOnly = false,
  noneLabel = '不设置（用项目默认排版）',
}: ThemePickerProps) {
  const { data: themeResources } = useQuery({
    queryKey: queryKeys.resources.themes,
    queryFn: () => api.resources.list('themes'),
    staleTime: Infinity,
  })
  const themeOptions = ((themeResources?.items || []) as ResourceEntry[])
    .filter((t) => !!t.name)
    .sort((a, b) => themeLabel(a).localeCompare(themeLabel(b), 'zh-Hans-CN'))
  const selectedTheme = themeOptions.find((t) => t.name === theme) ?? fallbackTheme(theme)
  const selectOptions = selectedTheme && !themeOptions.some((t) => t.name === selectedTheme.name)
    ? [selectedTheme, ...themeOptions]
    : themeOptions

  const header = (
    <div className="flex items-center justify-between">
      <Label className="text-sm font-medium">排版风格</Label>
      {readOnly && (
        <Badge variant="secondary" className="text-[10px]">只读</Badge>
      )}
    </div>
  )

  const select = (
    <Select
      value={theme || NONE}
      onValueChange={(v) => onTheme(v && v !== NONE ? v : '')}
    >
      <SelectTrigger className="h-auto min-h-12 w-full whitespace-normal px-2 py-2">
        <SelectValue placeholder={noneLabel}>
          <div className="flex min-w-0 items-center gap-2">
            <ThemeSwatch theme={selectedTheme} />
            <ThemeSummary theme={selectedTheme} compact={false} noneLabel={noneLabel} />
          </div>
        </SelectValue>
      </SelectTrigger>
      <SelectContent className="w-[min(24rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)]">
        <SelectItem value={NONE} label={noneLabel}>{noneLabel}</SelectItem>
        {selectOptions.map((opt) => (
          <SelectItem key={opt.name} value={opt.name} label={themeLabel(opt)} className="py-2">
            <div className="flex min-w-0 items-start gap-2">
              <ThemeSwatch theme={opt} />
              <ThemeSummary theme={opt} noneLabel={noneLabel} />
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )

  const readOnlyLabel = theme ? (
    <div className="rounded-md border border-input bg-muted/30 px-3 py-2 text-sm text-foreground">
      {themeLabel(selectedTheme)}
    </div>
  ) : (
    <p className="text-xs text-muted-foreground">未设置排版风格。</p>
  )

  return (
    <div className="space-y-2 rounded-lg border border-dashed border-input p-3">
      {header}
      {readOnly ? readOnlyLabel : select}
      {theme ? (
        <ThemePreview theme={theme} />
      ) : (
        <p className="text-xs text-muted-foreground">选择一个排版风格后即时预览。</p>
      )}
    </div>
  )
}
