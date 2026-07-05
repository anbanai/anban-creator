import { useState, type ReactNode } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { RotateCcw, Settings2 } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { VideoReferenceInput } from '@/components/video/VideoReferenceInput'
import { VIDEO_PROJECT_DEFAULT_VALUE, buildVideoFormConfig, defaultVideoPurposeForCreativeType, readVideoSelectValue, writeVideoSelectValue } from '@/lib/video-form'
import { videoCreativeTypeLabels, videoModelDisplayName, videoPurposeLabels } from '@/lib/video-display'
import type { Project, VideoModelSpec, VideoTaskConfig } from '@/types'

function yesNo(value: boolean | undefined) {
  return value ? '开启' : '关闭'
}

function settingValue<T>(override: T | undefined, fallback: T | undefined, empty: T): T {
  return override ?? fallback ?? empty
}

export function VideoCreationPanel({
  form,
  selectedProject,
  availableVideoModels,
  modelsLoading,
  promptField,
  estimateSummary,
  minimumBalanceHint,
  title = '视频创作',
}: {
  form: UseFormReturn<any>
  selectedProject?: Project
  availableVideoModels: VideoModelSpec[]
  modelsLoading?: boolean
  promptField: ReactNode
  estimateSummary: ReactNode
  minimumBalanceHint: string
  title?: string
}) {
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const config = form.watch('video_config') as VideoTaskConfig | undefined
  const defaults = selectedProject?.video_defaults
  const resolvedModel = settingValue(config?.model_key, defaults?.model_key, '')
  const resolvedResolution = settingValue(config?.resolution, defaults?.resolution, '720p')
  const resolvedRatio = settingValue(config?.ratio, defaults?.ratio, '9:16')
  const resolvedFallbackDuration = settingValue(config?.duration, defaults?.duration, 15)
  const resolvedWatermark = settingValue(config?.watermark, defaults?.watermark, false)

  const restoreDefaults = () => {
    const references = form.getValues('video_config.references') ?? []
    form.setValue('video_config', buildVideoFormConfig(defaults, { references }), { shouldDirty: true })
  }

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-border p-3">
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">{selectedProject?.name ? `将使用「${selectedProject.name}」的视频项目配置。` : '先选择视频项目，再补充创作要求和素材。'}</p>
      </div>

      <section className="flex flex-col gap-2">
        <FormField control={form.control} name="video_config.workflow" render={() => (
          <FormItem>
            <FormLabel>工作流</FormLabel>
            <FormControl>
              <div
                className="grid grid-cols-2 rounded-md border border-border p-1"
                aria-label="视频工作流"
                role="group"
              >
                <button
                  type="button"
                  aria-pressed={(config?.workflow ?? 'creator') === 'creator'}
                  className="h-9 rounded-sm px-2.5 text-sm font-medium transition-colors aria-pressed:bg-muted hover:bg-muted"
                  onClick={() => form.setValue('video_config.workflow', 'creator', { shouldDirty: true })}
                >
                  生成
                </button>
                <button
                  type="button"
                  aria-pressed={config?.workflow === 'editor'}
                  className="h-9 rounded-sm px-2.5 text-sm font-medium transition-colors aria-pressed:bg-muted hover:bg-muted"
                  onClick={() => form.setValue('video_config.workflow', 'editor', { shouldDirty: true })}
                >
                  剪辑
                </button>
              </div>
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </section>

      <section className="flex flex-col gap-2">
        <div>
          <p className="text-sm font-medium text-foreground">{config?.workflow === 'editor' ? '剪辑要求' : '创作要求'}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{config?.workflow === 'editor' ? '写清楚素材如何剪、字幕/调色/动效要求和交付格式。' : '写清楚这条视频要表达什么，留空则使用项目定位自动生成。'}</p>
        </div>
        {promptField}
      </section>

      <section className="grid gap-3 sm:grid-cols-2">
        <FormField control={form.control} name="video_config.creative_type" render={({ field }) => (
          <FormItem>
            <FormLabel>内容类型</FormLabel>
            <Select
              value={field.value || defaults?.creative_type || 'personal_ip'}
              onValueChange={(value) => {
                const previousDefaultPurpose = defaultVideoPurposeForCreativeType(field.value || defaults?.creative_type || 'personal_ip')
                field.onChange(value)
                const currentPurpose = form.getValues('video_config.purpose')
                if (!currentPurpose || currentPurpose === previousDefaultPurpose) {
                  form.setValue('video_config.purpose', defaultVideoPurposeForCreativeType(value), { shouldDirty: true })
                }
              }}
            >
              <FormControl><SelectTrigger className="w-full"><SelectValue /></SelectTrigger></FormControl>
              <SelectContent>
                {Object.entries(videoCreativeTypeLabels).map(([value, label]) => (
                  <SelectItem key={value} value={value} label={label}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="video_config.purpose" render={({ field }) => (
          <FormItem>
            <FormLabel>商业目标</FormLabel>
            <Select value={field.value || defaults?.purpose || defaultVideoPurposeForCreativeType(config?.creative_type || defaults?.creative_type)} onValueChange={field.onChange}>
              <FormControl><SelectTrigger className="w-full"><SelectValue /></SelectTrigger></FormControl>
              <SelectContent>
                {Object.entries(videoPurposeLabels).map(([value, label]) => (
                  <SelectItem key={value} value={value} label={label}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="video_config.subject_profile" render={({ field }) => (
          <FormItem className="sm:col-span-2">
            <FormLabel>人物 / 主体</FormLabel>
            <FormControl>
              <Textarea
                placeholder="例如：30 岁效率博主，黑色衬衫，语速快但亲和；必须保持同一人设和穿搭。"
                className="min-h-20 resize-y"
                {...field}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="video_config.audience" render={({ field }) => (
          <FormItem>
            <FormLabel>目标受众</FormLabel>
            <FormControl><Input placeholder="例如：想提升工作效率的职场新人" {...field} /></FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="video_config.single_message" render={({ field }) => (
          <FormItem>
            <FormLabel>核心信息</FormLabel>
            <FormControl><Input placeholder="例如：把会议记录变成行动清单" {...field} /></FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </section>

      <section className="flex flex-col gap-2">
        <div>
          <p className="text-sm font-medium text-foreground">参考素材</p>
          <p className="mt-0.5 text-xs text-muted-foreground">上传产品图、首帧、动作视频或 BGM，也可以添加文字约束。</p>
        </div>
        <FormField control={form.control} name="video_config.references" render={({ field }) => (
          <FormItem>
            <FormControl>
              <VideoReferenceInput value={field.value || []} onChange={field.onChange} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <p className="text-sm font-medium text-foreground">生成设置</p>
          <div className="mt-2 grid gap-2 text-xs sm:grid-cols-3">
            <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
              <p className="text-muted-foreground">模型</p>
              <p className="mt-0.5 truncate font-medium text-foreground">{resolvedModel ? videoModelDisplayName(resolvedModel) : '项目默认'}</p>
            </div>
            <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
              <p className="text-muted-foreground">比例 / 分辨率</p>
              <p className="mt-0.5 font-medium text-foreground">{resolvedRatio} · {resolvedResolution}</p>
            </div>
            <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
              <p className="text-muted-foreground">兜底时长 / 水印</p>
              <p className="mt-0.5 font-medium text-foreground">{resolvedFallbackDuration}s · 水印{yesNo(resolvedWatermark)}</p>
            </div>
          </div>
        </div>

        <div className="rounded-lg border border-border bg-muted/10">
          <div className="flex items-center justify-between gap-3 px-3 py-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="justify-start px-0 text-sm font-medium text-foreground hover:bg-transparent"
              aria-expanded={advancedOpen}
              onClick={() => setAdvancedOpen((open) => !open)}
            >
              <Settings2 />
              高级设置
            </Button>
            <Button type="button" variant="ghost" size="xs" onClick={restoreDefaults}>
              <RotateCcw />
              恢复项目默认
            </Button>
          </div>
          {advancedOpen && <div className="grid gap-3 border-t border-border p-3 sm:grid-cols-2">
            <FormField control={form.control} name="video_config.model_key" render={({ field }) => (
              <FormItem>
                <FormLabel>模型</FormLabel>
                <Select value={readVideoSelectValue(field.value)} onValueChange={(value) => field.onChange(writeVideoSelectValue(value))}>
                  <FormControl><SelectTrigger className="w-full"><SelectValue placeholder={modelsLoading ? '加载可用模型...' : '使用项目默认'} /></SelectTrigger></FormControl>
                  <SelectContent>
                    <SelectItem value={VIDEO_PROJECT_DEFAULT_VALUE} label="使用项目默认">
                      使用项目默认{defaults?.model_key ? `（${videoModelDisplayName(defaults.model_key)}）` : ''}
                    </SelectItem>
                    {availableVideoModels.map((model) => (
                      <SelectItem key={model.key} value={model.key} label={videoModelDisplayName(model)}>{videoModelDisplayName(model)}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
                {!modelsLoading && availableVideoModels.length === 0 && (
                  <p className="text-xs text-destructive">没有可用视频模型，请先在项目策略中选择已配置模型。</p>
                )}
              </FormItem>
            )} />
            <FormField control={form.control} name="video_config.resolution" render={({ field }) => (
              <FormItem>
                <FormLabel>分辨率</FormLabel>
                <Select value={readVideoSelectValue(field.value)} onValueChange={(value) => field.onChange(writeVideoSelectValue(value))}>
                  <FormControl><SelectTrigger className="w-full"><SelectValue placeholder="使用项目默认" /></SelectTrigger></FormControl>
                  <SelectContent>
                    <SelectItem value={VIDEO_PROJECT_DEFAULT_VALUE} label="使用项目默认">
                      使用项目默认{defaults?.resolution ? `（${defaults.resolution}）` : ''}
                    </SelectItem>
                    <SelectItem value="480p" label="480p">480p</SelectItem>
                    <SelectItem value="720p" label="720p">720p</SelectItem>
                    <SelectItem value="1080p" label="1080p">1080p</SelectItem>
                    <SelectItem value="4k" label="4K">4K</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )} />
            <FormField control={form.control} name="video_config.ratio" render={({ field }) => (
              <FormItem>
                <FormLabel>比例</FormLabel>
                <Select value={readVideoSelectValue(field.value)} onValueChange={(value) => field.onChange(writeVideoSelectValue(value))}>
                  <FormControl><SelectTrigger className="w-full"><SelectValue placeholder="使用项目默认" /></SelectTrigger></FormControl>
                  <SelectContent>
                    <SelectItem value={VIDEO_PROJECT_DEFAULT_VALUE} label="使用项目默认">
                      使用项目默认{defaults?.ratio ? `（${defaults.ratio}）` : ''}
                    </SelectItem>
                    <SelectItem value="9:16" label="9:16">9:16</SelectItem>
                    <SelectItem value="16:9" label="16:9">16:9</SelectItem>
                    <SelectItem value="1:1" label="1:1">1:1</SelectItem>
                    <SelectItem value="4:3" label="4:3">4:3</SelectItem>
                    <SelectItem value="3:4" label="3:4">3:4</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )} />
            <FormField control={form.control} name="video_config.duration" render={({ field }) => (
              <FormItem>
                <FormLabel>兜底目标时长（秒）</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    min={1}
                    max={600}
                    placeholder={String(defaults?.duration ?? 15)}
                    value={field.value ?? ''}
                    onChange={(event) => field.onChange(event.target.value === '' ? undefined : Number(event.target.value))}
                  />
                </FormControl>
                <p className="text-xs text-muted-foreground">没有明确用户时长或参考视频时才使用；模型单段上限由服务端自动拆分。</p>
                <FormMessage />
              </FormItem>
            )} />
            <div className="flex items-end gap-4 pb-2">
              <FormField control={form.control} name="video_config.watermark" render={({ field }) => (
                <FormItem className="flex items-center gap-2 space-y-0">
                  <FormControl><Switch checked={field.value ?? defaults?.watermark ?? false} onCheckedChange={field.onChange} /></FormControl>
                  <FormLabel className="text-sm">水印</FormLabel>
                </FormItem>
              )} />
              {modelsLoading && <Skeleton className="h-5 w-24 rounded-md" />}
            </div>
          </div>}
        </div>

        <p className="text-xs text-muted-foreground">{minimumBalanceHint}</p>
        {estimateSummary}
      </section>
    </div>
  )
}
