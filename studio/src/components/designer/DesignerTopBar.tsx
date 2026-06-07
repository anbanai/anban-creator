import { useState } from 'react'
import { Palette, ChevronDown } from 'lucide-react'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/Select'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerProvider, ModelCapabilities } from '@/types/designer'
import type { DesignerSettings } from '@/types/designer'

interface DesignerTopBarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
  provider: string
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
}

export default function DesignerTopBar({
  providers,
  selectedProviderId,
  onModelChange,
  provider,
  settings,
  onSettingsChange,
}: DesignerTopBarProps) {
  const caps = getModelCapabilities(provider)
  const selected = providers.find((p) => p.id === selectedProviderId) ?? providers[0]
  const [modelOpen, setModelOpen] = useState(false)

  function update(patch: Partial<DesignerSettings>) {
    onSettingsChange({ ...settings, ...patch })
  }

  return (
    <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border bg-card px-4">
      <div className="flex items-center gap-2">
        <Palette className="h-4 w-4 text-primary" />
        <span className="text-sm font-medium text-foreground">设计师</span>
      </div>

      {/* Model selector popover */}
      <Popover open={modelOpen} onOpenChange={setModelOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium text-foreground transition-colors hover:bg-accent"
            />
          }
        >
          {selected?.name ?? '选择模型'}
          <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" side="bottom" className="w-80">
          <div className="space-y-1">
            {providers.map((p) => {
              const isActive = selectedProviderId === p.id
              const pCaps: ModelCapabilities | undefined = getModelCapabilities(p.provider)
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => {
                    onModelChange(p.id)
                    setModelOpen(false)
                  }}
                  className={`flex w-full flex-col items-start gap-1 rounded-md px-3 py-2 text-left transition-colors ${
                    isActive
                      ? 'bg-primary/10 text-foreground'
                      : 'text-foreground hover:bg-accent'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{p.name}</span>
                    {isActive && (
                      <span className="h-1.5 w-1.5 rounded-full bg-primary" />
                    )}
                  </div>
                  {p.description && (
                    <span className="text-xs text-muted-foreground">{p.description}</span>
                  )}
                  {pCaps && (
                    <div className="flex gap-1">
                      {pCaps.batch && <Badge variant="secondary" className="text-[10px]">批量</Badge>}
                      {pCaps.inpainting && <Badge variant="secondary" className="text-[10px]">局部编辑</Badge>}
                    </div>
                  )}
                </button>
              )
            })}
          </div>
        </PopoverContent>
      </Popover>

      {/* Quick settings on the right */}
      <div className="ml-auto flex items-center gap-2">
        {caps && caps.qualityLevels.length > 0 && (
          <Select
            value={settings.quality}
            onValueChange={(val: string | null) => update({ quality: val ?? '' })}
          >
            <SelectTrigger className="h-8 w-24 text-xs">
              <SelectValue placeholder="质量" />
            </SelectTrigger>
            <SelectContent>
              {caps.qualityLevels.map((level) => (
                <SelectItem key={level} value={level}>
                  {level === 'auto' ? '自动' : level === 'low' ? '低' : level === 'medium' ? '中' : '高'}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}

        {caps && caps.outputFormats.length > 1 && (
          <Select
            value={settings.outputFormat}
            onValueChange={(val: string | null) => update({ outputFormat: val ?? '' })}
          >
            <SelectTrigger className="h-8 w-20 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {caps.outputFormats.map((fmt) => (
                <SelectItem key={fmt} value={fmt}>
                  {fmt.toUpperCase()}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
    </div>
  )
}
