import type { Channel, ChannelStats } from '@/types'
import { platformLabels } from '@/lib/labels'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'

interface ChannelCardProps {
  channel: Channel
  stats?: ChannelStats
  onEdit?: (channel: Channel) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
  onDelete?: (id: string) => void
}

const platformBadgeVariant: Record<string, 'success' | 'info' | 'danger' | 'neutral'> = {
  article: 'success',
  xls: 'info',
  rednote: 'danger',
}

const platformBorderColor: Record<string, string> = {
  article: 'border-l-[#07C160]',
  xls: 'border-l-[#34C759]',
  rednote: 'border-l-[#FF2442]',
}

const platformHoverBorderColor: Record<string, string> = {
  article: 'hover:border-l-[#07C160]/50',
  xls: 'hover:border-l-[#34C759]/50',
  rednote: 'hover:border-l-[#FF2442]/50',
}

const platformBgColor: Record<string, string> = {
  article: 'bg-[#07C160]/10',
  xls: 'bg-[#34C759]/10',
  rednote: 'bg-[#FF2442]/10',
}

export function ChannelCard({ channel, stats, onEdit, archiving, restoring, onArchive, onRestore, onDelete }: ChannelCardProps) {
  const platformLabel = platformLabels[channel.platform] || channel.platform
  const platformBadge = platformBadgeVariant[channel.platform] || ('neutral' as const)
  const borderColor = platformBorderColor[channel.platform] || ''
  const hoverBorderColor = platformHoverBorderColor[channel.platform] || ''

  return (
    <div className={`group rounded-lg border border-border bg-card p-5 border-l-4 ${borderColor} ${hoverBorderColor} transition-all duration-200 hover:shadow-md active:scale-[0.98]`}>
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          {channel.avatar_url ? (
            <img
              src={channel.avatar_url}
              alt={channel.name}
              className="h-11 w-11 rounded-full object-cover ring-1 ring-border"
            />
          ) : (
            <div className={`flex h-11 w-11 items-center justify-center rounded-full ${platformBgColor[channel.platform] || 'bg-primary/10'}`}>
              {renderPlatformIcon(channel.platform)}
            </div>
          )}
          <div>
            <h3 className="text-sm font-semibold text-foreground">{channel.name}</h3>
            <Badge variant={platformBadge} className="mt-1 text-[10px]">
              {renderPlatformIcon(channel.platform)}
              {platformLabel}
            </Badge>
          </div>
        </div>
        {channel.status === 'archived' && (
          <Badge variant="warning" className="text-[10px]">已归档</Badge>
        )}
      </div>
      {channel.positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{channel.positioning}</p>
      )}
      {stats && (
        <div className="mt-4 flex gap-4 border-t border-border pt-3 text-xs text-muted-foreground">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex gap-1.5">
        {onEdit && (
          <Button variant="ghost" size="xs" onClick={() => onEdit(channel)} aria-label="编辑账号">
            编辑
          </Button>
        )}
        {channel.status === 'active' && onArchive && (
          <Button variant="ghost" size="xs" disabled={archiving} loading={archiving} onClick={() => onArchive(channel.id)} aria-label="归档账号">
            归档
          </Button>
        )}
        {channel.status === 'archived' && onRestore && (
          <Button variant="ghost" size="xs" disabled={restoring} loading={restoring} onClick={() => onRestore(channel.id)} aria-label="恢复账号">
            恢复
          </Button>
        )}
        {onDelete && (
          <Button variant="destructive" size="xs" onClick={() => onDelete(channel.id)} aria-label="删除账号">
            删除
          </Button>
        )}
      </div>
    </div>
  )
}
