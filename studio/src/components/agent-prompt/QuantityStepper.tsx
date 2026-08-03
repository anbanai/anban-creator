import { MinusIcon, PlusIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'

export interface QuantityStepperProps {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
  disabled?: boolean
}

export function QuantityStepper({
  label,
  value,
  min,
  max,
  onChange,
  disabled = false,
}: QuantityStepperProps) {
  if (min === max) {
    return (
      <span
        data-slot="quantity-stepper"
        aria-label={`${label}：${value}，当前能力上限`}
        className="whitespace-nowrap text-sm text-muted-foreground tabular-nums"
      >
        {label} {value} · 当前能力上限
      </span>
    )
  }

  return (
    <span data-slot="quantity-stepper" className="flex items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="icon-sm"
        aria-label={`减少${label}`}
        disabled={disabled || value <= min}
        onClick={() => onChange(Math.max(min, value - 1))}
      >
        <MinusIcon data-icon="inline-start" />
      </Button>
      <span
        aria-label={`${label}：${value}`}
        className="w-8 text-center text-sm font-medium tabular-nums"
      >
        {value}
      </span>
      <Button
        type="button"
        variant="outline"
        size="icon-sm"
        aria-label={`增加${label}`}
        disabled={disabled || value >= max}
        onClick={() => onChange(Math.min(max, value + 1))}
      >
        <PlusIcon data-icon="inline-start" />
      </Button>
    </span>
  )
}
