import { useState, useRef } from 'react'
import { X, Upload, ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/Select'
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from '@/components/ui/collapsible'
import { getModelCapabilities } from '@/types/designer'
import type { ModelCapabilities } from '@/types/designer'

export interface DesignerSettings {
  quality: string
  size: string
  n: number
  outputFormat: string
  referenceFiles: File[]
  maskFile: File | null
}

interface SettingsPanelProps {
  model: string
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
}

const SIZE_OPTIONS = [
  { value: '1:1', label: '1:1', desc: '方形' },
  { value: '16:9', label: '16:9', desc: '横屏' },
  { value: '9:16', label: '9:16', desc: '竖屏' },
  { value: '4:3', label: '4:3', desc: '横屏' },
  { value: '3:4', label: '3:4', desc: '竖屏' },
]

export default function SettingsPanel({ model, settings, onSettingsChange }: SettingsPanelProps) {
  const caps: ModelCapabilities | undefined = getModelCapabilities(model)
  const refInputRef = useRef<HTMLInputElement>(null)
  const maskInputRef = useRef<HTMLInputElement>(null)
  const [sizeOpen, setSizeOpen] = useState(true)
  const [qualityOpen, setQualityOpen] = useState(true)
  const [countOpen, setCountOpen] = useState(true)
  const [formatOpen, setFormatOpen] = useState(true)
  const [refOpen, setRefOpen] = useState(true)

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

  return (
    <div className="flex w-60 shrink-0 flex-col rounded-lg border border-border bg-card">
      <div className="px-3 py-3">
        <h3 className="text-sm font-medium text-foreground">生成设置</h3>
      </div>
      <Separator />
      <ScrollArea className="flex-1">
        <div className="space-y-1 p-3">

          {/* Size section */}
          <Collapsible open={sizeOpen} onOpenChange={setSizeOpen}>
            <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
              尺寸
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${sizeOpen ? 'rotate-180' : ''}`} />
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="grid grid-cols-3 gap-1.5 pb-2 pt-1">
                {SIZE_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => update({ size: opt.value })}
                    className={`flex flex-col items-center gap-0.5 rounded-md border px-2 py-1.5 text-xs transition-all ${
                      settings.size === opt.value
                        ? 'border-primary bg-primary/10 text-primary'
                        : 'border-border text-muted-foreground hover:border-primary/40 hover:text-foreground'
                    }`}
                  >
                    <span className="font-medium">{opt.label}</span>
                    <span className="text-[10px] opacity-70">{opt.desc}</span>
                  </button>
                ))}
              </div>
            </CollapsibleContent>
          </Collapsible>

          {/* Quality section - only for models with quality levels */}
          {caps && caps.qualityLevels.length > 0 && (
            <Collapsible open={qualityOpen} onOpenChange={setQualityOpen}>
              <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
                质量
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${qualityOpen ? 'rotate-180' : ''}`} />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="pb-2 pt-1">
                  <Select
                    value={settings.quality}
                    onValueChange={(val: string | null) => update({ quality: val ?? '' })}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="选择质量" />
                    </SelectTrigger>
                    <SelectContent>
                      {caps.qualityLevels.map((level) => (
                        <SelectItem key={level} value={level}>
                          {level === 'auto' ? '自动' : level === 'low' ? '低' : level === 'medium' ? '中' : '高'}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}

          {/* Count section - only for batch models */}
          {caps && caps.batch && (
            <Collapsible open={countOpen} onOpenChange={setCountOpen}>
              <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
                数量
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${countOpen ? 'rotate-180' : ''}`} />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="flex items-center gap-3 pb-2 pt-1">
                  <Slider
                    value={[settings.n]}
                    onValueChange={(val) => update({ n: Array.isArray(val) ? val[0] : val })}
                    min={1}
                    max={caps.maxBatch}
                    step={1}
                    className="flex-1"
                  />
                  <span className="w-6 text-center text-xs font-medium text-foreground">{settings.n}</span>
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}

          {/* Format section */}
          {caps && caps.outputFormats.length > 1 && (
            <Collapsible open={formatOpen} onOpenChange={setFormatOpen}>
              <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
                格式
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${formatOpen ? 'rotate-180' : ''}`} />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="pb-2 pt-1">
                  <Select
                    value={settings.outputFormat}
                    onValueChange={(val: string | null) => update({ outputFormat: val ?? '' })}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {caps.outputFormats.map((fmt) => (
                        <SelectItem key={fmt} value={fmt}>
                          {fmt.toUpperCase()}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}

          {/* Reference images section */}
          {caps && caps.maxRefImages > 0 && (
            <Collapsible open={refOpen} onOpenChange={setRefOpen}>
              <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
                参考图片 ({settings.referenceFiles.length}/{caps.maxRefImages})
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${refOpen ? 'rotate-180' : ''}`} />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="space-y-1.5 pb-2 pt-1">
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
                  {settings.referenceFiles.length < caps.maxRefImages && (
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
              </CollapsibleContent>
            </Collapsible>
          )}

          {/* Mask section - only for inpainting models */}
          {caps && caps.inpainting && (
            <Collapsible>
              <CollapsibleTrigger className="flex w-full items-center justify-between py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
                蒙版（局部编辑）
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="space-y-1.5 pb-2 pt-1">
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
              </CollapsibleContent>
            </Collapsible>
          )}

        </div>
      </ScrollArea>
    </div>
  )
}
