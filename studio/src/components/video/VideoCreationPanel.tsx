import { useState, type ReactNode } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { ChevronDown, RotateCcw, Settings2 } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { VideoReferenceInput } from '@/components/video/VideoReferenceInput'
import { VIDEO_PROJECT_DEFAULT_VALUE, readVideoSelectValue, writeVideoSelectValue } from '@/lib/video-form'
import type { Project } from '@/types'

type VideoInputFieldRoot = 'video_creator_input' | 'video_editor_input'

export function VideoCreationPanel({
  form,
  fieldRoot,
  selectedProject,
  promptField,
  title,
}: {
  form: UseFormReturn<any>
  fieldRoot: VideoInputFieldRoot
  selectedProject?: Project
  promptField: ReactNode
  title?: string
}) {
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const clearHardConstraints = () => {
    form.setValue(`${fieldRoot}.hard_constraints`, {}, { shouldDirty: true })
  }

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-border p-3">
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">{selectedProject?.name ? `项目：${selectedProject.name}` : '请选择视频项目'}</p>
      </div>

      <section className="flex flex-col gap-2">
        <FormLabel>本次视频要求</FormLabel>
        {promptField}
      </section>

      <section className="flex flex-col gap-2">
        <FormField control={form.control} name={`${fieldRoot}.references`} render={({ field }) => (
          <FormItem>
            <FormLabel>参考素材</FormLabel>
            <FormControl>
              <VideoReferenceInput value={field.value || []} onChange={field.onChange} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </section>

      <section className="rounded-lg border border-border bg-muted/10">
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
            高级硬约束
            <ChevronDown className={advancedOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
          </Button>
          <Button type="button" variant="ghost" size="xs" onClick={clearHardConstraints}>
            <RotateCcw />
            清空
          </Button>
        </div>

        {advancedOpen && (
          <div className="grid gap-3 border-t border-border p-3 sm:grid-cols-3">
            <FormField control={form.control} name={`${fieldRoot}.hard_constraints.ratio`} render={({ field }) => (
              <FormItem>
                <FormLabel>比例</FormLabel>
                <Select value={readVideoSelectValue(field.value)} onValueChange={(value) => field.onChange(writeVideoSelectValue(value))}>
                  <FormControl><SelectTrigger className="w-full"><SelectValue placeholder="由 Agent 判断" /></SelectTrigger></FormControl>
                  <SelectContent>
                    <SelectItem value={VIDEO_PROJECT_DEFAULT_VALUE} label="由 Agent 判断">由 Agent 判断</SelectItem>
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
            <FormField control={form.control} name={`${fieldRoot}.hard_constraints.duration`} render={({ field }) => (
              <FormItem>
                <FormLabel>时长（秒）</FormLabel>
                <FormControl>
                  <Input
                    aria-label="时长（秒）"
                    type="number"
                    min={1}
                    max={600}
                    placeholder="由 Agent 判断"
                    value={field.value ?? ''}
                    onChange={(event) => field.onChange(event.target.value === '' ? undefined : Number(event.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )} />
            <FormField control={form.control} name={`${fieldRoot}.hard_constraints.watermark`} render={({ field }) => (
              <FormItem className="flex min-h-16 items-end gap-2 space-y-0 pb-2">
                <FormControl><Switch aria-label="加水印" checked={field.value ?? false} onCheckedChange={field.onChange} /></FormControl>
                <FormLabel className="text-sm">加水印</FormLabel>
              </FormItem>
            )} />
          </div>
        )}
      </section>
    </div>
  )
}
