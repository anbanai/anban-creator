import { ChevronDownIcon, ImageIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { ImageRatio } from '@/lib/schemas'
import type { ImageCapabilityOption } from '@/types/imageCapability'

interface ImageGenerationToolbarProps {
  ratios: string[]
  ratio: string
  onRatioChange: (value: ImageRatio) => void
  capabilities: ImageCapabilityOption[]
  capabilityKey: string
  onCapabilityChange: (value: string) => void
  loading?: boolean
  disabled?: boolean
}

export function ImageGenerationToolbar({
  ratios,
  ratio,
  onRatioChange,
  capabilities,
  capabilityKey,
  onCapabilityChange,
  loading = false,
  disabled = false,
}: ImageGenerationToolbarProps) {
  const selectedCapability = capabilities.find((option) => option.key === capabilityKey)
  const ratioLabel = ratio === 'auto' ? '智能适配' : ratio
  const capabilityLabel = selectedCapability?.display_name ?? '图像能力'
  const summary = `${ratioLabel} · ${capabilityLabel}`

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled || loading}
            aria-label={`图像设置：${summary}`}
            className="max-w-full gap-1.5 px-2 text-muted-foreground"
          />
        )}
      >
        <ImageIcon data-icon="inline-start" />
        <span className="truncate">{loading ? '加载图像设置...' : summary}</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(22rem,calc(100vw-2rem))] gap-4 p-4">
        <section className="flex flex-col gap-2">
          <PopoverTitle>图片比例</PopoverTitle>
          <ToggleGroup
            aria-label="图片比例"
            value={[ratio || 'auto']}
            onValueChange={(next) => {
              const selected = next[0]
              if (selected) onRatioChange(selected as ImageRatio)
            }}
            variant="outline"
            size="sm"
            className="flex w-full flex-wrap justify-start"
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
      </PopoverContent>
    </Popover>
  )
}
