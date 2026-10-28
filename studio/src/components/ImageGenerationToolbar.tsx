import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Separator } from '@/components/ui/separator'
import { Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { ImageRatio } from '@/lib/schemas'
import type { ImageCapabilityOption } from '@/types/imageCapability'

export interface ImageGenerationSettingsProps {
  ratios: string[]
  ratio: string
  onRatioChange: (value: ImageRatio) => void
  capabilities: ImageCapabilityOption[]
  capabilityKey: string
  onCapabilityChange: (value: string) => void
  loading?: boolean
  disabled?: boolean
  ratioLabel?: string
}

export function ImageGenerationSettings({
  ratios,
  ratio,
  onRatioChange,
  capabilities,
  capabilityKey,
  onCapabilityChange,
  disabled = false,
  ratioLabel = '图片比例',
}: ImageGenerationSettingsProps) {
  const selectedCapability = capabilities.find((option) => option.key === capabilityKey)

  return (
    <>
      <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
        <h3 className="pt-1 font-medium">{ratioLabel}</h3>
        <ToggleGroup
          aria-label={ratioLabel}
          value={[ratio || 'auto']}
          onValueChange={(next) => {
            const selected = next[0]
            if (selected) onRatioChange(selected as ImageRatio)
          }}
          variant="outline"
          size="sm"
          spacing={1}
          disabled={disabled}
          className="flex min-w-0 flex-wrap justify-start"
        >
          <ToggleGroupItem value="auto">智能适配</ToggleGroupItem>
          {ratios.map((item) => (
            <ToggleGroupItem key={item} value={item}>{item}</ToggleGroupItem>
          ))}
        </ToggleGroup>
      </section>

      <Separator />

      <section className="flex flex-col gap-1">
        <h3 className="font-medium">图像能力</h3>
        <div className="flex min-w-0 flex-col gap-1.5">
          {selectedCapability?.description ? (
            <p aria-live="polite" className="min-h-5 text-xs leading-5 text-muted-foreground">{selectedCapability.description}</p>
          ) : null}
          <ToggleGroup
            aria-label="图像能力"
            value={capabilityKey ? [capabilityKey] : []}
            onValueChange={(next) => {
              const selected = next[0]
              if (selected) onCapabilityChange(selected)
            }}
            variant="default"
            disabled={disabled}
            className="flex w-full flex-wrap justify-start gap-1"
          >
            {capabilities.map((option) => {
              const available = option.enabled === true && option.price_available === true
              return (
                <ToggleGroupItem
                  key={option.key}
                  value={option.key}
                  disabled={!available}
                  title={option.description}
                  className="h-8 gap-1.5 px-2 text-xs aria-pressed:bg-primary/8 aria-pressed:text-primary data-[state=on]:bg-primary/8 data-[state=on]:text-primary"
                >
                  <Check aria-hidden="true" className={cn('size-3', capabilityKey !== option.key && 'invisible')} />
                  <span>{option.display_name}</span>
                </ToggleGroupItem>
              )
            })}
          </ToggleGroup>
        </div>
      </section>
    </>
  )
}
