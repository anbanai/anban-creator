import { ChevronDownIcon, ListOrderedIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { QuantityStepper, type QuantityStepperProps } from './QuantityStepper'

export type ComposerQuantityControlProps = QuantityStepperProps

export function ComposerQuantityControl(props: ComposerQuantityControlProps) {
  if (props.min === props.max) return <QuantityStepper {...props} />

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={props.disabled}
            aria-label={`${props.label}：${props.value}`}
            className="max-w-full px-2 text-muted-foreground"
          />
        )}
      >
        <ListOrderedIcon data-icon="inline-start" />
        <span className="truncate">{props.label} {props.value}</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(18rem,calc(100vw-2rem))] p-4"
      >
        <div className="flex items-center justify-between gap-3">
          <span className="font-medium">{props.label}</span>
          <QuantityStepper {...props} />
        </div>
      </PopoverContent>
    </Popover>
  )
}
