import type { UseFormReturn } from 'react-hook-form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'

interface MontageCreationPanelProps {
  form: UseFormReturn<any>
  fieldRoot: 'montage_input'
}

export function MontageCreationPanel({ form, fieldRoot }: MontageCreationPanelProps) {
  const control = form.control

  return (
    <div className="space-y-4">
      <FormField control={control} name={`${fieldRoot}.brief`} render={({ field }) => (
        <FormItem>
          <FormLabel>视频 brief</FormLabel>
          <FormControl>
            <Textarea
              {...field}
              value={field.value ?? ''}
              placeholder="描述这次要生产的视频内容、素材用途、节奏和交付目标"
              rows={5}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />

      <div className="grid gap-3 md:grid-cols-3">
        <FormField control={control} name={`${fieldRoot}.pipeline_key`} render={({ field }) => (
          <FormItem>
            <FormLabel>Pipeline</FormLabel>
            <FormControl>
              <Input {...field} value={field.value ?? ''} placeholder="pipeline（可选）" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.aspect_ratio`} render={({ field }) => (
          <FormItem>
            <FormLabel>画幅</FormLabel>
            <FormControl>
              <Input {...field} value={field.value ?? ''} placeholder="画幅，如 9:16" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.duration_seconds`} render={({ field }) => (
          <FormItem>
            <FormLabel>时长（秒）</FormLabel>
            <FormControl>
              <Input
                aria-label="时长（秒）"
                type="number"
                min={1}
                max={600}
                value={field.value ?? ''}
                onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)}
                placeholder="时长（秒）"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      <FormField control={control} name={`${fieldRoot}.preferences.style`} render={({ field }) => (
        <FormItem>
          <FormLabel>风格偏好</FormLabel>
          <FormControl>
            <Textarea {...field} value={field.value ?? ''} placeholder="风格偏好（可选）" rows={3} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />
    </div>
  )
}
