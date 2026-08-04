import { ChevronDownIcon, SlidersHorizontalIcon } from 'lucide-react'

import { QuantityStepper } from '@/components/agent-prompt/QuantityStepper'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { DesignerCapability, DesignerSettings } from '@/types/designer'
import type { DesignerSettingsPatch } from './DesignerToolbar'

interface DesignerGenerationToolbarProps {
  capabilities: DesignerCapability[]
  capabilityKey: string
  settings: DesignerSettings
  onCapabilityChange: (key: string) => void
  onSettingsChange: (patch: DesignerSettingsPatch) => void
  disabled?: boolean
}

const QUALITY_LABELS: Record<string, string> = {
  auto: '自动',
  low: '低',
  medium: '中',
  high: '高',
}

function settingLabel(value: string) {
  const fixedPreset = value.match(/^(\d+):(\d+):(1K|2K|4K)$/i)
  if (fixedPreset) {
    return `${fixedPreset[1]}:${fixedPreset[2]} · ${fixedPreset[3].toUpperCase()}`
  }
  return value
}

export function DesignerGenerationToolbar({
  capabilities,
  capabilityKey,
  settings,
  onCapabilityChange,
  onSettingsChange,
  disabled = false,
}: DesignerGenerationToolbarProps) {
  const selectedCapability = capabilities.find((capability) => capability.id === capabilityKey)
  const designerFeatures = selectedCapability?.designerFeatures
  const qualityLevels = designerFeatures?.qualityLevels ?? []
  const sizePresets = designerFeatures?.sizePresets ?? []
  const outputFormats = designerFeatures?.outputFormats ?? []
  const maxBatch = Math.max(1, designerFeatures?.maxBatch ?? 1)
  const quantity = Math.min(maxBatch, Math.max(1, settings.n))
  const summary = [
    selectedCapability?.name ?? '图像能力',
    settingLabel(settings.size),
    qualityLevels.length > 0 ? QUALITY_LABELS[settings.quality] ?? settings.quality : undefined,
    settings.outputFormat.toUpperCase(),
    `${quantity} 张`,
  ].filter(Boolean).join(' · ')

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled || !selectedCapability}
            aria-label={`创作参数：${summary}`}
            className="max-w-full gap-1.5 px-2 text-muted-foreground"
          />
        )}
      >
        <SlidersHorizontalIcon data-icon="inline-start" />
        <span>创作参数</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent align="start" className="max-h-[var(--available-height)] w-[min(30rem,calc(100vw-2rem))] gap-3 overflow-y-auto p-4">
        <PopoverTitle className="sr-only">创作参数</PopoverTitle>
        <section className="flex flex-col gap-2">
          <h3 className="font-medium">图像能力</h3>
          <ToggleGroup
            aria-label="图像能力"
            value={capabilityKey ? [capabilityKey] : []}
            onValueChange={(next) => {
              if (next[0]) onCapabilityChange(next[0])
            }}
            variant="outline"
            className="flex w-full flex-col items-stretch"
          >
            {capabilities.map((capability) => (
              <ToggleGroupItem
                key={capability.id}
                value={capability.id}
                disabled={!capability.enabled || capability.priceAvailable !== true}
                className="h-auto min-h-14 w-full min-w-0 items-start justify-between gap-3 whitespace-normal px-3 py-2 text-left"
              >
                <span className="min-w-0">
                  <span className="block font-medium">{capability.name}</span>
                  {capability.description ? <span className="block text-xs text-muted-foreground">{capability.description}</span> : null}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">每张 {capability.credits.toLocaleString()} 积分</span>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </section>

        <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
          <h3 className="pt-1 font-medium">尺寸</h3>
          <ToggleGroup
            aria-label="尺寸"
            value={settings.size ? [settings.size] : []}
            onValueChange={(next) => {
              if (next[0]) onSettingsChange({ size: next[0] })
            }}
            variant="outline"
            size="sm"
            className="flex min-w-0 flex-wrap justify-start"
          >
            {sizePresets.map((size) => (
              <ToggleGroupItem key={size} value={size}>{settingLabel(size)}</ToggleGroupItem>
            ))}
          </ToggleGroup>
        </section>

        {qualityLevels.length > 0 ? (
          <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
            <h3 className="pt-1 font-medium">质量</h3>
            <ToggleGroup
              aria-label="质量"
              value={settings.quality ? [settings.quality] : []}
              onValueChange={(next) => {
                if (next[0]) onSettingsChange({ quality: next[0] })
              }}
              variant="outline"
              size="sm"
              className="flex min-w-0 flex-wrap justify-start"
            >
              {qualityLevels.map((quality) => (
                <ToggleGroupItem key={quality} value={quality}>{QUALITY_LABELS[quality] ?? quality}</ToggleGroupItem>
              ))}
            </ToggleGroup>
          </section>
        ) : null}

        <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
          <h3 className="pt-1 font-medium">输出格式</h3>
          <ToggleGroup
            aria-label="输出格式"
            value={settings.outputFormat ? [settings.outputFormat] : []}
            onValueChange={(next) => {
              if (next[0]) onSettingsChange({ outputFormat: next[0] })
            }}
            variant="outline"
            size="sm"
            className="flex min-w-0 flex-wrap justify-start"
          >
            {outputFormats.map((format) => (
              <ToggleGroupItem key={format} value={format}>{format.toUpperCase()}</ToggleGroupItem>
            ))}
          </ToggleGroup>
        </section>

        <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2">
          <h3 className={maxBatch === 1 ? 'sr-only' : 'font-medium'}>图片数量</h3>
          <div className="flex min-w-0 justify-end">
            <QuantityStepper
              label="图片数量"
              value={quantity}
              min={1}
              max={maxBatch}
              onChange={(n) => onSettingsChange({ n })}
              disabled={disabled}
            />
          </div>
        </section>
      </PopoverContent>
    </Popover>
  )
}
