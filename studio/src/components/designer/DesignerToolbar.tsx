import { useRef, useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Proportions,
  Layers,
  ImagePlus,
  History,
  X,
  Upload,
  Sparkles,
  Maximize,
  Coins,
  Stamp,
  SlidersHorizontal,
} from 'lucide-react'
import ModelSelector from '@/components/designer/ModelSelector'
import { Slider } from '@/components/ui/slider'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import type { DesignerSettings, DesignerProvider, ModelCapabilities } from '@/types/designer'
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

// Size preset labels for GPT-Image pixel sizes
const SIZE_PRESET_LABELS: Record<string, { label: string; desc: string }> = {
  'auto': { label: '自动', desc: '智能选择' },
  '1024x1024': { label: '1:1', desc: '1024×1024' },
  '1536x1024': { label: '3:2', desc: '1536×1024 · 横屏' },
  '1024x1536': { label: '2:3', desc: '1024×1536 · 竖屏' },
}

function validateCustomSize(w: number, h: number): string | null {
  if (w < 256 || h < 256) return '最小边长 256px'
  if (w > 3840 || h > 3840) return '最大边长 3840px'
  if (w % 16 !== 0 || h % 16 !== 0) return '必须是 16 的倍数'
  if (Math.max(w, h) / Math.min(w, h) > 3) return '长宽比不能超过 3:1'
  if (w * h < 655360) return '总像素数不能少于 655,360'
  if (w * h > 8294400) return '总像素数不能超过 8,294,400'
  return null
}

// Size preset section for GPT-Image models with pixel-size presets and custom input
function SizePresetSection({ presets, value, onChange }: {
  presets: string[]
  value: string
  onChange: (size: string) => void
}) {
  const [customW, setCustomW] = useState('1280')
  const [customH, setCustomH] = useState('1280')
  const isCustom = value === 'custom' || (value !== 'auto' && !presets.includes(value))

  const wNum = parseInt(customW, 10) || 0
  const hNum = parseInt(customH, 10) || 0
  const customError = isCustom ? validateCustomSize(wNum, hNum) : null

  function handleCustomApply() {
    if (!customError) {
      onChange(`${wNum}x${hNum}`)
    }
  }

  return (
    <div className="space-y-2">
      <h4 className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
        <Proportions className="h-3 w-3" />
        尺寸
      </h4>
      <div className="grid grid-cols-3 gap-1">
        {presets.map((preset) => {
          const info = SIZE_PRESET_LABELS[preset]
          return (
            <button
              key={preset}
              type="button"
              onClick={() => onChange(preset)}
              className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
                value === preset
                  ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                  : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
              }`}
            >
              <span className="font-medium">{info?.label ?? preset}</span>
              <span className="text-[10px] opacity-60">{info?.desc ?? ''}</span>
            </button>
          )
        })}
        {/* Custom size button */}
        <button
          type="button"
          onClick={() => {
            if (!isCustom) onChange(`${wNum}x${hNum}`)
          }}
          className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
            isCustom
              ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
              : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
          }`}
        >
          <span className="font-medium">自定义</span>
          <span className="text-[10px] opacity-60">W×H</span>
        </button>
      </div>

      {/* Custom size inputs */}
      {isCustom && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5">
            <input
              type="number"
              value={customW}
              onChange={(e) => setCustomW(e.target.value)}
              min={256}
              max={3840}
              step={16}
              className="w-full rounded-md border border-border/50 bg-muted/20 px-2 py-1 text-center text-xs tabular-nums focus:border-primary/50 focus:outline-none"
              placeholder="宽度"
            />
            <span className="text-[10px] text-muted-foreground">×</span>
            <input
              type="number"
              value={customH}
              onChange={(e) => setCustomH(e.target.value)}
              min={256}
              max={3840}
              step={16}
              className="w-full rounded-md border border-border/50 bg-muted/20 px-2 py-1 text-center text-xs tabular-nums focus:border-primary/50 focus:outline-none"
              placeholder="高度"
            />
          </div>
          {customError && (
            <p className="text-[10px] text-destructive">{customError}</p>
          )}
          {!customError && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full text-[11px] text-primary"
              onClick={handleCustomApply}
            >
              应用 {wNum}×{hNum}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

interface DesignerToolbarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
  capabilities?: ModelCapabilities
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
  onHistoryToggle: () => void
}

export default function DesignerToolbar({
  providers,
  selectedProviderId,
  onModelChange,
  capabilities,
  settings,
  onSettingsChange,
  onHistoryToggle,
}: DesignerToolbarProps) {
  const caps = capabilities
  const refInputRef = useRef<HTMLInputElement>(null)

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
    const max = caps?.maxReferenceImages ?? 0
    const combined = [...settings.referenceFiles, ...files].slice(0, max)
    update({ referenceFiles: combined })
    e.target.value = ''
  }

  function removeRefFile(index: number) {
    update({ referenceFiles: settings.referenceFiles.filter((_, i) => i !== index) })
  }

  const showCount = (caps?.maxBatch ?? 1) > 1
  const showQuality = (caps?.qualityLevels.length ?? 0) > 0
  const showFormat = (caps?.outputFormats.length ?? 0) > 1
  const showRefs = (caps?.supportsReference ?? false) && (caps?.maxReferenceImages ?? 0) > 0

  // Generate object URLs for reference image previews
  const [refPreviewUrls, setRefPreviewUrls] = useState<string[]>([])
  useEffect(() => {
    const urls = settings.referenceFiles.map((f) => URL.createObjectURL(f))
    setRefPreviewUrls(urls)
    return () => urls.forEach((u) => URL.revokeObjectURL(u))
  }, [settings.referenceFiles])

  const sectionHeader = 'flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground'

  return (
    <>
      {/* Desktop: floating glass sidebar */}
      <div className="hidden w-[280px] shrink-0 p-3 md:block">
        <aside className="relative flex h-full flex-col overflow-hidden rounded-2xl border border-border/75 bg-card/80 shadow-[0_8px_40px_-12px_rgba(0,0,0,0.32)]">
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
                  设计师
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
            <ModelSelector
              providers={providers}
              selectedProviderId={selectedProviderId}
              onChange={onModelChange}
            />

            <Separator />

            {/* Size — pixel presets for GPT-Image, ratio grid for others */}
            {caps && caps.sizePresets.length > 0 ? (
              <SizePresetSection
                presets={caps.sizePresets}
                value={settings.size}
                onChange={(size) => update({ size })}
              />
            ) : (
              <>
                {/* Ratio grid */}
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
                            : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
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
                            : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
                        }`}
                      >
                        <span className="font-medium">{opt.label}</span>
                        <span className="text-[10px] opacity-60">{opt.desc}</span>
                      </button>
                    ))}
                  </div>
                </div>
              </>
            )}

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
                          : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
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
                          : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
                      }`}
                    >
                      {fmt.toUpperCase()}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* Compression — only for JPEG/WebP */}
            {caps?.hasCompression && (settings.outputFormat === 'jpeg' || settings.outputFormat === 'webp') && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>
                  <SlidersHorizontal className="h-3 w-3" />
                  压缩率
                </h4>
                <div className="flex items-center gap-3">
                  <Slider
                    value={[settings.compression]}
                    onValueChange={(val) => update({ compression: Array.isArray(val) ? val[0] : val })}
                    min={0}
                    max={100}
                    step={1}
                    className="flex-1"
                  />
                  <span className="w-8 text-center text-xs font-bold tabular-nums text-foreground">{settings.compression}%</span>
                </div>
              </div>
            )}

            {/* Background */}
            {caps?.hasBackground && (
              <div className="space-y-2">
                <h4 className={sectionHeader}>背景</h4>
                <div className="flex flex-wrap gap-1">
                  {(['auto', 'opaque'] as const).map((bg) => (
                    <button
                      key={bg}
                      type="button"
                      onClick={() => update({ background: bg })}
                      className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-all duration-200 ${
                        settings.background === bg
                          ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                          : 'bg-muted/35 text-muted-foreground hover:bg-muted/55 hover:text-foreground'
                      }`}
                    >
                      {bg === 'auto' ? '自动' : '不透明'}
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
                  参考图 ({settings.referenceFiles.length}/{caps!.maxReferenceImages})
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
                  {settings.referenceFiles.length < (caps?.maxReferenceImages ?? 0) && (
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
          </div>

          {/* Bottom: History */}
          <div className="shrink-0 p-3">
            <Separator className="mb-3" />
            <button
              type="button"
              onClick={onHistoryToggle}
              className="flex w-full items-center gap-2 rounded-xl px-3 py-2.5 text-xs text-muted-foreground transition-all duration-200 hover:bg-muted/55 hover:text-foreground"
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
          <ModelSelector
            providers={providers}
            selectedProviderId={selectedProviderId}
            onChange={onModelChange}
          />

          {/* Mobile settings row */}
          <div className="flex gap-1 overflow-x-auto">
            {caps && caps.sizePresets.length > 0 ? (
              caps.sizePresets.map((preset) => {
                const info = SIZE_PRESET_LABELS[preset]
                return (
                  <button
                    key={preset}
                    type="button"
                    onClick={() => update({ size: preset })}
                    className={`shrink-0 rounded-lg px-2.5 py-1 text-xs transition-all ${
                      settings.size === preset
                        ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                        : 'bg-muted/30 text-muted-foreground'
                    }`}
                  >
                    {info?.label ?? preset}
                  </button>
                )
              })
            ) : (
              SIZE_OPTIONS.map((opt) => (
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
              ))
            )}
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
