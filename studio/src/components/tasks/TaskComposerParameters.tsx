import { ChevronDownIcon, SlidersHorizontalIcon } from 'lucide-react'

import {
  ImageGenerationSettings,
  type ImageGenerationSettingsProps,
} from '@/components/ImageGenerationToolbar'
import {
  QuantityStepper,
  type QuantityStepperProps,
} from '@/components/agent-prompt/QuantityStepper'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Separator } from '@/components/ui/separator'
import { ExecutionProfileSelector, type ExecutionProfileSelectorProps } from './ExecutionProfileSelector'

export interface TaskComposerParametersProps {
  execution: ExecutionProfileSelectorProps
  image?: ImageGenerationSettingsProps
  quantity?: QuantityStepperProps
  disabled?: boolean
  label?: string
}

function executionSummary(execution: ExecutionProfileSelectorProps) {
  if (execution.loading) return '执行配置加载中'
  const selected = execution.profiles.find((profile) => profile.id === execution.value)
  if (!selected) return '未选择执行配置'
  return selected.display_name
}

function imageSummary(image: ImageGenerationSettingsProps) {
  if (image.loading) return '图像设置加载中'
  const capability = image.capabilities.find((option) => option.key === image.capabilityKey)
  const ratio = image.ratio === 'auto' ? '智能适配' : image.ratio
  return [ratio, capability?.display_name].filter(Boolean).join(' / ')
}

export function TaskComposerParameters({
  execution,
  image,
  quantity,
  disabled = false,
  label = '创作参数',
}: TaskComposerParametersProps) {
  const summary = [
    executionSummary(execution),
    image ? imageSummary(image) : undefined,
    quantity ? `${quantity.label} ${quantity.value}` : undefined,
  ].filter(Boolean).join(' / ')

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            aria-label={`${label}：${summary}`}
            className="max-w-full px-2 text-muted-foreground"
          />
        )}
      >
        <SlidersHorizontalIcon data-icon="inline-start" />
        <span className="max-w-64 truncate">{summary}</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-[var(--available-height)] w-[min(38rem,calc(100vw-1rem))] gap-3 overflow-y-auto p-3"
      >
        <PopoverTitle className="sr-only">{label}</PopoverTitle>
        <section className="flex flex-col gap-1">
          <h3 className="font-medium">执行配置</h3>
          <ExecutionProfileSelector {...execution} layout="horizontal" />
        </section>
        {image ? (
          <>
            <Separator />
            <ImageGenerationSettings
              {...image}
              ratioLabel={image.ratioLabel ?? (execution.taskType === 'montage' ? '视频比例' : undefined)}
            />
          </>
        ) : null}
        {quantity ? (
          <>
            <Separator />
            <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2">
              <h3 className={quantity.min === quantity.max ? 'sr-only' : 'font-medium'}>
                {quantity.label}
              </h3>
              <div className={`flex min-w-0 justify-end ${quantity.min === quantity.max ? 'col-span-2' : ''}`}>
                <QuantityStepper {...quantity} />
              </div>
            </section>
          </>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}
