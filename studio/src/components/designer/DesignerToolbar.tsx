import { useState, useRef } from 'react'
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
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerSettings } from '@/types/designer'

const SIZE_OPTIONS = [
  { value: '1:1', label: '1:1', desc: '方形' },
  { value: '16:9', label: '16:9', desc: '横屏' },
  { value: '9:16', label: '9:16', desc: '竖屏' },
  { value: '4:3', label: '4:3', desc: '横屏' },
  { value: '3:4', label: '3:4', desc: '竖屏' },
]

type ToolId = 'size' | 'count' | 'references' | 'mask'

interface ToolDef {
  id: ToolId
  label: string
  icon: React.ComponentType<{ className?: string }>
  capability: string | null
}

const TOOLS: ToolDef[] = [
  { id: 'size', label: '尺寸', icon: Proportions, capability: null },
  { id: 'count', label: '数量', icon: Layers, capability: 'batch' },
  { id: 'references', label: '参考图', icon: ImagePlus, capability: 'references' },
  { id: 'mask', label: '蒙版', icon: Paintbrush, capability: 'inpainting' },
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
  const [activeTool, setActiveTool] = useState<ToolId | null>(null)
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

  const visibleTools = TOOLS.filter((tool) => {
    if (!caps) return tool.capability === null
    if (tool.capability === null) return true
    if (tool.capability === 'batch') return caps.batch
    if (tool.capability === 'references') return caps.maxRefImages > 0
    if (tool.capability === 'inpainting') return caps.inpainting
    return true
  })

  function renderToolPanel(id: ToolId) {
    switch (id) {
      case 'size':
        return (
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground">尺寸</h4>
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
        )
      case 'count':
        if (!caps) return null
        return (
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground">数量</h4>
            <div className="flex items-center gap-3">
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
          </div>
        )
      case 'references':
        if (!caps) return null
        return (
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground">
              参考图片 ({settings.referenceFiles.length}/{caps.maxRefImages})
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
          </div>
        )
      case 'mask':
        return (
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground">蒙版（局部编辑）</h4>
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
        )
    }
  }

  return (
    <>
      {/* Desktop: vertical sidebar */}
      <aside className="hidden shrink-0 flex-col md:flex">
        <div className="flex flex-1">
          <div className="flex w-12 flex-col items-center gap-0.5 border-r border-border/30 bg-card/60 py-3 backdrop-blur-sm">
            {visibleTools.map((tool) => (
              <Tooltip key={tool.id}>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      onClick={() => setActiveTool(activeTool === tool.id ? null : tool.id)}
                      className={`flex h-9 w-9 items-center justify-center rounded-lg transition-all duration-200 ${
                        activeTool === tool.id
                          ? 'bg-primary/10 text-primary shadow-sm shadow-primary/20'
                          : 'text-muted-foreground/60 hover:bg-accent/50 hover:text-foreground'
                      }`}
                    />
                  }
                >
                  <tool.icon className="h-5 w-5" />
                </TooltipTrigger>
                <TooltipContent side="right">{tool.label}</TooltipContent>
              </Tooltip>
            ))}

            <Separator className="my-1.5 w-6 opacity-30" />

            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={onHistoryToggle}
                    className="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground/60 transition-all duration-200 hover:bg-accent/50 hover:text-foreground"
                  />
                }
              >
                <History className="h-5 w-5" />
              </TooltipTrigger>
              <TooltipContent side="right">历史记录</TooltipContent>
            </Tooltip>
          </div>

          {activeTool && (
            <div className="w-52 animate-fade-in overflow-y-auto border-l border-border/30 bg-card/70 p-3 shadow-lg backdrop-blur-md">
              {renderToolPanel(activeTool)}
            </div>
          )}
        </div>
      </aside>

      {/* Mobile: horizontal strip */}
      <div className="flex shrink-0 items-center gap-0.5 border-t border-border/30 bg-card/60 px-2 py-1.5 backdrop-blur-sm md:hidden">
        {visibleTools.map((tool) => (
          <Tooltip key={tool.id}>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => setActiveTool(activeTool === tool.id ? null : tool.id)}
                  className={`flex h-9 w-9 items-center justify-center rounded-md transition-all duration-200 ${
                    activeTool === tool.id
                      ? 'bg-primary/10 text-primary shadow-sm shadow-primary/20'
                      : 'text-muted-foreground/60 hover:bg-accent/50 hover:text-foreground'
                  }`}
                />
              }
            >
              <tool.icon className="h-4 w-4" />
            </TooltipTrigger>
            <TooltipContent side="top">{tool.label}</TooltipContent>
          </Tooltip>
        ))}

        <Separator orientation="vertical" className="mx-1 h-6 opacity-30" />

        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={onHistoryToggle}
                className="flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground/60 transition-all duration-200 hover:bg-accent/50 hover:text-foreground"
              />
            }
          >
            <History className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="top">历史记录</TooltipContent>
        </Tooltip>

        {activeTool && (
          <div className="ml-2 flex-1 overflow-x-auto">
            <div className="min-w-max">
              {renderToolPanel(activeTool)}
            </div>
          </div>
        )}
      </div>
    </>
  )
}
