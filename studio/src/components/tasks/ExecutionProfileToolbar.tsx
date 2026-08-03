import { ChevronDownIcon, GaugeIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { executionProfilePriceInfo } from '@/lib/pricing'
import {
  ExecutionProfileSelector,
  type ExecutionProfileSelectorProps,
} from './ExecutionProfileSelector'

export type ExecutionProfileToolbarProps = ExecutionProfileSelectorProps

export function ExecutionProfileToolbar(props: ExecutionProfileToolbarProps) {
  const selected = props.profiles.find((profile) => profile.id === props.value)
  const pricing = selected && props.taskType
    ? executionProfilePriceInfo(props.catalog, props.taskType, selected.id, props.priceUnit)
    : undefined
  const displayName = props.loading ? '加载中' : selected?.display_name ?? '选择配置'
  const priceLabel = pricing?.price === undefined
    ? undefined
    : `${pricing.price.toLocaleString()} 积分`
  const summary = [displayName, priceLabel].filter(Boolean).join(' · ')

  return (
    <Popover>
      <PopoverTrigger
        render={(
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={props.disabled}
            aria-label={`执行配置：${summary}`}
            className="max-w-full px-2 text-muted-foreground"
          />
        )}
      >
        <GaugeIcon data-icon="inline-start" />
        <span className="truncate">{summary}</span>
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-[var(--available-height)] w-[min(42rem,calc(100vw-2rem))] overflow-y-auto p-4"
      >
        <ExecutionProfileSelector {...props} />
      </PopoverContent>
    </Popover>
  )
}
