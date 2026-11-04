import { ImageIcon } from 'lucide-react'
import { Input } from '@/components/ui/input'
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

const CAPABILITY_LABELS: Record<string, string> = {
  standard: '标准',
  professional: '增强',
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
  return (
    <fieldset aria-label="创作参数" disabled={disabled || !selectedCapability} className="min-w-0 space-y-3 text-xs disabled:opacity-50">
      <section className="space-y-1.5">
        <h3 className="font-medium text-muted-foreground">图像能力</h3>
        <ToggleGroup
          aria-label="图像能力"
          value={capabilityKey ? [capabilityKey] : []}
          onValueChange={(next) => {
            if (next[0]) onCapabilityChange(next[0])
          }}
          className="grid w-full grid-cols-2 gap-1.5"
        >
          {capabilities.map((capability) => (
            <ToggleGroupItem
              key={capability.id}
              value={capability.id}
              disabled={!capability.enabled || capability.priceAvailable !== true}
              className="h-12 w-full min-w-0 flex-col gap-1 rounded-lg border border-border/70 px-2 hover:border-primary/30 aria-pressed:border-primary/40 aria-pressed:bg-primary/5 aria-pressed:text-primary"
            >
              <span className="text-[13px] font-medium">{CAPABILITY_LABELS[capability.id] ?? capability.name}</span>
              <span className="text-[11px] font-normal text-muted-foreground tabular-nums">{capability.priceAvailable === true ? `${capability.credits.toLocaleString()} 积分/张` : '价格暂不可用'}</span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </section>

      <section className="space-y-1.5">
        <h3 className="font-medium text-muted-foreground">画面尺寸</h3>
        <ToggleGroup
          aria-label="尺寸"
          value={settings.size ? [settings.size] : []}
          onValueChange={(next) => {
            if (next[0]) onSettingsChange({ size: next[0] })
          }}
          size="sm"
          className="grid w-full min-w-0 grid-cols-[repeat(auto-fit,minmax(2.5rem,1fr))] gap-1.5"
        >
          {sizePresets.map((size) => {
            const preset = size.match(/^(\d+):(\d+):(1K|2K|4K)$/i)
            return (
              <ToggleGroupItem
                key={size}
                value={size}
                aria-label={settingLabel(size)}
                className="h-14 min-w-0 flex-col gap-1 rounded-lg border border-border/70 px-1 text-muted-foreground hover:border-primary/30 aria-pressed:border-primary/50 aria-pressed:bg-primary/5 aria-pressed:text-primary"
              >
                <span aria-hidden="true" className="flex h-5 items-center justify-center">
                  {preset ? (
                    <span
                      className="rounded-[3px] border border-current bg-current/5"
                      style={{ width: 18 * Math.min(Number(preset[1]) / Number(preset[2]), 1), height: 18 * Math.min(Number(preset[2]) / Number(preset[1]), 1) }}
                    />
                  ) : <ImageIcon className="size-5" />}
                </span>
                <span className="text-[11px] font-medium">{preset ? `${preset[1]}:${preset[2]}` : size}</span>
              </ToggleGroupItem>
            )
          })}
        </ToggleGroup>
      </section>

      {qualityLevels.length > 0 ? (
        <section className="space-y-1.5">
          <h3 className="font-medium text-muted-foreground">质量</h3>
          <ToggleGroup
            aria-label="质量"
            value={settings.quality ? [settings.quality] : []}
            onValueChange={(next) => {
              if (next[0]) onSettingsChange({ quality: next[0] })
            }}
            size="sm"
            className="grid w-full min-w-0 auto-cols-fr grid-flow-col gap-1 rounded-lg bg-muted/70 p-1"
          >
            {qualityLevels.map((quality) => (
              <ToggleGroupItem key={quality} value={quality} className="h-7 min-w-0 rounded-md px-1 text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">{QUALITY_LABELS[quality] ?? quality}</ToggleGroupItem>
            ))}
          </ToggleGroup>
        </section>
      ) : null}

      <section className="space-y-1.5">
        <h3 className="font-medium text-muted-foreground">输出格式</h3>
        <ToggleGroup
          aria-label="输出格式"
          value={settings.outputFormat ? [settings.outputFormat] : []}
          onValueChange={(next) => {
            if (next[0]) onSettingsChange({ outputFormat: next[0] })
          }}
          size="sm"
          className="grid w-full min-w-0 auto-cols-fr grid-flow-col gap-1 rounded-lg bg-muted/70 p-1"
        >
          {outputFormats.map((format) => (
            <ToggleGroupItem key={format} value={format} className="h-7 min-w-0 rounded-md px-1 text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">{format.toUpperCase()}</ToggleGroupItem>
          ))}
        </ToggleGroup>
      </section>

      <div className="flex items-center justify-between text-muted-foreground">
        <span>生成数量</span>
        <div className="flex items-center gap-1.5"><Input type="number" aria-label="生成数量" min={1} max={4} step={1} value={settings.n} onChange={(event) => onSettingsChange({ n: Math.min(4, Math.max(1, Math.trunc(Number(event.target.value)) || 1)) })} className="h-7 w-16 px-2 text-xs tabular-nums" /><span className="text-[11px]">张</span></div>
      </div>
    </fieldset>
  )
}
