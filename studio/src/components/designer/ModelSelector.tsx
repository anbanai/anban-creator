import { DESIGNER_MODELS } from '@/types/designer'
import { getModelCapabilities } from '@/types/designer'
import { Badge } from '@/components/ui/badge'
import type { ModelCapabilities } from '@/types/designer'

interface ModelSelectorProps {
  selectedModel: string
  onModelChange: (modelId: string) => void
}

export default function ModelSelector({ selectedModel, onModelChange }: ModelSelectorProps) {
  return (
    <div className="flex flex-wrap gap-2">
      {DESIGNER_MODELS.map((model) => {
        const isActive = selectedModel === model.id
        const caps: ModelCapabilities | undefined = getModelCapabilities(model.id)
        return (
          <button
            key={model.id}
            type="button"
            onClick={() => onModelChange(model.id)}
            className={`flex flex-col items-start gap-1 rounded-lg border px-3 py-2 text-left transition-all ${
              isActive
                ? 'border-primary bg-primary/5 ring-1 ring-primary/30'
                : 'border-border bg-card hover:border-primary/40 hover:bg-accent/50'
            }`}
          >
            <span className="text-sm font-medium text-foreground">{model.name}</span>
            <span className="text-xs text-muted-foreground">{model.description}</span>
            {caps && (
              <div className="flex flex-wrap gap-1">
                {caps.batch && <Badge variant="secondary" className="text-[10px]">批量</Badge>}
                {caps.inpainting && <Badge variant="secondary" className="text-[10px]">局部编辑</Badge>}
                {caps.streaming && <Badge variant="secondary" className="text-[10px]">流式</Badge>}
              </div>
            )}
          </button>
        )
      })}
    </div>
  )
}
