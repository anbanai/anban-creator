import { useRef, useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Proportions,
  Layers,
  ImagePlus,
  Paintbrush,
  History,
  X,
  Upload,
  Sparkles,
  Maximize,
  Coins,
  Lock,
  Stamp,
} from 'lucide-react'
import { Slider } from '@/components/ui/slider'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerSettings, DesignerProvider } from '@/types/designer'
import { api } from '@/lib/api'

const SIZE_OPTIONS = [
  { value: '1:1', label: '1:1', desc: '方形' },
  { value: '16:9', label: '16:9', desc: '横屏' },
  { value: '9:16', label: '9:16', desc: '竖屏' },
  { value: '4:3', label: '4:3', desc: '横屏' },
  { value: '3:4', label: '3:4', desc: '竖屏' },
]

const RESOLUTION_OPTIONS = [
  { value: '1K', label: '1K', desc: '标准' },
  { value: '2K', label: '2K', desc: '高清' },
  { value: '4K', label: '4K', desc: '超清' },
]

interface DesignerToolbarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
  provider: string
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
  onHistoryToggle: () => void
}

export default function DesignerToolbar({
  providers,
  selectedProviderId,
  onModelChange,
  provider,
  settings,
  onSettingsChange,
  onHistoryToggle,
}: DesignerToolbarProps) {
  const caps = getModelCapabilities(provider)
  const refInputRef = useRef<HTMLInputElement>(null)
  const maskInputRef = useRef<HTMLInputElement>(null)

  // Fetch user credit balance
  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })
  const balance = balanceData?.balance ?? 0

  function update(patch: Partial<DesignerSettings>) {
    onSettingsChange({ ...settings, ...patch })
  }

  function handleRefFiles(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files ?? [])
    const max = caps?.maxRefImages ?? 0
    const combined = [...settings.referenceFiles, ...files].slice(0, max)
    update({ referenceFiles: combined })
    e.target.value = ''
  }

  function removeRefFile(index: number) {
    update({ referenceFiles: settings.referenceFiles.filter((_, i) => i !== index) })
  }

  function handleMaskFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0] ?? null
    update({ maskFile: file })
    e.target.value = ''
  }

  const showCount = caps?.batch
  const showQuality = (caps?.qualityLevels.length ?? 0) > 0
  const showFormat = (caps?.outputFormats.length ?? 0) > 1
  const showRefs = (caps?.maxRefImages ?? 0) > 0
  const showMask = caps?.inpainting

  // Generate object URLs for reference image previews
  const [refPreviewUrls, setRefPreviewUrls] = useState<string[]>([])
  useEffect(() => {
    const urls = settings.referenceFiles.map((f) => URL.createObjectURL(f))
    setRefPreviewUrls(urls)
    return () => urls.forEach((u) => URL.revokeObjectURL(u))
  }, [settings.referenceFiles])

  const sectionHeader = 'flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70'

  return (
    <>
      {/* Desktop: floating glass sidebar */}
      <div className="hidden w-[280px] shrink-0 p-3 md:block">
        <aside className="relative flex h-full flex-col overflow-hidden rounded-2xl border border-white/[0.08] bg-background/50 shadow-[0_8px_40px_-12px_rgba(0,0,0,0.12),0_0_0_1px_rgba(255,255,255,0.05)] backdrop-blur-2xl dark:border-white/[0.04] dark:bg-background/40 dark:shadow-[0_8px_40px_-12px_rgba(0,0,0,0.4),0_0_0_1px_rgba(255,255,255,0.03)]">
          {/* Top accent line */}
          <div className="relative h-px shrink-0 bg-gradient-to-r from-transparent via-primary/50 to-transparent" />

          {/* Scrollable content */}
          <div className="flex-1 overflow-y-auto p-3 space-y-3">
            {/* Brand header + credits balance */}
            <div className="flex items-center justify-between px-1">
              <div className="flex items-center gap-2.5">
                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-chart-2/20">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                </div>
                <span className="bg-gradient-to-r from-primary via-chart-2 to-chart-4 bg-clip-text text-sm font-bold tracking-wide text-transparent">
                  Designer
                </span>
              </div>
              <Tooltip>
                <TooltipTrigger>
                  <Badge variant="secondary" className="gap-1 text-[11px] font-medium">
                    <Coins className="h-3 w-3" />
                    {balanceLoading ? <Skeleton className="h-3 w-8" /> : balance.toLocaleString()}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>积分余额</TooltipContent>
              </Tooltip>
            </div>

            {/* Model selector */}
            <div className="space-y-1">
              {providers.map((p) => {
                const isActive = selectedProviderId === p.id && p.enabled
                const isDisabled = !p.enabled
                const pCaps = getModelCapabilities(p.provider)
                return (
                  <button
                    key={p.id}
                    type="button"
                    disabled={isDisabled}
                    onClick={() => !isDisabled && onModelChange(p.id)}
                    className={`group relative flex w-full flex-col items-start gap-0.5 rounded-xl px-3 py-2.5 text-left transition-all duration-300 ${
                      isDisabled
                        ? 'cursor-not-allowed opacity-40'
                        : isActive
                          ? 'bg-primary/[0.07] ring-1 ring-primary/20 dark:bg-primary/[0.12]'
                          : 'hover:bg-muted/40'
                    }`}
                    style={isActive ? { boxShadow: '0 0 12px -4px var(--color-primary)' } : undefined}
                  >
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium">{p.name}</span>
                      {isActive && (
                        <span className="h-1.5 w-1.5 rounded-full bg-primary animate-glow-pulse" style={{ boxShadow: '0 0 6px var(--color-primary)' }} />
                      )}
                      {isDisabled && (
                        <Badge variant="outline" className="h-4 gap-0.5 px-1.5 text-[9px] text-muted-foreground">
                          <Lock className="h-2.5 w-2.5" />
                          未启用
                        </Badge>
                      )}
                      {!isDisabled && p.credits > 0 && (
                        <Tooltip>
                          <TooltipTrigger>
                            <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">
                              {p.credits}
                            </Badge>
                          </TooltipTrigger>
                          <TooltipContent>每次生成消耗 {p.credits} 积分</TooltipContent>
                        </Tooltip>
                      )}
                    </div>
                    {pCaps && !isDisabled && (
                      <div className="mt-0.5 flex gap-1">
                        {pCaps.batch && <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">批量</Badge>}
                        {pCaps.inpainting && <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">局部编辑</Badge>}
                      </div>
                    )}
                  </button>
                )
              })}
            </div>

            <Separator />

            {/* Size */}
            <div className="space-y-2">
              <h4 className={sectionHeader}>
                <Proportions className="h-3 w-3" />
                尺寸
              </h4>
              <div className="grid grid-cols-3 gap-1">
                {SIZE_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => update({ size: opt.value })}
                    className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
                      settings.size === opt.value
                        ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                        : 'bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
                    }`}
                  >
                    <span className="font-medium">{opt.label}</span>
                    <span className="text-[10px] opacity-60">{opt.desc}</span>
                  </button>
                ))}
              </div>
            </div>

            {/* Resolution */}
            <div className="space-y-2">
              <h4 className={sectionHeader}>
                <Maximize className="h-3 w-3" />
                分辨率
              </h4>
              <div className="grid grid-cols-3 gap-1">
                {RESOLUTION_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => update({ resolution: opt.value })}
                    className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
                      settings.resolution === opt.value
                        ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                        : 'bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
                    }`}
                  >
                    <span className="font-medium">{opt.label}</span>
                    <span className="text-[10px] opacity-60">{opt.desc}</span>
                  </button>
                ))}
              </div>
            </div>

            {/* Watermark */}
            {caps?.watermark && <button
              type="button"
              onClick={() => update({ watermark: !settings.watermark })}
              className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
                settings.watermark
                  ? 'border-primary bg-primary/5'
                  : 'border-border hover:border-foreground/20'
              }`}
            >
              <Stamp className={`mt-0.5 h-4 w-4 shrink-0 ${settings.watermark ? 'text-primary' : 'text-muted-foreground'}`} />
              <div className="min-w-0">
                <p className={`text-xs font-medium ${settings.watermark ? 'text-foreground' : 'text-muted-foreground'}`}>
                  水印
                </p>
                <p className="text-[10px] text-muted-foreground">添加水印</p>
              </div>
            </button>}

            {/* Count */}
            {showCount && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>
                  <Layers className="h-3 w-3" />
                  数量
                </h4>
                <div className="flex items-center gap-3">
                  <Slider
                    value={[settings.n]}
                    onValueChange={(val) => update({ n: Array.isArray(val) ? val[0] : val })}
                    min={1}
                    max={caps!.maxBatch}
                    step={1}
                    className="flex-1"
                  />
                  <span className="w-6 text-center text-xs font-bold tabular-nums text-foreground">{settings.n}</span>
                </div>
              </div>
            )}

            {/* Quality */}
            {showQuality && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>质量</h4>
                <div className="flex flex-wrap gap-1">
                  {caps!.qualityLevels.map((level) => (
                    <button
                      key={level}
                      type="button"
                      onClick={() => update({ quality: level })}
                      className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-all duration-200 ${
                        settings.quality === level
                          ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                          : 'bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
                      }`}
                    >
                      {level === 'auto' ? '自动' : level === 'low' ? '低' : level === 'medium' ? '中' : '高'}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* Output Format */}
            {showFormat && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>格式</h4>
                <div className="flex flex-wrap gap-1">
                  {caps!.outputFormats.map((fmt) => (
                    <button
                      key={fmt}
                      type="button"
                      onClick={() => update({ outputFormat: fmt })}
                      className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-all duration-200 ${
                        settings.outputFormat === fmt
                          ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                          : 'bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
                      }`}
                    >
                      {fmt.toUpperCase()}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* References */}
            {showRefs && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>
                  <ImagePlus className="h-3 w-3" />
                  参考图 ({settings.referenceFiles.length}/{caps!.maxRefImages})
                </h4>
                <div className="space-y-1.5">
                  {refPreviewUrls.length > 0 && (
                    <div className="grid grid-cols-3 gap-1.5">
                      {refPreviewUrls.map((url, i) => (
                        <div key={url} className="group relative aspect-square overflow-hidden rounded-lg bg-muted/20 ring-1 ring-border/30">
                          <img src={url} alt={settings.referenceFiles[i]?.name} className="h-full w-full object-cover" />
                          <button
                            type="button"
                            onClick={() => removeRefFile(i)}
                            className="absolute right-0.5 top-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-black/50 text-white opacity-0 transition-opacity group-hover:opacity-100"
                          >
                            <X className="h-2.5 w-2.5" />
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                  {settings.referenceFiles.length < (caps?.maxRefImages ?? 0) && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="w-full border border-dashed border-border/50 text-[11px] text-muted-foreground hover:border-primary/30 hover:text-primary"
                      onClick={() => refInputRef.current?.click()}
                    >
                      <Upload className="h-3 w-3" />
                      上传参考图
                    </Button>
                  )}
                  <input
                    ref={refInputRef}
                    type="file"
                    accept="image/*"
                    multiple
                    className="hidden"
                    onChange={handleRefFiles}
                  />
                </div>
              </div>
            )}

            {/* Mask */}
            {showMask && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>
                  <Paintbrush className="h-3 w-3" />
                  蒙版
                </h4>
                <div className="space-y-1.5">
                  {settings.maskFile ? (
                    <div className="flex items-center gap-1.5 rounded-lg bg-muted/20 px-2.5 py-1.5">
                      <span className="flex-1 truncate text-[11px] text-foreground">{settings.maskFile.name}</span>
                      <button
                        type="button"
                        onClick={() => update({ maskFile: null })}
                        className="shrink-0 text-muted-foreground/60 transition-colors hover:text-destructive"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="w-full border border-dashed border-border/50 text-[11px] text-muted-foreground hover:border-primary/30 hover:text-primary"
                      onClick={() => maskInputRef.current?.click()}
                    >
                      <Upload className="h-3 w-3" />
                      上传蒙版
                    </Button>
                  )}
                  <input
                    ref={maskInputRef}
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={handleMaskFile}
                  />
                </div>
              </div>
            )}
          </div>

          {/* Bottom: History */}
          <div className="shrink-0 p-3">
            <Separator className="mb-3" />
            <button
              type="button"
              onClick={onHistoryToggle}
              className="flex w-full items-center gap-2 rounded-xl px-3 py-2.5 text-xs text-muted-foreground/60 transition-all duration-200 hover:bg-muted/40 hover:text-foreground"
            >
              <History className="h-3.5 w-3.5" />
              历史记录
            </button>
          </div>
        </aside>
      </div>

      {/* Mobile: bottom scrollable panel */}
      <div className="flex max-h-48 shrink-0 overflow-y-auto border-t border-border/50 bg-card/60 px-3 py-2 backdrop-blur-sm md:hidden">
        <div className="w-full space-y-2">
          {/* Mobile model selector */}
          {providers.length > 1 && (
            <div className="flex gap-1 overflow-x-auto pb-1">
              {providers.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  disabled={!p.enabled}
                  onClick={() => p.enabled && onModelChange(p.id)}
                  className={`shrink-0 rounded-lg px-3 py-1.5 text-xs font-medium transition-all ${
                    !p.enabled
                      ? 'cursor-not-allowed opacity-40'
                      : selectedProviderId === p.id
                        ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                        : 'bg-muted/30 text-muted-foreground'
                  }`}
                >
                  {p.name}
                  {!p.enabled && <Lock className="ml-1 inline h-2.5 w-2.5" />}
                </button>
              ))}
            </div>
          )}

          {/* Mobile settings row */}
          <div className="flex gap-1 overflow-x-auto">
            {SIZE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => update({ size: opt.value })}
                className={`shrink-0 rounded-lg px-2.5 py-1 text-xs transition-all ${
                  settings.size === opt.value
                    ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                    : 'bg-muted/30 text-muted-foreground'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={onHistoryToggle}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
          >
            <History className="h-3.5 w-3.5" />
            历史记录
          </button>
        </div>
      </div>
    </>
  )
}
