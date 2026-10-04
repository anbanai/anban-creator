import { Check, Layers3 } from 'lucide-react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { platformIcon } from '@/lib/PlatformIcon'
import { contentTypeLabel } from '@/lib/labels'

export const creationTypeDescriptions: Record<string, string> = {
  'wechat-article': '长文创作、配图与排版',
  'wechat-picture': '多图创作与配文',
  seednote: '笔记文案与种草配图',
  viral_analysis: '拆解笔记的内容与表达',
  moments: '朋友圈文案与配图',
  ecommerce: '商品主图与详情素材',
  montage: '根据素材生成视频',
  hypit: '参考视频改编与复刻',
  'whiteboard-animation': '根据字幕制作白板动画',
}

export interface CreationTypeOption {
  id: string
  label?: string
  description?: string
}

export function CreationTypePicker({ options, value, onChange, multiple = false, disabled = false }: {
  options: CreationTypeOption[]
  value: string[]
  onChange: (value: string[]) => void
  multiple?: boolean
  disabled?: boolean
}) {
  return (
    <ToggleGroup
      multiple={multiple}
      value={value}
      onValueChange={(next) => { if (multiple || next.length > 0) onChange(next) }}
      disabled={disabled}
      variant="outline"
      aria-label="创作类型"
      className="grid w-full grid-cols-1 items-stretch gap-2 sm:grid-cols-3"
    >
      {options.map((option) => {
        const Icon = platformIcon[option.id] ?? Layers3
        const label = option.label ?? contentTypeLabel[option.id] ?? option.id
        const selected = value.includes(option.id)
        return (
          <ToggleGroupItem
            key={option.id}
            value={option.id}
            aria-label={label}
            className="h-auto min-h-16 min-w-0 justify-start gap-2.5 whitespace-normal rounded-lg px-3 py-2.5 text-left aria-pressed:border-primary aria-pressed:bg-primary/5 aria-pressed:text-foreground"
          >
            <Icon className="size-4 shrink-0" />
            <span className="min-w-0 flex-1">
              <span className="block font-medium">{label}</span>
              <span className="mt-0.5 block text-xs font-normal leading-relaxed text-muted-foreground">{creationTypeDescriptions[option.id] ?? option.description}</span>
            </span>
            <Check aria-hidden="true" className={`size-3.5 shrink-0 ${selected ? 'text-primary' : 'invisible'}`} />
          </ToggleGroupItem>
        )
      })}
    </ToggleGroup>
  )
}
