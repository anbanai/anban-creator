import { useQuery } from '@tanstack/react-query'
import {
  Proportions,
  Layers,
  History,
  Sparkles,
  Maximize,
  Coins,
  Stamp,
  SlidersHorizontal,
} from 'lucide-react'
import ModelSelector from '@/components/designer/ModelSelector'
import { Slider } from '@/components/ui/slider'
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

const PIXEL_SIZE_PATTERN = /^\d+x\d+$/i
const RATIO_SIZE_PATTERN = /^\d+:\d+$/i

function isPixelSizePreset(preset: string): boolean {
  return PIXEL_SIZE_PATTERN.test(preset.trim())
}

function isRatioSizePreset(preset: string): boolean {
  return RATIO_SIZE_PATTERN.test(preset.trim())
}

function ratioPresetInfo(preset: string): { value: string; label: string; desc: string } {
  return SIZE_OPTIONS.find((opt) => opt.value === preset) ?? { value: preset, label: preset, desc: '' }
}

// Size preset section for configured auto/pixel-size presets.
function SizePresetSection({ presets, value, onChange }: {
  presets: string[]
  value: string
  onChange: (size: string) => void
}) {
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
      </div>
    </div>
  )
}

function RatioSizeSection({ presets, value, onChange, sectionHeader }: {
  presets: string[]
  value: string
  onChange: (size: string) => void
  sectionHeader: string
}) {
  const options = presets.length > 0 ? presets : SIZE_OPTIONS.map((opt) => opt.value)

  return (
    <div className="space-y-2">
      <h4 className={sectionHeader}>
        <Proportions className="h-3 w-3" />
        尺寸
      </h4>
      <div className="grid grid-cols-3 gap-1">
        {options.map((preset) => {
          const info = ratioPresetInfo(preset)
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
              <span className="font-medium">{info.label}</span>
              <span className="text-[10px] opacity-60">{info.desc}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ResolutionSection({ value, onChange, sectionHeader }: {
  value: string
  onChange: (resolution: string) => void
  sectionHeader: string
}) {
  return (
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
            onClick={() => onChange(opt.value)}
            className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
              value === opt.value
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
  )
}

export type DesignerSettingsPatch = Partial<DesignerSettings>

interface DesignerToolbarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
  capabilities?: ModelCapabilities
  settings: DesignerSettings
  onSettingsChange: (patch: DesignerSettingsPatch) => void
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

  // Standalone designer operations are prepaid from the fixed-SKU wallet.
  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })
  const balance = balanceData?.balance ?? 0

  function update(patch: DesignerSettingsPatch) {
    onSettingsChange(patch)
  }
  const showCount = (caps?.maxBatch ?? 1) > 1
  const showQuality = (caps?.qualityLevels.length ?? 0) > 0
  const showFormat = (caps?.outputFormats.length ?? 0) > 1
  const configuredSizePresets = caps?.sizePresets ?? []
  const pixelSizePresets = configuredSizePresets.filter((preset) => preset.trim().toLowerCase() === 'auto' || isPixelSizePreset(preset))
  const ratioSizePresets = configuredSizePresets.filter(isRatioSizePreset)
  const showPixelSizePresets = pixelSizePresets.length > 0
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

            {/* Size — render exactly from configured capability shapes */}
            {showPixelSizePresets ? (
              <SizePresetSection
                presets={pixelSizePresets}
                value={settings.size}
                onChange={(size) => update({ size })}
              />
            ) : (
              <>
                <RatioSizeSection
                  presets={ratioSizePresets}
                  value={settings.size}
                  onChange={(size) => update({ size })}
                  sectionHeader={sectionHeader}
                />
                <ResolutionSection
                  value={settings.resolution}
                  onChange={(resolution) => update({ resolution })}
                  sectionHeader={sectionHeader}
                />
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
            {showPixelSizePresets ? (
              pixelSizePresets.map((preset) => {
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
              (ratioSizePresets.length > 0 ? ratioSizePresets.map(ratioPresetInfo) : SIZE_OPTIONS).map((opt) => (
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
