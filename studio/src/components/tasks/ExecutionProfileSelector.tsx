import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { AgentExecutionProfileCapability, AgentExecutionProfileID } from '@/types'

const minimumTierLabel = {
  free: '全部用户',
  pro: 'Pro 版及以上',
  enterprise: '企业版',
} as const

const unavailableReasonLabel: Record<string, string> = {
  requires_pro: '需要 Pro 版或企业版',
  requires_enterprise: '需要企业版',
  provider_configuration_missing: '当前模型尚未配置',
  provider_configuration_invalid: '当前模型配置无效',
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
}

export function ExecutionProfileSelector({
  profiles,
  value,
  onChange,
  loading = false,
  disabled = false,
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
        return (
        <ToggleGroupItem
          key={profile.id}
          value={profile.id}
          disabled={!profile.available}
          aria-label={`${profile.display_name}，${profile.model_name}，${profile.model_id}，${minimumTierLabel[profile.min_tier]}${profile.available ? '' : `，不可用：${reason}`}`}
          className="h-auto min-h-24 min-w-0 flex-col items-start justify-start gap-1 whitespace-normal px-3 py-2 text-left"
        >
          <span className="flex w-full items-center justify-between gap-2">
            <span className="font-medium">{profile.display_name}</span>
            <span className="flex shrink-0 items-center gap-1">
              <Badge variant="outline">{minimumTierLabel[profile.min_tier]}</Badge>
              {!profile.available ? <Badge variant="secondary">不可用</Badge> : null}
            </span>
          </span>
          <span className="text-xs text-muted-foreground">
            {profile.model_name} <span className="font-mono text-[11px]">{profile.model_id}</span>
          </span>
          <span className="text-xs text-muted-foreground">
            {profile.available ? profile.description : reason}
          </span>
        </ToggleGroupItem>
        )
      })}
    </ToggleGroup>
  )
}
