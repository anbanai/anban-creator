import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
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
}

export function ImageGenerationSettings({
  ratios,
  ratio,
  onRatioChange,
  capabilities,
  capabilityKey,
  onCapabilityChange,
  disabled = false,
}: ImageGenerationSettingsProps) {
  return (
    <>
      <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
        <h3 className="pt-1 font-medium">图片比例</h3>
        <ToggleGroup
          aria-label="图片比例"
          value={[ratio || 'auto']}
          onValueChange={(next) => {
            const selected = next[0]
            if (selected) onRatioChange(selected as ImageRatio)
          }}
          variant="outline"
          size="sm"
          spacing={1}
          disabled={disabled}
          className="flex min-w-0 flex-nowrap justify-start"
        >
          <ToggleGroupItem value="auto">智能适配</ToggleGroupItem>
          {ratios.map((item) => (
            <ToggleGroupItem key={item} value={item}>{item}</ToggleGroupItem>
          ))}
        </ToggleGroup>
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium">图像能力</h3>
        <ToggleGroup
          aria-label="图像能力"
          value={capabilityKey ? [capabilityKey] : []}
          onValueChange={(next) => {
            const selected = next[0]
            if (selected) onCapabilityChange(selected)
          }}
          variant="outline"
          disabled={disabled}
          className="flex w-full flex-col items-stretch"
        >
          {capabilities.map((option) => {
            const available = option.enabled === true && option.price_available === true
            return (
              <ToggleGroupItem
                key={option.key}
                value={option.key}
                disabled={!available}
                className="h-auto min-h-16 w-full min-w-0 flex-col items-start justify-center gap-0.5 whitespace-normal px-3 py-2 text-left"
              >
                <span className="font-medium">{option.display_name}</span>
                {option.description ? <span className="text-xs text-muted-foreground">{option.description}</span> : null}
                <span className="text-xs text-muted-foreground">
                  {available && typeof option.price_credits === 'number'
                    ? `每张 ${option.price_credits.toLocaleString()} 积分`
                    : '价格暂不可用'}
                </span>
              </ToggleGroupItem>
            )
          })}
        </ToggleGroup>
      </section>
    </>
  )
}
