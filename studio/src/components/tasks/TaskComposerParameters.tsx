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
import { executionProfilePriceInfo } from '@/lib/pricing'
import { ExecutionProfileSelector, type ExecutionProfileSelectorProps } from './ExecutionProfileSelector'

export interface TaskComposerParametersProps {
  execution: ExecutionProfileSelectorProps
  image?: ImageGenerationSettingsProps
  quantity?: QuantityStepperProps
  disabled?: boolean
}

function executionSummary(execution: ExecutionProfileSelectorProps) {
  if (execution.loading) return '执行配置加载中'
  const selected = execution.profiles.find((profile) => profile.id === execution.value)
  if (!selected) return '未选择执行配置'
  const pricing = execution.taskType
    ? executionProfilePriceInfo(
        execution.catalog,
        execution.taskType,
        selected.id,
        execution.priceUnit,
      )
    : undefined
  return pricing?.price === undefined
    ? selected.display_name
    : `${selected.display_name} ${pricing.price.toLocaleString()} 积分`
}

function imageSummary(image: ImageGenerationSettingsProps) {
  if (image.loading) return '图像设置加载中'
  const capability = image.capabilities.find((option) => option.key === image.capabilityKey)
  const ratio = image.ratio === 'auto' ? '智能适配' : image.ratio
  return [ratio, capability?.display_name].filter(Boolean).join(' ')
}

export function TaskComposerParameters({
  execution,
  image,
  quantity,
  disabled = false,
}: TaskComposerParametersProps) {
  const summary = [
    executionSummary(execution),
    image ? imageSummary(image) : undefined,
    quantity ? `${quantity.label} ${quantity.value}` : undefined,
  ].filter(Boolean).join(' · ')

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            aria-label={`创作参数：${summary}`}
            className="max-w-full px-2 text-muted-foreground"
          />
        )}
      >
        <SlidersHorizontalIcon data-icon="inline-start" />
        <span>创作参数</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-[var(--available-height)] w-[min(36rem,calc(100vw-2rem))] gap-3 overflow-y-auto p-4"
      >
        <PopoverTitle className="sr-only">创作参数</PopoverTitle>
        <section className="flex flex-col gap-2">
          <h3 className="font-medium">执行配置</h3>
          <ExecutionProfileSelector {...execution} />
        </section>
        {image ? (
          <>
            <Separator />
            <ImageGenerationSettings {...image} />
          </>
        ) : null}
        {quantity ? (
          <>
            <Separator />
            <section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2">
              <h3 className={quantity.min === quantity.max ? 'sr-only' : 'font-medium'}>
                {quantity.label}
              </h3>
              <div className="flex min-w-0 justify-end">
                <QuantityStepper {...quantity} />
              </div>
            </section>
          </>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}
