import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronRight, Coins, History, Sparkles } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Slider } from '@/components/ui/slider'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useIsMobile } from '@/hooks/use-mobile'
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
  const showCompression = designerFeatures.hasCompression && (settings.outputFormat === 'jpeg' || settings.outputFormat === 'webp')
  if (!designerFeatures.watermark && !designerFeatures.hasBackground && !showCompression) return null
  return (
    <div className="space-y-3">
      {designerFeatures.watermark ? (
        <button
          type="button"
          aria-pressed={settings.watermark}
          onClick={() => onSettingsChange({ watermark: !settings.watermark })}
          className="flex w-full items-center justify-between gap-3 rounded-md text-left text-xs font-medium text-muted-foreground"
        >
          <span className="font-semibold text-foreground">添加水印</span>
          <span aria-hidden="true" className={`flex h-5 w-9 items-center rounded-full p-0.5 transition-colors ${settings.watermark ? 'bg-primary' : 'bg-muted'}`}>
            <span className={`size-4 rounded-full bg-background shadow-sm transition-transform ${settings.watermark ? 'translate-x-4' : ''}`} />
          </span>
        </button>
      ) : null}

      {showCompression ? (
        <div className="space-y-1.5">
          <p className="flex items-center justify-between text-xs">
            <span className="font-semibold text-foreground">压缩率</span>
            <span className="text-[11px] text-muted-foreground tabular-nums">{settings.compression}%</span>
          </p>
          <Slider
            aria-label="压缩率"
            value={[settings.compression]}
            onValueChange={(value) => onSettingsChange({ compression: Array.isArray(value) ? value[0] : value })}
            min={0}
            max={100}
            step={1}
          />
        </div>
      ) : null}

      {designerFeatures.hasBackground ? (
        <div className="space-y-1.5">
          <p className="text-xs font-semibold text-foreground">背景</p>
          <ToggleGroup
            aria-label="背景"
            value={[settings.background]}
            onValueChange={(value) => {
              if (value[0]) onSettingsChange({ background: value[0] })
            }}
            size="sm"
            className="grid w-full grid-cols-3 gap-1 rounded-lg bg-muted/70 p-0.5"
          >
            <ToggleGroupItem value="auto" className="h-6 min-w-0 rounded-md px-1 text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">自动</ToggleGroupItem>
            <ToggleGroupItem value="transparent" className="h-6 min-w-0 rounded-md px-1 text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">透明</ToggleGroupItem>
            <ToggleGroupItem value="opaque" className="h-6 min-w-0 rounded-md px-1 text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">不透明</ToggleGroupItem>
          </ToggleGroup>
          {settings.background === 'transparent' && (
            <div className="space-y-1.5 pt-1">
              <p className="flex items-center justify-between text-xs font-semibold text-foreground">
                背景透明度
                <span className="font-normal tabular-nums">{100 - (settings.backgroundOpacity ?? 0)}%</span>
              </p>
              <Slider
                aria-label="背景透明度"
                value={[100 - (settings.backgroundOpacity ?? 0)]}
                onValueChange={(value) => onSettingsChange({ backgroundOpacity: 100 - (Array.isArray(value) ? value[0] : value) })}
                min={0}
                max={100}
                step={1}
              />
              <div className="flex justify-between text-[10px] text-muted-foreground"><span>白底</span><span>全透明</span></div>
              {settings.outputFormat === 'jpeg' && <p className="text-[10px] text-muted-foreground">JPEG 导出为白底</p>}
            </div>
          )}
        </div>
      ) : null}
    </div>
  )
}

interface DesignerToolbarProps {
  children?: ReactNode
  designerFeatures?: DesignerCapabilityFeatures
  settings: DesignerSettings
  onSettingsChange: (patch: DesignerSettingsPatch) => void
  onHistoryToggle: () => void
}

export default function DesignerToolbar({
  children,
  designerFeatures,
  settings,
  onSettingsChange,
  onHistoryToggle,
}: DesignerToolbarProps) {
  const isMobile = useIsMobile()
  const [mobileSettingsOpen, setMobileSettingsOpen] = useState(false)
  const { data: balanceData, isLoading: balanceLoading, isError: balanceError } = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })

  return (
    <>
      <div className="hidden w-[304px] shrink-0 p-3 md:block">
        <aside aria-label="设计师设置" className="flex h-full flex-col overflow-hidden rounded-2xl border border-border/70 bg-card shadow-sm">
          <div className="flex shrink-0 items-center justify-between border-b border-border/60 p-3">
            <div className="flex items-center gap-2.5">
              <div className="flex size-8 items-center justify-center rounded-xl bg-primary/8">
                <Sparkles className="size-4 text-primary" />
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
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-3">
            {children}
            {!isMobile && <AdvancedSettings designerFeatures={designerFeatures} settings={settings} onSettingsChange={onSettingsChange} />}
          </div>
          <div className="shrink-0 border-t border-border/60 p-3">
            <button
              type="button"
              onClick={onHistoryToggle}
              className="flex w-full items-center gap-2 rounded-lg bg-muted/40 px-3 py-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              <History className="h-3.5 w-3.5" />
              历史记录
              <ChevronRight className="ml-auto size-3.5" />
            </button>
          </div>
        </aside>
      </div>

      <div className="flex shrink-0 items-center justify-between border-t border-border/50 bg-card/60 px-3 py-2 md:hidden">
        <details className="relative text-xs" onToggle={(event) => setMobileSettingsOpen(event.currentTarget.open)}>
          <summary className="cursor-pointer text-muted-foreground">创作参数</summary>
          {mobileSettingsOpen && <div className="absolute left-0 top-6 z-20 max-h-[60dvh] w-[min(20rem,calc(100vw-1.5rem))] space-y-4 overflow-y-auto rounded-xl border bg-popover p-4 shadow-lg">
            {children}
            <AdvancedSettings designerFeatures={designerFeatures} settings={settings} onSettingsChange={onSettingsChange} />
          </div>}
        </details>
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
