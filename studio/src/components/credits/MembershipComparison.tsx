import { Globe, Terminal, Puzzle, Headphones, Zap, Layers } from 'lucide-react'
import { cn } from '@/lib/utils'
import { tierBenefits } from '@/lib/labels'
import Badge from '@/components/ui/Badge'

const platformIcons: Record<string, React.ComponentType<{ className?: string }>> = {
  'Web 端': Globe,
  'Claude Code': Terminal,
  'OpenClaw': Puzzle,
  '全部平台': Layers,
  '业务指导与辅助': Headphones,
}

export default function MembershipComparison({ currentTier }: { currentTier: string }) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      {tierBenefits.map((tier) => {
        const isCurrent = tier.key === currentTier
        return (
          <div
            key={tier.key}
            className={cn(
              'relative rounded-xl border p-5 transition-colors',
              isCurrent
                ? 'border-primary ring-2 ring-primary/20'
                : 'border-border',
            )}
          >
            <div className="flex items-center gap-2">
              <h3 className="text-lg font-semibold text-foreground">{tier.name}</h3>
              {tier.recommended && <Badge variant="info">推荐</Badge>}
              {isCurrent && <Badge variant="success">当前</Badge>}
            </div>

            <div className="mt-4 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">积分倍率</span>
                <span className="text-sm font-semibold text-primary">{tier.creditMultiplier}</span>
              </div>

              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">并发任务</span>
                <span className="text-sm font-medium text-foreground">{tier.concurrentTasks} 个</span>
              </div>

              <div>
                <span className="text-sm text-muted-foreground">可用平台</span>
                <ul className="mt-1.5 space-y-1">
                  {tier.platforms.map((platform) => {
                    const Icon = platformIcons[platform] ?? Zap
                    return (
                      <li key={platform} className="flex items-center gap-2 text-sm text-foreground">
                        <Icon className="h-4 w-4 shrink-0 text-muted-foreground" />
                        {platform}
                      </li>
                    )
                  })}
                </ul>
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
