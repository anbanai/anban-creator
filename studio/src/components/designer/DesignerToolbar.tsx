import { useRef } from 'react'
import {
  Proportions,
  Layers,
  ImagePlus,
  Paintbrush,
  History,
  X,
  Upload,
} from 'lucide-react'
import { Slider } from '@/components/ui/slider'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/Select'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerSettings } from '@/types/designer'

const SIZE_OPTIONS = [
  { value: '1:1', label: '1:1', desc: '方形' },
  { value: '16:9', label: '16:9', desc: '横屏' },
  { value: '9:16', label: '9:16', desc: '竖屏' },
  { value: '4:3', label: '4:3', desc: '横屏' },
  { value: '3:4', label: '3:4', desc: '竖屏' },
]

interface DesignerToolbarProps {
  provider: string
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
  onHistoryToggle: () => void
}

export default function DesignerToolbar({
  provider,
  settings,
  onSettingsChange,
  onHistoryToggle,
}: DesignerToolbarProps) {
  const caps = getModelCapabilities(provider)
  const refInputRef = useRef<HTMLInputElement>(null)
  const maskInputRef = useRef<HTMLInputElement>(null)

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

  const sectionClass = 'space-y-2'
  const headerClass = 'flex items-center gap-1.5 text-xs font-medium text-muted-foreground'

  const sidebarContent = (
    <>
      {/* Size */}
      <div className={sectionClass}>
        <h4 className={headerClass}>
          <Proportions className="h-3.5 w-3.5" />
          尺寸
        </h4>
        <div className="grid grid-cols-3 gap-1.5">
          {SIZE_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              onClick={() => update({ size: opt.value })}
              className={`flex flex-col items-center gap-0.5 rounded-lg px-2 py-2 text-xs transition-all duration-200 ${
                settings.size === opt.value
                  ? 'bg-primary/10 text-primary ring-1 ring-primary/30'
                  : 'bg-muted/30 text-muted-foreground hover:bg-accent/50 hover:text-foreground'
              }`}
            >
              <span className="font-medium">{opt.label}</span>
              <span className="text-[10px] opacity-70">{opt.desc}</span>
            </button>
          ))}
        </div>
      </div>

      <Separator />

      {/* Count */}
      {showCount && (
        <>
          <div className={sectionClass}>
            <h4 className={headerClass}>
              <Layers className="h-3.5 w-3.5" />
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
              <span className="w-6 text-center text-xs font-medium text-foreground">{settings.n}</span>
            </div>
          </div>
          <Separator />
        </>
      )}

      {/* Quality */}
      {showQuality && (
        <>
          <div className={sectionClass}>
            <h4 className={headerClass}>质量</h4>
            <div className="flex flex-wrap gap-1">
              {caps!.qualityLevels.map((level) => (
                <button
                  key={level}
                  type="button"
                  onClick={() => update({ quality: level })}
                  className={`rounded-md px-2.5 py-1 text-xs transition-all duration-200 ${
                    settings.quality === level
                      ? 'bg-primary/10 text-primary ring-1 ring-primary/30'
                      : 'bg-muted/30 text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                  }`}
                >
                  {level === 'auto' ? '自动' : level === 'low' ? '低' : level === 'medium' ? '中' : '高'}
                </button>
              ))}
            </div>
          </div>
          <Separator />
        </>
      )}

      {/* Output Format */}
      {showFormat && (
        <>
          <div className={sectionClass}>
            <h4 className={headerClass}>格式</h4>
            <div className="flex flex-wrap gap-1">
              {caps!.outputFormats.map((fmt) => (
                <button
                  key={fmt}
                  type="button"
                  onClick={() => update({ outputFormat: fmt })}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-all duration-200 ${
                    settings.outputFormat === fmt
                      ? 'bg-primary/10 text-primary ring-1 ring-primary/30'
                      : 'bg-muted/30 text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                  }`}
                >
                  {fmt.toUpperCase()}
                </button>
              ))}
            </div>
          </div>
          <Separator />
        </>
      )}

      {/* References */}
      {showRefs && (
        <>
          <div className={sectionClass}>
            <h4 className={headerClass}>
              <ImagePlus className="h-3.5 w-3.5" />
              参考图片 ({settings.referenceFiles.length}/{caps!.maxRefImages})
            </h4>
            <div className="space-y-1.5">
              {settings.referenceFiles.map((file, i) => (
                <div key={i} className="flex items-center gap-1.5 rounded-md border border-border px-2 py-1">
                  <span className="flex-1 truncate text-xs text-foreground">{file.name}</span>
                  <button
                    type="button"
                    onClick={() => removeRefFile(i)}
                    className="shrink-0 text-muted-foreground hover:text-destructive"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
              {settings.referenceFiles.length < caps!.maxRefImages && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => refInputRef.current?.click()}
                >
                  <Upload className="h-3.5 w-3.5" />
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
          <Separator />
        </>
      )}

      {/* Mask */}
      {showMask && (
        <>
          <div className={sectionClass}>
            <h4 className={headerClass}>
              <Paintbrush className="h-3.5 w-3.5" />
              蒙版（局部编辑）
            </h4>
            <div className="space-y-1.5">
              {settings.maskFile ? (
                <div className="flex items-center gap-1.5 rounded-md border border-border px-2 py-1">
                  <span className="flex-1 truncate text-xs text-foreground">{settings.maskFile.name}</span>
                  <button
                    type="button"
                    onClick={() => update({ maskFile: null })}
                    className="shrink-0 text-muted-foreground hover:text-destructive"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => maskInputRef.current?.click()}
                >
                  <Upload className="h-3.5 w-3.5" />
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
          <Separator />
        </>
      )}

      {/* History */}
      <button
        type="button"
        onClick={onHistoryToggle}
        className="flex w-full items-center gap-2 rounded-lg px-2 py-2 text-xs text-muted-foreground transition-all duration-200 hover:bg-accent/50 hover:text-foreground"
      >
        <History className="h-4 w-4" />
        历史记录
      </button>
    </>
  )

  return (
    <>
      {/* Desktop: scrollable sidebar */}
      <aside className="hidden w-56 shrink-0 flex-col border-r border-border/50 bg-card/60 backdrop-blur-sm md:flex">
        <div className="flex-1 overflow-y-auto p-3">
          {sidebarContent}
        </div>
      </aside>

      {/* Mobile: scrollable panel */}
      <div className="flex max-h-48 shrink-0 overflow-y-auto border-t border-border/50 bg-card/60 px-3 py-2 backdrop-blur-sm md:hidden">
        <div className="w-full">
          {sidebarContent}
        </div>
      </div>
    </>
  )
}
