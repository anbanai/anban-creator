import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Check } from 'lucide-react'
import type { AgentExecutionProfileCapability, AgentExecutionProfileID } from '@/types'
import { cn } from '@/lib/utils'

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
  taskType?: string
  layout?: 'vertical' | 'horizontal'
}

export function ExecutionProfileSelector({
  profiles,
  value,
  onChange,
  loading = false,
  disabled = false,
  layout = 'vertical',
}: ExecutionProfileSelectorProps) {
  if (loading) {
    return (
      <div
        className={cn('gap-2', layout === 'horizontal' ? 'flex flex-wrap' : 'grid grid-cols-1')}
        aria-label="正在加载执行配置"
      >
        {[0, 1, 2].map((index) => <Skeleton key={index} className={layout === 'horizontal' ? 'h-8 w-28' : 'h-16 w-full'} />)}
      </div>
    )
  }

  const selectedProfile = profiles.find((profile) => profile.id === value)

  return (
    <div className={cn('min-w-0', layout === 'horizontal' && 'flex flex-col gap-1.5')}>
      {layout === 'horizontal' && selectedProfile ? (
        <p aria-live="polite" className="min-h-5 text-xs leading-5 text-muted-foreground">
          {selectedProfile.available ? selectedProfile.description : unavailableReason(selectedProfile)}
        </p>
      ) : null}
      <ToggleGroup
        aria-label="Agent 执行配置"
        className={cn(
          'w-full gap-1',
          layout === 'horizontal' ? 'flex flex-wrap justify-start' : 'grid grid-cols-1 items-stretch gap-2',
        )}
        value={value ? [value] : []}
        onValueChange={(next) => {
          const selected = next[0] as AgentExecutionProfileID | undefined
          if (selected) onChange(selected)
        }}
        variant={layout === 'horizontal' ? 'default' : 'outline'}
        disabled={disabled}
      >
        {profiles.map((profile) => {
          const reason = unavailableReason(profile)
          return (
            <ToggleGroupItem
              key={profile.id}
              value={profile.id}
              disabled={!profile.available}
              aria-label={`${profile.display_name}，${minimumTierLabel[profile.min_tier]}${profile.available ? '' : `，不可用：${reason}`}`}
              title={profile.available ? profile.description : reason}
              className={cn(
                layout === 'horizontal'
                  ? 'h-8 gap-1.5 px-2 text-xs aria-pressed:bg-primary/8 aria-pressed:text-primary data-[state=on]:bg-primary/8 data-[state=on]:text-primary'
                  : 'h-auto min-h-16 w-full min-w-0 flex-col items-start justify-center gap-0.5 whitespace-normal px-3 py-2 text-left',
              )}
            >
              {layout === 'horizontal' ? <>
                <Check aria-hidden="true" className={cn('size-3', value !== profile.id && 'invisible')} />
                <span>{profile.display_name}</span>
              </> : <>
                <span className="flex w-full min-w-0 items-center justify-between gap-2">
                  <span className="font-medium">
                    {profile.display_name}
                  </span>
                  <span className="flex shrink-0 flex-wrap items-center gap-1">
                    <Badge variant="outline">{minimumTierLabel[profile.min_tier]}</Badge>
                    {!profile.available ? <Badge variant="secondary">不可用</Badge> : null}
                  </span>
                </span>
                <span className="w-full min-w-0 truncate text-xs text-muted-foreground">
                  {profile.available ? profile.description : reason}
                </span>
              </>}
            </ToggleGroupItem>
          )
        })}
      </ToggleGroup>
    </div>
  )
}
