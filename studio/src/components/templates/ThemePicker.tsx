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

const NONE = '_none'

// ThemePicker — 公众号「排版风格」选择器 + 实时预览，被模板编辑器与公众号项目编辑器共用。
//
// - 选中后下拉触发器显示中文名（用 Base UI SelectValue 的 function child 渲染
//   description||name，不依赖隐式 label 映射，避免显示英文 key）。
// - 实时预览：选了主题即在下方即时渲染 ThemePreview（iframe），无需点按钮。
// - readOnly=true（项目绑定模板、排版随模板同步）时只展示当前主题名 + 预览。
interface ThemePickerProps {
  theme: string
  onTheme: (v: string) => void
  readOnly?: boolean
  /** 默认占位文案；项目侧用「用项目默认排版」，模板侧用「用项目默认排版」。 */
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
    .map((t) => ({ value: t.name, label: t.description || t.name }))
    .filter((t) => !!t.value)
    .sort((a, b) => a.label.localeCompare(b.label))

  // value(英文 key) → 中文标签，用于 SelectValue function child。
  const labelFor = (value: string): string => {
    if (!value || value === NONE) return noneLabel
    return themeOptions.find((o) => o.value === value)?.label ?? value
  }

  const header = (
    <div className="flex items-center justify-between">
      <Label className="text-sm font-medium">排版风格</Label>
      {readOnly && (
        <Badge variant="secondary" className="text-[10px]">随模板同步</Badge>
      )}
    </div>
  )

  const select = (
    <Select
      value={theme || NONE}
      onValueChange={(v) => onTheme(v && v !== NONE ? v : '')}
    >
      <SelectTrigger className="w-full">
        {/* function child 渲染中文，保证选中后触发器显示中文名而非英文 key */}
        <SelectValue placeholder={noneLabel}>
          {(value: string) => labelFor(value)}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NONE} label={noneLabel}>{noneLabel}</SelectItem>
        {themeOptions.map((opt) => (
          <SelectItem key={opt.value} value={opt.value} label={opt.label}>
            {opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )

  const readOnlyLabel = theme ? (
    <div className="rounded-md border border-input bg-muted/30 px-3 py-2 text-sm text-foreground">
      {labelFor(theme)}
    </div>
  ) : (
    <p className="text-xs text-muted-foreground">所选模板未设置排版风格。</p>
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
