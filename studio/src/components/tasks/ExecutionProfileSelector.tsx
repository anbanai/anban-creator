import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { AgentExecutionProfileCapability, AgentExecutionProfileID } from '@/types'
import type { BillingCatalog } from '@/types'
import { executionProfilePriceInfo } from '@/lib/pricing'

const minimumTierLabel = {
  free: '全部用户',
  pro: 'Pro 版及以上',
  enterprise: '企业版',
} as const

const unavailableReasonLabel: Record<string, string> = {
  requires_pro: '需要 Pro 版或企业版',
  requires_enterprise: '需要企业版',
  profile_configuration_missing: '当前档位尚未配置',
  agent_provider_unavailable: '当前执行服务不可用',
  agent_model_cost_unmapped: '当前档位尚未配置成本',
}

function unavailableReason(profile: AgentExecutionProfileCapability): string {
  const reason = profile.unavailable_reason || ''
  return unavailableReasonLabel[reason] || reason || '当前不可用'
}

export interface ExecutionProfileSelectorProps {
  profiles: AgentExecutionProfileCapability[]
  value: AgentExecutionProfileID | ''
  onChange: (value: AgentExecutionProfileID) => void
  loading?: boolean
  disabled?: boolean
  catalog?: BillingCatalog
  taskType?: string
  priceUnit?: 'task' | 'run'
}

export function ExecutionProfileSelector({
  profiles,
  value,
  onChange,
  loading = false,
  disabled = false,
  catalog,
  taskType,
  priceUnit = 'task',
}: ExecutionProfileSelectorProps) {
  if (loading) {
    return (
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3" aria-label="正在加载执行配置">
        {[0, 1, 2].map((index) => <Skeleton key={index} className="h-24 w-full" />)}
      </div>
    )
  }

  return (
    <ToggleGroup
      aria-label="Agent 执行配置"
      className="grid w-full grid-cols-1 items-stretch gap-2 sm:grid-cols-3"
      value={value ? [value] : []}
      onValueChange={(next) => {
        const selected = next[0] as AgentExecutionProfileID | undefined
        if (selected) onChange(selected)
      }}
      variant="outline"
      disabled={disabled}
    >
      {profiles.map((profile) => {
        const reason = unavailableReason(profile)
        const pricing = taskType ? executionProfilePriceInfo(catalog, taskType, profile.id, priceUnit) : undefined
        return (
          <ToggleGroupItem
            key={profile.id}
            value={profile.id}
            disabled={!profile.available}
            aria-label={`${profile.display_name}，${minimumTierLabel[profile.min_tier]}${profile.available ? '' : `，不可用：${reason}`}`}
            className="h-auto min-h-24 w-full min-w-0 flex-col items-start justify-start gap-1 whitespace-normal px-3 py-2 text-left"
          >
            <span className="flex w-full items-center justify-between gap-2">
              <span className="font-medium">{profile.display_name}</span>
              <span className="flex shrink-0 items-center gap-1">
                <Badge variant="outline">{minimumTierLabel[profile.min_tier]}</Badge>
                {!profile.available ? <Badge variant="secondary">不可用</Badge> : null}
              </span>
            </span>
            <span className="text-xs text-muted-foreground">
              {profile.available ? profile.description : reason}
            </span>
            {pricing ? <span className="text-xs font-medium text-foreground">{pricing.price === undefined ? '价格暂不可用' : `${pricing.price.toLocaleString()} 积分 · ${pricing.multiplier === undefined ? '倍率暂不可用' : `${pricing.multiplier.toFixed(2).replace(/\.00$/, '')}x`}`}</span> : null}
          </ToggleGroupItem>
        )
      })}
    </ToggleGroup>
  )
}
