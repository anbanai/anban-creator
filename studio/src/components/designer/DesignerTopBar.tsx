import { useState } from 'react'
import { Palette, ChevronDown } from 'lucide-react'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { Badge } from '@/components/ui/badge'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerProvider, ModelCapabilities } from '@/types/designer'

interface DesignerTopBarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
}

export default function DesignerTopBar({
  providers,
  selectedProviderId,
  onModelChange,
}: DesignerTopBarProps) {
  const selected = providers.find((p) => p.id === selectedProviderId) ?? providers[0]
  const [modelOpen, setModelOpen] = useState(false)

  return (
    <div className="flex h-11 shrink-0 items-center gap-3 border-b border-border/50 bg-card/80 px-4 backdrop-blur-md">
      <div className="flex items-center gap-2">
        <Palette className="h-3.5 w-3.5 text-muted-foreground/60" />
        <span className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground/50">Designer</span>
      </div>

      <Popover open={modelOpen} onOpenChange={setModelOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-foreground/80 transition-all hover:bg-accent/50 hover:text-foreground"
            />
          }
        >
          {selected?.name ?? '选择模型'}
          <ChevronDown className="h-3 w-3 text-muted-foreground/60" />
        </PopoverTrigger>
        <PopoverContent align="start" side="bottom" className="w-80 border-border/50 shadow-xl backdrop-blur-lg">
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
                  className={`flex w-full flex-col items-start gap-1 rounded-md px-3 py-2.5 text-left transition-all ${
                    isActive
                      ? 'border-l-2 border-l-primary bg-primary/5 text-foreground'
                      : 'border-l-2 border-l-transparent text-foreground hover:bg-accent/50'
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
    </div>
  )
}
