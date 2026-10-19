import { Layers3 } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { contentTypeLabel } from '@/lib/labels'
import {
  platformBgColor,
  platformIcon,
  platformIconColor,
} from '@/lib/PlatformIcon'
import { cn } from '@/lib/utils'

const agentLabels: Record<string, string> = {
  'wechat-article': '公众号文章',
  'wechat-picture': '公众号贴图',
  wechat: '公众号',
  seednote: '种草笔记',
  moments: '朋友圈',
  ecommerce: '电商出图',
  viral_analysis: '爆文拆解',
  'profile-analysis': '人设画像',
  profile_analysis: '人设画像',
  'feedback-analysis': '反馈分析',
  feedback_analysis: '反馈分析',
  montage: '视频生成',
  hypit: '视频复刻',
  'whiteboard-animation': '白板动画',
}

const fallbackBackground = 'bg-muted/80 dark:bg-muted/50'
const fallbackForeground = 'text-muted-foreground'

export function agentDisplayName(agentID: string) {
  return agentLabels[agentID] ?? contentTypeLabel[agentID] ?? agentID
}

function uniqueAgentIDs(agentIds: readonly string[]) {
  return agentIds
    .map((id) => id.trim())
    .filter(Boolean)
    .filter((id, index, all) => all.indexOf(id) === index)
}

interface AgentIconStackProps {
  agentIds?: readonly string[]
  maxVisible?: number
  compact?: boolean
  showLabels?: boolean
  className?: string
  emptyLabel?: string
}

export function AgentIconStack({
  agentIds = [],
  maxVisible = 4,
  compact = false,
  showLabels = false,
  className,
  emptyLabel = '尚未关联 Agent',
}: AgentIconStackProps) {
  const reducedMotion = useReducedMotion()
  const ids = uniqueAgentIDs(agentIds)
  const visible = ids.slice(0, Math.max(0, maxVisible))
  const overflow = ids.slice(visible.length)
  const names = ids.map(agentDisplayName)
  const stackLabel = names.length ? `关联 Agent：${names.join('、')}` : emptyLabel
  const allCapabilitiesLabel = `支持的内容类型：${names.join('、')}`
  const shellClassName = compact
    ? 'size-5 rounded-md'
    : 'size-6 rounded-md'
  const iconClassName = compact ? 'size-3.5' : 'size-4'

  if (!ids.length) {
    return (
      <span
        data-slot="agent-icon-stack"
        data-empty="true"
        aria-label={stackLabel}
        className={cn('inline-flex min-w-0 items-center text-xs text-muted-foreground', className)}
      >
        {emptyLabel}
      </span>
    )
  }

  return (
    <span
      data-slot="agent-icon-stack"
      aria-label={stackLabel}
      className={cn(
        showLabels ? 'flex max-w-full flex-wrap' : 'inline-flex',
        'min-w-0 items-center gap-1',
        className,
      )}
    >
      {visible.map((agentID, index) => {
        const Icon = platformIcon[agentID] ?? Layers3
        const label = agentDisplayName(agentID)
        const background = platformBgColor[agentID] ?? fallbackBackground
        const foreground = platformIconColor[agentID] ?? fallbackForeground
        return (
          <Tooltip key={agentID}>
            <TooltipTrigger
              render={
                <motion.span
                  data-slot="agent-icon"
                  data-agent-id={agentID}
                  role="img"
                  aria-label={label}
                  className={cn(
                    'inline-flex shrink-0 items-center justify-center border border-border/70 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring',
                    showLabels ? 'min-h-6 gap-1 rounded-md px-1.5 text-[11px] font-medium' : shellClassName,
                    background,
                    foreground,
                  )}
                  whileFocus={reducedMotion ? undefined : { scale: 1.08 }}
                  whileHover={reducedMotion ? undefined : { scale: 1.12, rotate: index % 2 === 0 ? -4 : 4 }}
                  transition={{ type: 'spring', stiffness: 420, damping: 20 }}
                />
              }
            >
              <Icon aria-hidden="true" className={cn('shrink-0', iconClassName)} />
              {showLabels ? <span className="max-w-24 truncate">{label}</span> : null}
            </TooltipTrigger>
            <TooltipContent>{label}</TooltipContent>
          </Tooltip>
        )
      })}
      {overflow.length ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <span
                data-slot="agent-icon-overflow"
                role="img"
                aria-label={allCapabilitiesLabel}
                title={allCapabilitiesLabel}
                className={cn(
                  'inline-flex shrink-0 items-center justify-center border border-dashed border-border bg-muted text-[10px] font-semibold text-muted-foreground transition-transform hover:scale-105',
                  shellClassName,
                )}
              />
            }
          >
            +{overflow.length}
          </TooltipTrigger>
          <TooltipContent>{allCapabilitiesLabel}</TooltipContent>
        </Tooltip>
      ) : null}
    </span>
  )
}
