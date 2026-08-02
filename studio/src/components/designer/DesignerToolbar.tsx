import { useQuery } from '@tanstack/react-query'
import { Coins, History, SlidersHorizontal, Sparkles, Stamp } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Slider } from '@/components/ui/slider'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import type { DesignerCapabilityFeatures, DesignerSettings } from '@/types/designer'

export type DesignerSettingsPatch = Partial<DesignerSettings>

interface AdvancedSettingsProps {
  designerFeatures?: DesignerCapabilityFeatures
  settings: DesignerSettings
  onSettingsChange: (patch: DesignerSettingsPatch) => void
}

function AdvancedSettings({ designerFeatures, settings, onSettingsChange }: AdvancedSettingsProps) {
  if (!designerFeatures) return null
  return (
    <div className="space-y-3">
      {designerFeatures.watermark ? (
        <button
          type="button"
          onClick={() => onSettingsChange({ watermark: !settings.watermark })}
          className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
            settings.watermark ? 'border-primary bg-primary/5' : 'border-border hover:border-foreground/20'
          }`}
        >
          <Stamp className={`mt-0.5 h-4 w-4 shrink-0 ${settings.watermark ? 'text-primary' : 'text-muted-foreground'}`} />
          <span className="min-w-0">
            <span className={`block text-xs font-medium ${settings.watermark ? 'text-foreground' : 'text-muted-foreground'}`}>水印</span>
            <span className="block text-[10px] text-muted-foreground">添加水印</span>
          </span>
        </button>
      ) : null}

      {designerFeatures.hasCompression && (settings.outputFormat === 'jpeg' || settings.outputFormat === 'webp') ? (
        <div className="space-y-2">
          <p className="flex items-center gap-1.5 text-[11px] font-semibold uppercase text-muted-foreground">
            <SlidersHorizontal className="h-3 w-3" />
            压缩率
          </p>
          <div className="flex items-center gap-3">
            <Slider
              value={[settings.compression]}
              onValueChange={(value) => onSettingsChange({ compression: Array.isArray(value) ? value[0] : value })}
              min={0}
              max={100}
              step={1}
              className="flex-1"
            />
            <span className="w-9 text-center text-xs font-medium tabular-nums">{settings.compression}%</span>
          </div>
        </div>
      ) : null}

      {designerFeatures.hasBackground ? (
        <div className="space-y-2">
          <p className="text-[11px] font-semibold uppercase text-muted-foreground">背景</p>
          <ToggleGroup
            aria-label="背景"
            value={[settings.background]}
            onValueChange={(value) => {
              if (value[0]) onSettingsChange({ background: value[0] })
            }}
            variant="outline"
            size="sm"
            className="w-full"
          >
            <ToggleGroupItem value="auto">自动</ToggleGroupItem>
            <ToggleGroupItem value="opaque">不透明</ToggleGroupItem>
          </ToggleGroup>
        </div>
      ) : null}
    </div>
  )
}

interface DesignerToolbarProps {
  designerFeatures?: DesignerCapabilityFeatures
  settings: DesignerSettings
  onSettingsChange: (patch: DesignerSettingsPatch) => void
  onHistoryToggle: () => void
}

export default function DesignerToolbar({
  designerFeatures,
  settings,
  onSettingsChange,
  onHistoryToggle,
}: DesignerToolbarProps) {
  const { data: balanceData, isLoading: balanceLoading, isError: balanceError } = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })

  return (
    <>
      <div className="hidden w-[280px] shrink-0 p-3 md:block">
        <aside className="flex h-full flex-col overflow-hidden rounded-xl border border-border/75 bg-card/80">
          <div className="flex-1 space-y-3 overflow-y-auto p-3">
            <div className="flex items-center justify-between px-1">
              <div className="flex items-center gap-2.5">
                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-muted">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                </div>
                <span className="text-sm font-semibold">设计师</span>
              </div>
              <Tooltip>
                <TooltipTrigger>
                  <Badge variant="secondary" className="gap-1 text-[11px] font-medium">
                    <Coins className="h-3 w-3" />
                    {balanceLoading ? <Skeleton className="h-3 w-8" /> : balanceError ? '—' : balanceData?.balance.toLocaleString() ?? '—'}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>{balanceError ? '积分余额暂不可用' : '积分余额'}</TooltipContent>
              </Tooltip>
            </div>
            <Separator />
            <AdvancedSettings designerFeatures={designerFeatures} settings={settings} onSettingsChange={onSettingsChange} />
          </div>
          <div className="shrink-0 p-3">
            <Separator className="mb-3" />
            <button
              type="button"
              onClick={onHistoryToggle}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              <History className="h-3.5 w-3.5" />
              历史记录
            </button>
          </div>
        </aside>
      </div>

      <div className="flex shrink-0 items-center justify-end border-t border-border/50 bg-card/60 px-3 py-2 md:hidden">
        <button
          type="button"
          onClick={onHistoryToggle}
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
        >
          <History className="h-3.5 w-3.5" />
          历史记录
        </button>
      </div>
    </>
  )
}
