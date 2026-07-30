import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { ChevronDown } from 'lucide-react'
import type { AgentExecutionProfileCapability, AgentExecutionProfileID, AgentModelMatrix } from '@/types'

const minimumTierLabel = {
  free: '全部用户',
  pro: 'Pro 版及以上',
  enterprise: '企业版',
} as const

const unavailableReasonLabel: Record<string, string> = {
  requires_pro: '需要 Pro 版或企业版',
  requires_enterprise: '需要企业版',
  profile_configuration_missing: '当前档位尚未配置',
  agent_provider_unavailable: '当前模型服务不可用',
  agent_model_cost_unmapped: '当前模型尚未配置成本',
}

const modelRoles: Array<[keyof AgentModelMatrix, string]> = [
  ['default', '默认'],
  ['opus', 'Opus'],
  ['fable', 'Fable'],
  ['sonnet', 'Sonnet'],
  ['haiku', 'Haiku'],
]

function uniformModel(models: AgentModelMatrix): string | undefined {
  const values = Object.values(models)
  return values.every((model) => model === values[0]) ? values[0] : undefined
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
        const uniform = uniformModel(profile.models)
        return (
          <Collapsible key={profile.id} className="min-w-0">
            <ToggleGroupItem
              value={profile.id}
              disabled={!profile.available}
              aria-label={`${profile.display_name}，${profile.provider}，${profile.models.default}，${minimumTierLabel[profile.min_tier]}${profile.available ? '' : `，不可用：${reason}`}`}
              className="h-auto min-h-24 w-full min-w-0 flex-col items-start justify-start gap-1 whitespace-normal px-3 py-2 text-left"
            >
              <span className="flex w-full items-center justify-between gap-2">
                <span className="font-medium">{profile.display_name}</span>
                <span className="flex shrink-0 items-center gap-1">
                  <Badge variant="outline">{minimumTierLabel[profile.min_tier]}</Badge>
                  {!profile.available ? <Badge variant="secondary">不可用</Badge> : null}
                </span>
              </span>
              <span className="text-xs text-muted-foreground">{profile.provider}</span>
              <span className="break-all font-mono text-[11px] text-muted-foreground">
                {uniform ? `全部角色：${uniform}` : `默认：${profile.models.default}`}
              </span>
              <span className="text-xs text-muted-foreground">
                {profile.available ? profile.description : reason}
              </span>
            </ToggleGroupItem>
            {!uniform ? (
              <>
                <CollapsibleTrigger
                  aria-label={`查看${profile.display_name}模型配置`}
                  title="查看模型配置"
                  className="mt-1 flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <ChevronDown className="size-4 transition-transform in-data-[panel-open]:rotate-180" />
                </CollapsibleTrigger>
                <CollapsibleContent className="pt-1">
                  <div className="space-y-1 border-l border-border pl-2 text-[11px] text-muted-foreground">
                    {modelRoles.slice(1).map(([role, label]) => (
                      <div key={role} className="break-all font-mono">{label}：{profile.models[role]}</div>
                    ))}
                  </div>
                </CollapsibleContent>
              </>
            ) : null}
          </Collapsible>
        )
      })}
    </ToggleGroup>
  )
}
